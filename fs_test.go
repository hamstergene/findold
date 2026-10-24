package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Tests of the real filesystem implementation.

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func skipIfRoot(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("permissions don't apply to root")
	}
}

func TestOSStat(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	mustWrite(t, file, "x")
	must(t, os.Mkdir(filepath.Join(dir, "dir"), 0o755))
	must(t, os.Symlink("file", filepath.Join(dir, "link")))
	must(t, os.Symlink("nowhere", filepath.Join(dir, "dangling")))
	must(t, os.Link(file, filepath.Join(dir, "hardlink")))

	var f osFS
	fi, err := f.Stat(file)
	must(t, err)
	if fi.Uid != os.Getuid() || fi.IsDir || fi.IsSymlink || fi.Ino == 0 {
		t.Errorf("Stat(file) = %+v", fi)
	}
	if di, err := f.Stat(filepath.Join(dir, "dir")); err != nil || !di.IsDir || di.IsSymlink {
		t.Errorf("Stat(dir) = %+v, %v", di, err)
	}
	if li, err := f.Stat(filepath.Join(dir, "link")); err != nil || li.IsSymlink || li.Dev != fi.Dev || li.Ino != fi.Ino {
		t.Errorf("Stat(link) = %+v, %v; want same as %+v", li, err, fi)
	}
	if li, err := f.Lstat(filepath.Join(dir, "link")); err != nil || !li.IsSymlink || li.IsDir || li.Ino == fi.Ino {
		t.Errorf("Lstat(link) = %+v, %v", li, err)
	}
	if hi, err := f.Lstat(filepath.Join(dir, "hardlink")); err != nil || hi.Dev != fi.Dev || hi.Ino != fi.Ino {
		t.Errorf("Lstat(hardlink) = %+v, %v; want same as %+v", hi, err, fi)
	}
	if _, err := f.Stat(filepath.Join(dir, "dangling")); !isNotExist(err) {
		t.Errorf("Stat(dangling) error = %v", err)
	}
	if li, err := f.Lstat(filepath.Join(dir, "dangling")); err != nil || !li.IsSymlink {
		t.Errorf("Lstat(dangling) = %+v, %v", li, err)
	}
	if _, err := f.Lstat(filepath.Join(dir, "missing")); !isNotExist(err) {
		t.Errorf("Lstat(missing) error = %v", err)
	}
}

func TestOSTouchTime(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	before := time.Now().Add(-time.Second)
	mustWrite(t, file, "x")
	var f osFS

	// An old modification time doesn't make a file old: changing it updates the status change time.
	must(t, os.Chtimes(file, time.Unix(1, 0), time.Unix(1000, 0)))
	fi, err := f.Stat(file)
	must(t, err)
	if fi.TouchTime.Before(before) || fi.TouchTime.After(time.Now().Add(time.Second)) {
		t.Errorf("TouchTime = %v, want about now", fi.TouchTime)
	}

	// The access time is ignored, a modification time in the future is not.
	future := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	must(t, os.Chtimes(file, future.Add(time.Hour), future))
	fi, err = f.Stat(file)
	must(t, err)
	if !fi.TouchTime.Equal(future) {
		t.Errorf("TouchTime = %v, want %v", fi.TouchTime, future)
	}
}

func TestOSReadDir(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"b", "a", ".hidden", "c"} {
		mustWrite(t, filepath.Join(dir, name), "")
	}
	var f osFS
	names, err := f.ReadDir(dir)
	if want := []string{".hidden", "a", "b", "c"}; err != nil || !reflect.DeepEqual(names, want) {
		t.Errorf("ReadDir = %q, %v; want %q", names, err, want)
	}
	if _, err := f.ReadDir(filepath.Join(dir, "missing")); !isNotExist(err) {
		t.Errorf("ReadDir(missing) error = %v", err)
	}
}

func TestOSMove(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "src/file"), "content")
	var f osFS
	must(t, f.Move(filepath.Join(dir, "src"), filepath.Join(dir, "dst")))
	if b, err := os.ReadFile(filepath.Join(dir, "dst/file")); err != nil || string(b) != "content" {
		t.Errorf("moved file = %q, %v", b, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "src")); !isNotExist(err) {
		t.Errorf("source still exists: %v", err)
	}

	// Errors other than crossing filesystems are returned as is.
	err := f.Move(filepath.Join(dir, "missing"), filepath.Join(dir, "x"))
	if !isNotExist(err) {
		t.Errorf("Move(missing) error = %v", err)
	}
}

// crossFS is an osFS on which renames always fail as if crossing filesystems.
var crossFS = osFS{rename: func(src, dst string) error {
	return &os.LinkError{Op: "rename", Old: src, New: dst, Err: syscall.EXDEV}
}}

