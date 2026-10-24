package main

import (
	"math"
	"strings"
	"syscall"
	"testing"
	"time"
)

var testNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

const testUid = 501

// ago returns the time the given number of days before testNow.
func ago(days float64) time.Time {
	return testNow.Add(-time.Duration(days * float64(24*time.Hour)))
}

type result struct {
	code           int
	stdout, stderr string
}

func runAs(m *memFS, uid int, args ...string) result {
	var stdout, stderr strings.Builder
	code := run(args, Env{FS: m, Now: testNow, Uid: uid, Stdout: &stdout, Stderr: &stderr})
	return result{code, stdout.String(), stderr.String()}
}

func runMem(m *memFS, args ...string) result { return runAs(m, testUid, args...) }

func lines(ls ...string) string {
	if len(ls) == 0 {
		return ""
	}
	return strings.Join(ls, "\n") + "\n"
}

func expect(t *testing.T, got, want result) {
	t.Helper()
	if got.code != want.code {
		t.Errorf("exit code = %d, want %d", got.code, want.code)
	}
	if got.stdout != want.stdout {
		t.Errorf("stdout:\n%s\nwant:\n%s", got.stdout, want.stdout)
	}
	if got.stderr != want.stderr {
		t.Errorf("stderr:\n%s\nwant:\n%s", got.stderr, want.stderr)
	}
}

func expectExist(t *testing.T, m *memFS, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if !m.exists(p) {
			t.Errorf("%s does not exist", p)
		}
	}
}

func expectGone(t *testing.T, m *memFS, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if m.exists(p) {
			t.Errorf("%s still exists", p)
		}
	}
}

// sampleTree has a mix of old and recent items in /d.
func sampleTree() *memFS {
	m := newMemFS()
	m.dir("/d", ago(100))
	m.file("/d/old", ago(10))
	m.file("/d/recent", ago(1.5))
	m.file("/d/.DS_Store", ago(100))
	m.file("/d/.localized", ago(100))
	m.dir("/d/olddir", ago(30))
	m.file("/d/olddir/sub/deep", ago(20))
	m.dir("/d/livedir", ago(30))
	m.file("/d/livedir/sub/fresh", ago(2)) // keeps livedir alive
	return m
}

func TestReport(t *testing.T) {
	m := sampleTree()
	expect(t, runMem(m, "/d"), result{0, lines("/d/old", "/d/olddir"), ""})
	expectExist(t, m, "/d/old", "/d/olddir/sub/deep")

	// Dry run doesn't change reporting.
	expect(t, runMem(m, "/d", "-n"), result{0, lines("/d/old", "/d/olddir"), ""})
}

func TestReportVerbose(t *testing.T) {
	m := sampleTree()
	expect(t, runMem(m, "/d", "-v"), result{0, lines(
		"2.0 /d/livedir",
		"10.0 /d/old -- OLD",
		"20.0 /d/olddir -- OLD",
		"1.5 /d/recent",
	), ""})
}

func TestReportQuiet(t *testing.T) {
	expect(t, runMem(sampleTree(), "/d", "-q"), result{0, "", ""})
}

func TestNothingOld(t *testing.T) {
	m := newMemFS()
	m.file("/d/recent", ago(1))
	expect(t, runMem(m, "/d"), result{1, "", ""})

	expect(t, runMem(newMemFS(), "/"), result{1, "", ""})
}

func TestAgeThreshold(t *testing.T) {
	m := newMemFS()
	m.file("/d/exactly7", ago(7))
	m.file("/d/almost7", ago(6.99))
	m.file("/d/now", testNow)
	m.file("/d/future", ago(-1))
	expect(t, runMem(m, "/d"), result{0, lines("/d/exactly7"), ""})
	expect(t, runMem(m, "/d", "-a", "0"), result{0, lines("/d/almost7", "/d/exactly7", "/d/now"), ""})
	expect(t, runMem(m, "/d", "-a", "8"), result{1, "", ""})
	expect(t, runMem(m, "/d", "-a", "-1"), result{0, lines("/d/almost7", "/d/exactly7", "/d/future", "/d/now"), ""})
}

