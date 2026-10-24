package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// FileInfo is the file metadata findold needs.
type FileInfo struct {
	Dev, Ino  uint64
	Uid       int
	IsDir     bool
	IsSymlink bool

	// TouchTime is the latest of the modification, status change and (where available) creation times.
	// Access time is not used: merely listing a directory may update it.
	TouchTime time.Time
}

// FS is the filesystem as findold sees it. It allows testing against an in-memory filesystem
// with arbitrary timestamps and owners, which can't be set up on a real one.
type FS interface {
	Stat(name string) (FileInfo, error)
	Lstat(name string) (FileInfo, error)
	// ReadDir returns the names of the directory entries sorted by name.
	ReadDir(name string) ([]string, error)
	MkdirAll(name string, perm fs.FileMode) error
	Remove(name string) error
	RemoveAll(name string) error
	// Move renames src to the non-existent dst, copying across filesystems if necessary.
	// Symlinks are moved themselves rather than followed.
	Move(src, dst string) error
}

// osFS is the real filesystem.
type osFS struct {
	rename func(src, dst string) error // os.Rename if nil; replaceable to test cross-filesystem moves
}

func (osFS) Stat(name string) (FileInfo, error) {
	fi, err := os.Stat(name)
	return fileInfo(fi, err)
}

func (osFS) Lstat(name string) (FileInfo, error) {
	fi, err := os.Lstat(name)
	return fileInfo(fi, err)
}

func fileInfo(fi fs.FileInfo, err error) (FileInfo, error) {
	if err != nil {
		return FileInfo{}, err
	}
	st := fi.Sys().(*syscall.Stat_t)
	return FileInfo{
		Dev:       uint64(st.Dev),
		Ino:       uint64(st.Ino),
		Uid:       int(st.Uid),
		IsDir:     fi.IsDir(),
		IsSymlink: fi.Mode()&fs.ModeSymlink != 0,
		TouchTime: statTouchTime(st),
	}, nil
}

func (osFS) ReadDir(name string) ([]string, error) {
	entries, err := os.ReadDir(name)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names, nil
}

func (osFS) MkdirAll(name string, perm fs.FileMode) error { return os.MkdirAll(name, perm) }
func (osFS) Remove(name string) error                     { return os.Remove(name) }
func (osFS) RemoveAll(name string) error                  { return os.RemoveAll(name) }

func (f osFS) Move(src, dst string) error {
	rename := f.rename
	if rename == nil {
		rename = os.Rename
	}
	err := rename(src, dst)
	if !errors.Is(err, syscall.EXDEV) {
		return err
	}

	created, err := copyTree(src, dst)
	if err != nil {
		if created {
			os.RemoveAll(dst)
		}
		return err
	}
	return os.RemoveAll(src)
}

// copyTree copies src to the new path dst, preserving symlinks, permissions and access and modification times.
// It reports whether it has created dst, which may remain as a partial copy on error.
func copyTree(src, dst string) (created bool, err error) {
	fi, err := os.Lstat(src)
	if err != nil {
		return false, err
	}
	switch mode := fi.Mode(); {
	case mode&fs.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return false, err
		}
		return false, os.Symlink(target, dst)

	case mode.IsDir():
		if err := os.Mkdir(dst, 0o700); err != nil {
			return false, err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return true, err
		}
		for _, e := range entries {
			if _, err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return true, err
			}
		}
		return true, copyAttrs(dst, fi)

	case mode.IsRegular():
		in, err := os.Open(src)
		if err != nil {
			return false, err
		}
		defer in.Close()
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return false, err
		}
		_, err = io.Copy(out, in)
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = copyAttrs(dst, fi)
		}
		return true, err

	default:
		return false, fmt.Errorf("cannot move special file %s across filesystems", src)
	}
}

func copyAttrs(dst string, src fs.FileInfo) error {
	if err := os.Chmod(dst, src.Mode()&(fs.ModePerm|fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky)); err != nil {
		return err
	}
	return os.Chtimes(dst, statAccessTime(src.Sys().(*syscall.Stat_t)), src.ModTime())
}

func timespec(ts syscall.Timespec) time.Time {
	return time.Unix(ts.Unix())
}

func latest(t time.Time, ts ...time.Time) time.Time {
	for _, u := range ts {
		if u.After(t) {
			t = u
		}
	}
	return t
}

func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}