func TestOSMoveAcrossFilesystems(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	mtime := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	atime := time.Date(2021, 1, 2, 3, 4, 5, 0, time.UTC)
	mustWrite(t, filepath.Join(src, "file"), "content")
	must(t, os.Chmod(filepath.Join(src, "file"), 0o640))
	must(t, os.Chtimes(filepath.Join(src, "file"), atime, mtime))
	mustWrite(t, filepath.Join(src, "sub/nested"), "nested")
	must(t, os.Symlink("../file", filepath.Join(src, "sub/link")))
	must(t, os.Chmod(filepath.Join(src, "sub"), 0o750))
	must(t, os.Chtimes(filepath.Join(src, "sub"), atime, mtime))
	must(t, os.Chmod(src, 0o705))
	must(t, os.Chtimes(src, atime, mtime))

	dst := filepath.Join(dir, "dst")
	must(t, crossFS.Move(src, dst))

	if _, err := os.Lstat(src); !isNotExist(err) {
		t.Errorf("source still exists: %v", err)
	}
	// Check times before reading the files updates them.
	for path, perm := range map[string]fs.FileMode{"": 0o705, "file": 0o640, "sub": 0o750} {
		fi, err := os.Stat(filepath.Join(dst, path))
		must(t, err)
		if fi.Mode().Perm() != perm {
			t.Errorf("%q permissions = %v, want %v", path, fi.Mode().Perm(), perm)
		}
		if !fi.ModTime().Equal(mtime) {
			t.Errorf("%q modification time = %v, want %v", path, fi.ModTime(), mtime)
		}
		if at := statAccessTime(fi.Sys().(*syscall.Stat_t)); !at.Equal(atime) {
			t.Errorf("%q access time = %v, want %v", path, at, atime)
		}
	}
	for path, content := range map[string]string{"file": "content", "sub/nested": "nested", "sub/link": "content"} {
		if b, err := os.ReadFile(filepath.Join(dst, path)); err != nil || string(b) != content {
			t.Errorf("%s = %q, %v; want %q", path, b, err, content)
		}
	}
	if target, err := os.Readlink(filepath.Join(dst, "sub/link")); err != nil || target != "../file" {
		t.Errorf("symlink target = %q, %v", target, err)
	}

	// A symlink is moved as is.
	must(t, os.Symlink("dst/file", filepath.Join(dir, "link")))
	must(t, crossFS.Move(filepath.Join(dir, "link"), filepath.Join(dir, "moved-link")))
	if target, err := os.Readlink(filepath.Join(dir, "moved-link")); err != nil || target != "dst/file" {
		t.Errorf("moved symlink target = %q, %v", target, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "link")); !isNotExist(err) {
		t.Errorf("source symlink still exists: %v", err)
	}
}

func TestOSMoveAcrossFilesystemsErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	mustWrite(t, file, "content")

	// Nothing is touched if the source is missing or the destination exists.
	if err := crossFS.Move(filepath.Join(dir, "missing"), filepath.Join(dir, "x")); !isNotExist(err) {
		t.Errorf("Move(missing) error = %v", err)
	}
	mustWrite(t, filepath.Join(dir, "existing"), "precious")
	must(t, os.Mkdir(filepath.Join(dir, "existing-dir"), 0o755))
	mustWrite(t, filepath.Join(dir, "existing-dir/x"), "precious")
	must(t, os.Mkdir(filepath.Join(dir, "srcdir"), 0o755))
	must(t, os.Symlink("x", filepath.Join(dir, "srclink")))
	for _, tc := range []struct{ src, dst string }{
		{"file", "existing"},
		{"srcdir", "existing-dir"},
		{"srclink", "existing"},
		{"file", "missing-dir/x"},
	} {
		if err := crossFS.Move(filepath.Join(dir, tc.src), filepath.Join(dir, tc.dst)); err == nil {
			t.Errorf("Move(%s, %s) succeeded", tc.src, tc.dst)
		}
	}
	for path, content := range map[string]string{"file": "content", "existing": "precious", "existing-dir/x": "precious"} {
		if b, err := os.ReadFile(filepath.Join(dir, path)); err != nil || string(b) != content {
			t.Errorf("%s = %q, %v; want %q", path, b, err, content)
		}
	}

	// Special files can't be copied.
	fifo := filepath.Join(dir, "fifo")
	must(t, syscall.Mkfifo(fifo, 0o644))
	if err := crossFS.Move(fifo, filepath.Join(dir, "fifo2")); err == nil || !strings.Contains(err.Error(), "special file") {
		t.Errorf("Move(fifo) error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "fifo2")); !isNotExist(err) {
		t.Errorf("fifo was copied: %v", err)
	}
}

func TestOSMoveAcrossFilesystemsPermissions(t *testing.T) {
	skipIfRoot(t)
	dir := t.TempDir()

	// A failed copy is cleaned up and the source is left intact.
	src := filepath.Join(dir, "src")
	mustWrite(t, filepath.Join(src, "a"), "a")
	mustWrite(t, filepath.Join(src, "b"), "b")
	must(t, os.Chmod(filepath.Join(src, "b"), 0))
	err := crossFS.Move(src, filepath.Join(dir, "dst"))
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("Move error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "dst")); !isNotExist(err) {
		t.Errorf("partial copy remains: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(src, "b")); err != nil {
		t.Errorf("source damaged: %v", err)
	}

	// So is an unreadable directory.
	locked := filepath.Join(dir, "locked")
	must(t, os.Mkdir(locked, 0))
	defer os.Chmod(locked, 0o755)
	if err := crossFS.Move(locked, filepath.Join(dir, "locked2")); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("Move(locked) error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "locked2")); !isNotExist(err) {
		t.Errorf("partial copy remains: %v", err)
	}

	// If the source can't be removed after copying, the copy stays.
	parent := filepath.Join(dir, "readonly")
	mustWrite(t, filepath.Join(parent, "file"), "content")
	must(t, os.Chmod(parent, 0o555))
	defer os.Chmod(parent, 0o755)
	if err := crossFS.Move(filepath.Join(parent, "file"), filepath.Join(dir, "copy")); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("Move(readonly) error = %v", err)
	}
	for _, path := range []string{"readonly/file", "copy"} {
		if b, err := os.ReadFile(filepath.Join(dir, path)); err != nil || string(b) != "content" {
			t.Errorf("%s = %q, %v", path, b, err)
		}
	}
}
