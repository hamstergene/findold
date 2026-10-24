package main

import (
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Action is what to do with old items.
type Action int

const (
	Report Action = iota
	Move
	Delete
)

// Options are the parsed command-line arguments.
type Options struct {
	Dir       string
	MinAge    int // days
	Verbosity int // 0 quiet, 1 normal, 2 verbose
	Action    Action
	MoveTo    string   // destination for Move, relative to Dir
	Exclude   []string // names of items to ignore in addition to defaultExclude
	Dry       bool     // print shell commands instead of acting
}

// defaultExclude are metadata files maintained by macOS Finder.
var defaultExclude = []string{".DS_Store", ".localized"}

// Env is everything findold takes from the outside world.
type Env struct {
	FS     FS
	Now    time.Time
	Uid    int
	Stdout io.Writer
	Stderr io.Writer
}

type fileID struct{ dev, ino uint64 }

type finder struct {
	Env
	opts Options
	dest string // where Move puts old items

	// Ignored items are matched both by name and by identity, so that e.g. hard links
	// and differently cased names on case-insensitive filesystems are ignored too.
	ignoredNames map[string]bool
	ignoredIDs   map[fileID]bool

	dryCreated map[string]bool // paths a dry run pretends to have created
}

// findOld processes the old items directly inside opts.Dir and reports whether there were any.
// Problems with individual items are printed and skipped; the error is only for failing to list opts.Dir.
func findOld(env Env, opts Options) (bool, error) {
	f := &finder{
		Env:          env,
		opts:         opts,
		ignoredNames: map[string]bool{},
		ignoredIDs:   map[fileID]bool{},
		dryCreated:   map[string]bool{},
	}
	for _, name := range append(append([]string{}, defaultExclude...), opts.Exclude...) {
		f.ignore(name, pyJoin(opts.Dir, name))
	}
	if opts.Action == Move {
		f.dest = pyJoin(opts.Dir, opts.MoveTo)
		f.ignore(containingItem(opts.MoveTo), f.dest)
	}

	names, err := f.FS.ReadDir(opts.Dir)
	if err != nil {
		return false, err
	}
	found := false
	for _, name := range names {
		if f.ignoredNames[name] {
			continue
		}
		path := pyJoin(opts.Dir, name)
		old, err := f.process(name, path)
		found = found || old
		if err != nil && opts.Verbosity >= 1 {
			fmt.Fprintf(f.Stderr, "%s : %v\n", path, err)
		}
	}
	return found, nil
}

func (f *finder) ignore(name, path string) {
	if name != "" {
		f.ignoredNames[name] = true
	}
	if fi, err := f.FS.Stat(path); err == nil {
		f.ignoredIDs[fileID{fi.Dev, fi.Ino}] = true
	}
}

// containingItem returns the name of the item directly inside the scanned directory that contains
// the relative path p, or "" if p is outside of it.
func containingItem(p string) string {
	p = filepath.Clean(p)
	if filepath.IsAbs(p) || p == "." || p == ".." || strings.HasPrefix(p, "../") {
		return ""
	}
	first, _, _ := strings.Cut(p, "/")
	return first
}

// process handles a single item and reports whether it is old.
func (f *finder) process(name, path string) (bool, error) {
	link, err := f.FS.Lstat(path)
	if err != nil {
		return false, err
	}
	fi := f.resolve(path, link)
	if f.ignoredIDs[fileID{fi.Dev, fi.Ino}] {
		return false, nil
	}
	if f.Uid != 0 && fi.Uid != f.Uid {
		if f.opts.Dry {
			f.printf("# ignored (not owned): %s", path)
		} else if f.opts.Verbosity >= 2 {
			f.printf("-1 %s -- not owned", path)
		}
		return false, nil
	}

	t, err := f.touchTime(path, fi)
	if err != nil {
		return false, err
	}
	age := days(f.Now, t)
	if age < float64(f.opts.MinAge) {
		if f.opts.Verbosity >= 2 {
			if f.opts.Dry {
				f.printf("# skip (age=%d) %s", int(age), path)
			} else {
				f.printf("%s %s", formatAge(age), path)
			}
		}
		return false, nil
	}

	switch f.opts.Action {
	case Delete:
		// link.IsDir is false for symlinks to directories: those are removed, not their targets.
		return true, f.delete(path, link.IsDir, age)
	case Move:
		return true, f.move(name, path, age)
	}
	switch f.opts.Verbosity {
	case 1:
		f.printf("%s", path)
	case 2:
		f.printf("%s %s -- OLD", formatAge(age), path)
	}
	return true, nil
}

// resolve returns the metadata an item is judged by: that of its target if it is a symlink,
// or of the item itself if it isn't or the target can't be examined (e.g. a dangling symlink).
func (f *finder) resolve(path string, link FileInfo) FileInfo {
	if link.IsSymlink {
		if target, err := f.FS.Stat(path); err == nil {
			return target
		}
	}
	return link
}

// touchTime returns the latest touch time of the item at path with metadata fi and,
// if it's a directory, of everything inside it recursively. Symlinks inside are judged
// by their targets but never descended into, so symlink cycles are harmless.
func (f *finder) touchTime(path string, fi FileInfo) (time.Time, error) {
	t := fi.TouchTime
	if !fi.IsDir {
		return t, nil
	}
	names, err := f.FS.ReadDir(path)
	if err != nil {
		return t, err
	}
	for _, name := range names {
		child := pyJoin(path, name)
		cfi, err := f.FS.Lstat(child)
		if isNotExist(err) {
			continue // removed while scanning
		}
		if err != nil {
			return t, err
		}
		ct := f.resolve(child, cfi).TouchTime
		if cfi.IsDir {
			if ct, err = f.touchTime(child, cfi); err != nil {
				return t, err
			}
		}
		if ct.After(t) {
			t = ct
		}
	}
	return t, nil
}

func (f *finder) delete(path string, isDir bool, age float64) error {
	if f.opts.Dry {
		if isDir {
			f.printf("rm -r %s", shellQuote(path))
		} else {
			f.printf("rm %s", shellQuote(path))
		}
		return nil
	}

	var err error
	if isDir {
		err = f.FS.RemoveAll(path)
	} else {
		err = f.FS.Remove(path)
	}
	if err != nil {
		return err
	}
	switch f.opts.Verbosity {
	case 1:
		f.printf("%s", path)
	case 2:
		f.printf("%s %s -- DELETED", formatAge(age), path)
	}
	return nil
}

func (f *finder) move(name, path string, age float64) error {
	// The destination is only created when there is something to move.
	if !f.exists(f.dest) {
		if f.opts.Dry {
			f.printf("mkdir -p %s", shellQuote(f.dest))
			f.dryCreated[f.dest] = true
		} else if err := f.FS.MkdirAll(f.dest, 0o775); err != nil {
			return err
		}
	}

	// Never overwrite: append a numeric suffix if the destination already has an item with this name.
	dst := pyJoin(f.dest, name)
	for i := 1; f.exists(dst); i++ {
		dst = pyJoin(f.dest, name) + "_" + strconv.Itoa(i)
	}

	if f.opts.Dry {
		f.printf("mv %s %s", shellQuote(path), shellQuote(dst))
		f.dryCreated[dst] = true
		return nil
	}
	if err := f.FS.Move(path, dst); err != nil {
		return err
	}
	switch f.opts.Verbosity {
	case 1:
		f.printf("%s -> %s", path, dst)
	case 2:
		f.printf("%s %s -> %s", formatAge(age), path, dst)
	}
	return nil
}

func (f *finder) exists(path string) bool {
	if f.dryCreated[path] {
		return true
	}
	_, err := f.FS.Lstat(path)
	return err == nil
}

func (f *finder) printf(format string, args ...any) {
	fmt.Fprintf(f.Stdout, format+"\n", args...)
}

// days returns the time from t to now in days.
// Unlike time.Sub, it doesn't saturate for times centuries apart.
func days(now, t time.Time) float64 {
	secs := float64(now.Unix()-t.Unix()) + float64(now.Nanosecond()-t.Nanosecond())/1e9
	return secs / (24 * 60 * 60)
}

// formatAge formats a number of days like Python's repr(float) did in the original.
func formatAge(age float64) string {
	if a := math.Abs(age); a != 0 && (a < 1e-4 || a >= 1e16) {
		return strconv.FormatFloat(age, 'e', -1, 64)
	}
	s := strconv.FormatFloat(age, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

// pyJoin joins paths like Python's os.path.join did in the original. Unlike filepath.Join,
// it doesn't clean the result, so "." and "-x" join to "./-x", which is safe to print as a shell argument.
func pyJoin(dir, name string) string {
	switch {
	case strings.HasPrefix(name, "/"):
		return name
	case dir == "" || strings.HasSuffix(dir, "/"):
		return dir + name
	default:
		return dir + "/" + name
	}
}

// shellQuote quotes s for a POSIX shell like Python's shlex.quote.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		safe := 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || strings.IndexByte("_@%+=:,./-", c) >= 0
		if !safe {
			return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
		}
	}
	return s
}
