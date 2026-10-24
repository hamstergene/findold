package main

import (
	"io/fs"
	"path"
	"sort"
	"strings"
	"syscall"
	"time"
)

// memFS is an in-memory FS for tests. Paths must be absolute.
type memFS struct {
	nodes    map[string]*memNode // by clean path
	lastIno  uint64
	failures map[string]error // injected errors by "Op path"
	now      time.Time        // touch time of directories created by MkdirAll
}

type memNode struct {
	isDir  bool
	target string // symlink target, if non-empty
	uid    int
	ino    uint64
	touch  time.Time
}

func newMemFS() *memFS {
	m := &memFS{nodes: map[string]*memNode{}, failures: map[string]error{}, now: testNow}
	m.nodes["/"] = m.newNode(true, "", time.Unix(0, 0))
	return m
}

func (m *memFS) newNode(isDir bool, target string, touch time.Time) *memNode {
	m.lastIno++
	return &memNode{isDir: isDir, target: target, uid: testUid, ino: m.lastIno, touch: touch}
}

// add creates a node, along with any missing parents, which are given an ancient touch time.
func (m *memFS) add(p string, n *memNode) *memNode {
	p = path.Clean(p)
	for dir := path.Dir(p); m.nodes[dir] == nil; dir = path.Dir(dir) {
		m.nodes[dir] = m.newNode(true, "", time.Unix(0, 0))
	}
	m.nodes[p] = n
	return n
}

func (m *memFS) file(p string, touch time.Time) *memNode {
	return m.add(p, m.newNode(false, "", touch))
}
func (m *memFS) dir(p string, touch time.Time) *memNode { return m.add(p, m.newNode(true, "", touch)) }
func (m *memFS) link(p, target string, touch time.Time) *memNode {
	return m.add(p, m.newNode(false, target, touch))
}

// hardlink makes p another name of the existing node at existing.
func (m *memFS) hardlink(p, existing string) { m.add(p, m.nodes[existing]) }

func (m *memFS) fail(op, p string, err error) { m.failures[op+" "+p] = err }

func (m *memFS) check(op, p string) error {
	if err := m.failures[op+" "+p]; err != nil {
		return &fs.PathError{Op: op, Path: p, Err: err}
	}
	return nil
}

func (m *memFS) exists(p string) bool { return m.nodes[path.Clean(p)] != nil }

// resolve returns the clean path of p with symlinks in it resolved, except in the last element unless followLast.
func (m *memFS) resolve(op, p string, followLast bool) (string, *memNode, error) {
	hops := 0
	elems := strings.Split(strings.TrimPrefix(path.Clean(p), "/"), "/")
	cur := "/"
	for i := 0; i < len(elems); i++ {
		if elems[i] == "" {
			continue
		}
		next := path.Join(cur, elems[i])
		n := m.nodes[next]
		if n == nil {
			return "", nil, &fs.PathError{Op: op, Path: p, Err: syscall.ENOENT}
		}
		if n.target != "" && (i < len(elems)-1 || followLast) {
			if hops++; hops > 40 {
				return "", nil, &fs.PathError{Op: op, Path: p, Err: syscall.ELOOP}
			}
			target := n.target
			if !path.IsAbs(target) {
				target = path.Join(cur, target)
			}
			elems = append(strings.Split(strings.TrimPrefix(path.Clean(target), "/"), "/"), elems[i+1:]...)
			cur, i = "/", -1
			continue
		}
		if !n.isDir && i < len(elems)-1 {
			return "", nil, &fs.PathError{Op: op, Path: p, Err: syscall.ENOTDIR}
		}
		cur = next
	}
	return cur, m.nodes[cur], nil
}

func (m *memFS) info(n *memNode) FileInfo {
	return FileInfo{Dev: 1, Ino: n.ino, Uid: n.uid, IsDir: n.isDir, IsSymlink: n.target != "", TouchTime: n.touch}
}