func TestExclude(t *testing.T) {
	m := newMemFS()
	m.file("/d/a", ago(10))
	m.file("/d/b", ago(10))
	m.file("/d/c", ago(10))
	m.file("/d/d", ago(10))
	m.file("/d/item", ago(10))
	m.hardlink("/d/alias", "/d/item")
	expect(t, runMem(m, "/d", "-x", "a", "--exclude=b", "--ignore", "c", "-x", "alias", "-x", "missing"),
		result{0, lines("/d/d"), ""})
}

func TestNotOwned(t *testing.T) {
	m := newMemFS()
	m.file("/d/theirs", ago(10)).uid = 0
	m.file("/t", ago(10)).uid = 0
	m.link("/d/link-to-theirs", "/t", ago(10)) // judged by its target, like the original
	expect(t, runMem(m, "/d"), result{1, "", ""})
	expect(t, runMem(m, "/d", "-v"), result{1, lines("-1 /d/link-to-theirs -- not owned", "-1 /d/theirs -- not owned"), ""})
	expect(t, runMem(m, "/d", "-q", "-n", "-d"), result{1, lines("# ignored (not owned): /d/link-to-theirs", "# ignored (not owned): /d/theirs"), ""})

	// root owns everything
	expect(t, runAs(m, 0, "/d", "-d"), result{0, lines("/d/link-to-theirs", "/d/theirs"), ""})
	expectGone(t, m, "/d/theirs", "/d/link-to-theirs")
	expectExist(t, m, "/t")
}

func deleteTree() *memFS {
	m := newMemFS()
	m.dir("/d", ago(100))
	m.file("/d/oldfile", ago(10))
	m.dir("/d/olddir", ago(10))
	m.file("/d/olddir/a", ago(10))
	m.dir("/t", ago(10))
	m.file("/t/x", ago(10))
	m.link("/d/dirlink", "/t", ago(10))
	m.file("/d/recent", ago(1.75))
	return m
}

func TestDelete(t *testing.T) {
	m := deleteTree()
	expect(t, runMem(m, "/d", "-d"), result{0, lines("/d/dirlink", "/d/olddir", "/d/oldfile"), ""})
	expectGone(t, m, "/d/dirlink", "/d/olddir", "/d/olddir/a", "/d/oldfile")
	expectExist(t, m, "/d/recent", "/t/x") // symlinked directory's contents survive
}

func TestDeleteVerbose(t *testing.T) {
	m := deleteTree()
	expect(t, runMem(m, "/d", "--delete", "--verbose"), result{0, lines(
		"10.0 /d/dirlink -- DELETED",
		"10.0 /d/olddir -- DELETED",
		"10.0 /d/oldfile -- DELETED",
		"1.75 /d/recent",
	), ""})
}

func TestDeleteQuiet(t *testing.T) {
	m := deleteTree()
	expect(t, runMem(m, "/d", "-qd"), result{0, "", ""})
	expectGone(t, m, "/d/dirlink", "/d/olddir", "/d/oldfile")
}

func TestDeleteDry(t *testing.T) {
	m := deleteTree()
	m.file("/d/it's old", ago(10))
	m.dir("/d/my dir", ago(10))
	m.file("/d/-dash", ago(10))
	want := lines(
		"rm /d/-dash",
		"rm /d/dirlink",
		`rm '/d/it'"'"'s old'`,
		"rm -r '/d/my dir'",
		"rm -r /d/olddir",
		"rm /d/oldfile",
	)
	expect(t, runMem(m, "/d", "-d", "-n"), result{0, want, ""})
	expect(t, runMem(m, "/d", "-d", "--dry-run", "-q"), result{0, want, ""})
	expectExist(t, m, "/d/dirlink", "/d/olddir/a", "/d/oldfile", "/d/it's old", "/d/my dir", "/d/-dash")

	expect(t, runMem(m, "/d", "-d", "-n", "-v", "-x", "my dir", "-x", "it's old", "--exclude=-dash"), result{0, lines(
		"rm /d/dirlink",
		"rm -r /d/olddir",
		"rm /d/oldfile",
		"# skip (age=1) /d/recent",
	), ""})
}

