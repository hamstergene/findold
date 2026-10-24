package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	names, err := osFS{}.ReadDir(dir)
	must(t, err)
	return names
}

// TestCleanupScenario runs the README's example against the real filesystem.
func TestCleanupScenario(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "old.txt"), "")
	mustWrite(t, filepath.Join(dir, "old dir/file"), "")
	mustWrite(t, filepath.Join(dir, "recent.txt"), "")
	mustWrite(t, filepath.Join(dir, "recent dir/old"), "")
	mustWrite(t, filepath.Join(dir, "recent dir/recent"), "")
	mustWrite(t, filepath.Join(dir, ".DS_Store"), "")

	// Files can't be made older than now: setting times updates the status change time.
	// So pretend that it's later than it is, and make the recent files recent by dating them into the future.
	now := time.Now().Add(30 * 24 * time.Hour)
	for _, name := range []string{"recent.txt", "recent dir/recent"} {
		must(t, os.Chtimes(filepath.Join(dir, name), time.Time{}, now.Add(-24*time.Hour)))
	}
	var stdout, stderr strings.Builder
	env := Env{FS: osFS{}, Now: now, Uid: os.Getuid(), Stdout: &stdout, Stderr: &stderr}

	code := run([]string{dir, "-q", "--age", "14", "--move", "PendingDelete"}, env)
	if code != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("move: exit %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	if got, want := listDir(t, dir), []string{".DS_Store", "PendingDelete", "recent dir", "recent.txt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("after moving: %q, want %q", got, want)
	}
	if got, want := listDir(t, filepath.Join(dir, "PendingDelete")), []string{"old dir", "old.txt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("moved: %q, want %q", got, want)
	}

	// The moved items are only as old as the real now: that is their status change time.
	pending := filepath.Join(dir, "PendingDelete")
	env.Now = now.Add(30 * 24 * time.Hour)
	if code := run([]string{pending, "-q", "--age", "64", "--delete"}, env); code != 1 {
		t.Errorf("premature delete: exit %d", code)
	}
	env.Now = now.Add(60 * 24 * time.Hour)
	if code := run([]string{pending, "-d", "-a", "64"}, env); code != 0 {
		t.Errorf("delete: exit %d", code)
	}
	if got := listDir(t, pending); len(got) != 0 {
		t.Errorf("not deleted: %q", got)
	}
	want := pending + "/old dir\n" + pending + "/old.txt\n"
	if stdout.String() != want || stderr.Len() != 0 {
		t.Errorf("delete: stdout %q, want %q; stderr %q", stdout.String(), want, stderr.String())
	}
}

// TestMainFunc runs main in a subprocess to check that it's wired to the real environment.
func TestMainFunc(t *testing.T) {
	if args := os.Getenv("FINDOLD_TEST_ARGS"); args != "" {
		os.Args = append([]string{"findold"}, strings.Split(args, "\n")...)
		main()
		return
	}

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "file"), "")
	runMain := func(args ...string) (int, string) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestMainFunc$")
		cmd.Env = append(os.Environ(), "FINDOLD_TEST_ARGS="+strings.Join(args, "\n"))
		out, err := cmd.Output()
		if err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				return exit.ExitCode(), string(out)
			}
			t.Fatal(err)
		}
		return 0, string(out)
	}
	if code, out := runMain(dir, "-a", "0", "-d"); code != 0 || out != dir+"/file\n" {
		t.Errorf("exit %d, output %q", code, out)
	}
	if _, err := os.Lstat(filepath.Join(dir, "file")); !isNotExist(err) {
		t.Errorf("file not deleted: %v", err)
	}
	if code, out := runMain(dir); code != 1 || out != "" {
		t.Errorf("exit %d, output %q", code, out)
	}
}