func (m *memFS) Stat(p string) (FileInfo, error) {
	if err := m.check("stat", p); err != nil {
		return FileInfo{}, err
	}
	_, n, err := m.resolve("stat", p, true)
	if err != nil {
		return FileInfo{}, err
	}
	return m.info(n), nil
}

func (m *memFS) Lstat(p string) (FileInfo, error) {
	if err := m.check("lstat", p); err != nil {
		return FileInfo{}, err
	}
	_, n, err := m.resolve("lstat", p, false)
	if err != nil {
		return FileInfo{}, err
	}
	return m.info(n), nil
}

// children returns the clean paths of the direct children of the directory at clean path dir.
func (m *memFS) children(dir string) []string {
	var rv []string
	for p := range m.nodes {
		if p != "/" && path.Dir(p) == dir {
			rv = append(rv, p)
		}
	}
	sort.Strings(rv)
	return rv
}

func (m *memFS) ReadDir(p string) ([]string, error) {
	if err := m.check("readdir", p); err != nil {
		return nil, err
	}
	rp, n, err := m.resolve("readdir", p, true)
	if err != nil {
		return nil, err
	}
	if !n.isDir {
		return nil, &fs.PathError{Op: "readdir", Path: p, Err: syscall.ENOTDIR}
	}
	var names []string
	for _, c := range m.children(rp) {
		names = append(names, path.Base(c))
	}
	return names, nil
}

func (m *memFS) MkdirAll(p string, perm fs.FileMode) error {
	if err := m.check("mkdir", p); err != nil {
		return err
	}
	if perm != 0o775 {
		panic("unexpected permissions")
	}
	rp, n, err := m.resolve("mkdir", p, true)
	if err == nil {
		if !n.isDir {
			return &fs.PathError{Op: "mkdir", Path: p, Err: syscall.ENOTDIR}
		}
		return nil
	}
	parent := path.Dir(path.Clean(p))
	if err := m.MkdirAll(parent, perm); err != nil {
		return err
	}
	if rp, _, err = m.resolve("mkdir", parent, true); err != nil {
		return err
	}
	m.nodes[path.Join(rp, path.Base(p))] = m.newNode(true, "", m.now)
	return nil
}

func (m *memFS) Remove(p string) error {
	if err := m.check("remove", p); err != nil {
		return err
	}
	rp, n, err := m.resolve("remove", p, false)
	if err != nil {
		return err
	}
	if n.isDir && len(m.children(rp)) > 0 {
		return &fs.PathError{Op: "remove", Path: p, Err: syscall.ENOTEMPTY}
	}
	delete(m.nodes, rp)
	return nil
}

func (m *memFS) RemoveAll(p string) error {
	if err := m.check("removeall", p); err != nil {
		return err
	}
	rp, n, err := m.resolve("removeall", p, false)
	if err != nil {
		return nil
	}
	if n.isDir {
		for _, c := range m.children(rp) {
			m.RemoveAll(c)
		}
	}
	delete(m.nodes, rp)
	return nil
}

func (m *memFS) Move(src, dst string) error {
	if err := m.check("move", src); err != nil {
		return err
	}
	rs, _, err := m.resolve("move", src, false)
	if err != nil {
		return err
	}
	rd, _, err := m.resolve("move", path.Dir(path.Clean(dst)), true)
	if err != nil {
		return err
	}
	rd = path.Join(rd, path.Base(dst))
	if m.nodes[rd] != nil {
		return &fs.PathError{Op: "move", Path: dst, Err: syscall.EEXIST}
	}
	if strings.HasPrefix(rd, rs+"/") {
		return &fs.PathError{Op: "move", Path: dst, Err: syscall.EINVAL}
	}
	moved := map[string]*memNode{}
	for p, n := range m.nodes {
		if p == rs || strings.HasPrefix(p, rs+"/") {
			moved[rd+strings.TrimPrefix(p, rs)] = n
			delete(m.nodes, p)
		}
	}
	for p, n := range moved {
		m.nodes[p] = n
	}
	return nil
}