func TestDeleteErrors(t *testing.T) {
	m := deleteTree()
	m.fail("remove", "/d/oldfile", syscall.EACCES)
	m.fail("removeall", "/d/olddir", syscall.EPERM)
	expect(t, runMem(m, "/d", "-d"), result{0, lines("/d/dirlink"), lines(
		"/d/olddir : removeall /d/olddir: operation not permitted",
		"/d/oldfile : remove /d/oldfile: permission denied",
	)})
	expectGone(t, m, "/d/dirlink")
	expectExist(t, m, "/d/olddir", "/d/oldfile")

	expect(t, runMem(m, "/d", "-d", "-q"), result{0, "", ""})
}

func moveTree() *memFS {
	m := newMemFS()
	m.dir("/d", ago(100))
	m.file("/d/a", ago(10))
	m.file("/d/b", ago(10))
	m.file("/d/recent", ago(1))
	return m
}

func TestMove(t *testing.T) {
	m := moveTree()
	expect(t, runMem(m, "/d", "-m", "+Pending"), result{0, lines("/d/a -> /d/+Pending/a", "/d/b -> /d/+Pending/b"), ""})
	expectGone(t, m, "/d/a", "/d/b")
	expectExist(t, m, "/d/+Pending/a", "/d/+Pending/b", "/d/recent")
	if n := m.nodes["/d/+Pending"]; !n.isDir || !n.touch.Equal(testNow) {
		t.Errorf("destination was not created as a new directory: %+v", n)
	}

	// The destination is old now, but it is never moved into itself.
	m.nodes["/d/+Pending"].touch = ago(50)
	expect(t, runMem(m, "/d", "-m", "+Pending"), result{1, "", ""})
}

func TestMoveExistingNames(t *testing.T) {
	m := moveTree()
	m.dir("/d/P", ago(100))
	m.file("/d/P/a", ago(50))
	m.file("/d/P/a_1", ago(50))
	m.link("/d/P/b", "/nowhere", ago(50)) // a dangling symlink still counts as existing
	expect(t, runMem(m, "/d", "-m", "P"), result{0, lines("/d/a -> /d/P/a_2", "/d/b -> /d/P/b_1"), ""})
	expectExist(t, m, "/d/P/a", "/d/P/a_1", "/d/P/a_2", "/d/P/b_1")
}

func TestMoveVerbose(t *testing.T) {
	m := moveTree()
	expect(t, runMem(m, "/d", "-v", "--move=P"), result{0, lines(
		"10.0 /d/a -> /d/P/a",
		"10.0 /d/b -> /d/P/b",
		"1.0 /d/recent",
	), ""})
}

func TestMoveQuiet(t *testing.T) {
	m := moveTree()
	expect(t, runMem(m, "/d", "-qmP"), result{0, "", ""})
	expectExist(t, m, "/d/P/a", "/d/P/b")
}

func TestMoveNothingOld(t *testing.T) {
	m := newMemFS()
	m.file("/d/recent", ago(1))
	expect(t, runMem(m, "/d", "-m", "P"), result{1, "", ""})
	expectGone(t, m, "/d/P") // created only on demand
}

func TestMoveNested(t *testing.T) {
	m := moveTree()
	m.dir("/d/x", ago(100)) // old, but contains the destination
	expect(t, runMem(m, "/d", "-m", "x/y/../y/"), result{0, lines("/d/a -> /d/x/y/../y/a", "/d/b -> /d/x/y/../y/b"), ""})
	expectExist(t, m, "/d/x/y/a", "/d/x/y/b")
}

func TestMoveOutside(t *testing.T) {
	m := moveTree()
	m.dir("/d/elsewhere", ago(100)) // not the destination
	expect(t, runMem(m, "/d", "-m", "/elsewhere"), result{0, lines(
		"/d/a -> /elsewhere/a",
		"/d/b -> /elsewhere/b",
		"/d/elsewhere -> /elsewhere/elsewhere",
	), ""})

	m = moveTree()
	expect(t, runMem(m, "/d", "-m", "../e"), result{0, lines("/d/a -> /d/../e/a", "/d/b -> /d/../e/b"), ""})
	expectExist(t, m, "/e/a", "/e/b")
}

func TestMoveDestinationByIdentity(t *testing.T) {
	m := moveTree()
	m.dir("/d/P", ago(100))
	m.link("/d/alias", "P", ago(100)) // the destination is excluded under any name
	expect(t, runMem(m, "/d", "-m", "alias"), result{0, lines("/d/a -> /d/alias/a", "/d/b -> /d/alias/b"), ""})
	expectExist(t, m, "/d/P/a", "/d/P/b")
}

func TestMoveDry(t *testing.T) {
	m := moveTree()
	m.file("/d/a b", ago(10))
	expect(t, runMem(m, "/d", "-m", "+P", "-n"), result{0, lines(
		"mkdir -p /d/+P",
		"mv /d/a /d/+P/a",
		"mv '/d/a b' '/d/+P/a b'",
		"mv /d/b /d/+P/b",
	), ""})
	expectGone(t, m, "/d/+P")
	expectExist(t, m, "/d/a", "/d/b")

	// Names taken earlier in the dry run are accounted for.
	m = newMemFS()
	m.dir("/d/P", ago(100))
	m.file("/d/P/a", ago(50))
	m.file("/d/a", ago(10))
	m.file("/d/a_1", ago(10))
	m.file("/d/recent", ago(3.5))
	expect(t, runMem(m, "/d", "-m", "P", "-n", "-v"), result{0, lines(
		"mv /d/a /d/P/a_1",
		"mv /d/a_1 /d/P/a_1_1",
		"# skip (age=3) /d/recent",
	), ""})
}

func TestMoveErrors(t *testing.T) {
	m := moveTree()
	m.fail("mkdir", "/d/P", syscall.EACCES)
	expect(t, runMem(m, "/d", "-m", "P"), result{0, "", lines(
		"/d/a : mkdir /d/P: permission denied",
		"/d/b : mkdir /d/P: permission denied",
	)})
	expectExist(t, m, "/d/a", "/d/b")

	m = moveTree()
	m.fail("move", "/d/a", syscall.EACCES)
	expect(t, runMem(m, "/d", "-m", "P"), result{0, lines("/d/b -> /d/P/b"), lines("/d/a : move /d/a: permission denied")})
	expect(t, runMem(m, "/d", "-m", "P", "-q"), result{0, "", ""})
}

func TestMoveEmptyIsReport(t *testing.T) {
	m := moveTree()
	expect(t, runMem(m, "/d", "-m", ""), result{0, lines("/d/a", "/d/b"), ""})
}

func TestSymlinks(t *testing.T) {
	m := newMemFS()
	m.dir("/d", ago(100))

	// Top-level symlinks are judged by their targets, including directory contents.
	m.dir("/t1", ago(10))
	m.file("/t1/fresh", ago(1))
	m.link("/d/to-live-dir", "/t1", ago(10))
	m.file("/t2", ago(10))
	m.link("/d/new-link-to-old-file", "/t2", ago(0))

	// Unless they can't be followed.
	m.link("/d/dangling-old", "/nowhere", ago(10))
	m.link("/d/dangling-recent", "/nowhere", ago(1))
	m.link("/d/loop", "loop", ago(10))

	// Nested symlinks count with the target's times but aren't descended into.
	m.dir("/d/cycle", ago(10))
	m.link("/d/cycle/parent", "..", ago(10))
	m.dir("/t3", ago(10))
	m.file("/t3/fresh", ago(1))
	m.link("/d/nested-to-live-dir/l", "/t3", ago(10))
	m.file("/t4", ago(1))
	m.link("/d/nested-to-recent-file/l", "/t4", ago(10))
	m.link("/d/nested-dangling-recent/l", "/nowhere", ago(1))

	expect(t, runMem(m, "/d", "-v"), result{0, lines(
		"10.0 /d/cycle -- OLD",
		"10.0 /d/dangling-old -- OLD",
		"1.0 /d/dangling-recent",
		"10.0 /d/loop -- OLD",
		"1.0 /d/nested-dangling-recent",
		"10.0 /d/nested-to-live-dir -- OLD",
		"1.0 /d/nested-to-recent-file",
		"10.0 /d/new-link-to-old-file -- OLD",
		"1.0 /d/to-live-dir",
	), ""})
}

func TestItemErrors(t *testing.T) {
	m := newMemFS()
	m.dir("/d", ago(100))
	m.dir("/d/locked", ago(10))
	m.dir("/d/locked/sub", ago(10))
	m.fail("readdir", "/d/locked/sub", syscall.EACCES)
	m.dir("/d/unstatable", ago(10))
	m.file("/d/unstatable/x", ago(10))
	m.fail("lstat", "/d/unstatable/x", syscall.EIO)
	m.file("/d/vanishing", ago(10))
	m.fail("lstat", "/d/vanishing", syscall.ENOENT)
	m.dir("/d/vanished-inside", ago(10))
	m.file("/d/vanished-inside/x", ago(1))
	m.fail("lstat", "/d/vanished-inside/x", syscall.ENOENT)
	m.file("/d/z", ago(10))

	// Items that can't be fully examined are left alone.
	expect(t, runMem(m, "/d", "-d"), result{0, lines("/d/vanished-inside", "/d/z"), lines(
		"/d/locked : readdir /d/locked/sub: permission denied",
		"/d/unstatable : lstat /d/unstatable/x: input/output error",
		"/d/vanishing : lstat /d/vanishing: no such file or directory",
	)})
	expectExist(t, m, "/d/locked/sub", "/d/unstatable/x")
	expectGone(t, m, "/d/z", "/d/vanished-inside")
}

func TestDirectoryErrors(t *testing.T) {
	m := newMemFS()
	m.dir("/d", ago(100))
	m.fail("readdir", "/d", syscall.EACCES)
	expect(t, runMem(m, "/d", "-q"), result{2, "", lines("findold: readdir /d: permission denied")})
	expect(t, runMem(m, "/missing"), result{2, "", lines("findold: readdir /missing: no such file or directory")})
	m.file("/f", ago(10))
	expect(t, runMem(m, "/f"), result{2, "", lines("findold: readdir /f: not a directory")})
}

func TestShellQuote(t *testing.T) {
	for s, want := range map[string]string{
		"":                       "''",
		"plain":                  "plain",
		"/a/b_c-d.e@f%g+h=i:j,k": "/a/b_c-d.e@f%g+h=i:j,k",
		"a b":                    "'a b'",
		"it's":                   `'it'"'"'s'`,
		"$HOME":                  "'$HOME'",
		"tab\there":              "'tab\there'",
		"ünïcode":                "'ünïcode'",
		"*":                      "'*'",
	} {
		if got := shellQuote(s); got != want {
			t.Errorf("shellQuote(%q) = %s, want %s", s, got, want)
		}
	}
}

func TestPyJoin(t *testing.T) {
	for _, tc := range []struct{ dir, name, want string }{
		{".", "-x", "./-x"},
		{"a", "b", "a/b"},
		{"a/", "b", "a/b"},
		{"", "b", "b"},
		{"a", "/abs", "/abs"},
		{"a/../b", "c", "a/../b/c"},
	} {
		if got := pyJoin(tc.dir, tc.name); got != tc.want {
			t.Errorf("pyJoin(%q, %q) = %q, want %q", tc.dir, tc.name, got, tc.want)
		}
	}
}

func TestContainingItem(t *testing.T) {
	for p, want := range map[string]string{
		"P":       "P",
		"./P/":    "P",
		"P/Q":     "P",
		"P/../Q":  "Q",
		".":       "",
		"..":      "",
		"../P":    "",
		"/abs/P":  "",
		"P/../..": "",
	} {
		if got := containingItem(p); got != want {
			t.Errorf("containingItem(%q) = %q, want %q", p, got, want)
		}
	}
}

func TestFormatAge(t *testing.T) {
	for age, want := range map[float64]string{
		0:                  "0.0",
		14:                 "14.0",
		1.5:                "1.5",
		-0.25:              "-0.25",
		3.2083449074074073: "3.2083449074074073",
		0.0001:             "0.0001",
		0.00005:            "5e-05",
		-0.00005:           "-5e-05",
		1e16:               "1e+16",
		123456789:          "123456789.0",
	} {
		if got := formatAge(age); got != want {
			t.Errorf("formatAge(%v) = %s, want %s", age, got, want)
		}
	}
}

func TestDays(t *testing.T) {
	if got := days(testNow, ago(1.25)); got != 1.25 {
		t.Errorf("days = %v, want 1.25", got)
	}
	if got := days(testNow, testNow.Add(-1500*time.Millisecond)); math.Abs(got*86400-1.5) > 1e-9 {
		t.Errorf("days = %v, want 1.5 seconds", got)
	}
	// time.Sub would saturate at about 292 years.
	if got, want := days(testNow, time.Time{}), 739889.5; got != want {
		t.Errorf("days since year 1 = %v, want %v", got, want)
	}
}
