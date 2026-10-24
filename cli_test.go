package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseArgs(t *testing.T) {
	def := func(edit func(o *Options)) Options {
		o := Options{Dir: "dir", MinAge: 7, Verbosity: 1}
		if edit != nil {
			edit(&o)
		}
		return o
	}
	for _, tc := range []struct {
		args []string
		want Options
	}{
		{[]string{"dir"}, def(nil)},
		{[]string{""}, def(func(o *Options) { o.Dir = "." })},

		// options before and after the directory
		{[]string{"-q", "dir", "--age", "14", "--move", "+P"}, def(func(o *Options) {
			o.Verbosity, o.MinAge, o.Action, o.MoveTo = 0, 14, Move, "+P"
		})},
		{[]string{"dir", "-v", "-d", "-n"}, def(func(o *Options) { o.Verbosity, o.Action, o.Dry = 2, Delete, true })},
		{[]string{"dir", "--verbose", "--delete", "--dry-run"}, def(func(o *Options) { o.Verbosity, o.Action, o.Dry = 2, Delete, true })},

		// value forms
		{[]string{"dir", "-a", "3"}, def(func(o *Options) { o.MinAge = 3 })},
		{[]string{"dir", "-a3"}, def(func(o *Options) { o.MinAge = 3 })},
		{[]string{"dir", "-a=3"}, def(func(o *Options) { o.MinAge = 3 })},
		{[]string{"dir", "--age=3"}, def(func(o *Options) { o.MinAge = 3 })},
		{[]string{"dir", "--age", " +3 "}, def(func(o *Options) { o.MinAge = 3 })},
		{[]string{"dir", "-a", "-3"}, def(func(o *Options) { o.MinAge = -3 })},
		{[]string{"dir", "-a", "1", "-a", "2"}, def(func(o *Options) { o.MinAge = 2 })},
		{[]string{"dir", "--move", "-"}, def(func(o *Options) { o.Action, o.MoveTo = Move, "-" })},
		{[]string{"dir", "-m", "a=b"}, def(func(o *Options) { o.Action, o.MoveTo = Move, "a=b" })},
		{[]string{"dir", "--move=a=b"}, def(func(o *Options) { o.Action, o.MoveTo = Move, "a=b" })},
		{[]string{"dir", "-m", "x", "-m", ""}, def(nil)},

		// clusters
		{[]string{"-qd", "dir"}, def(func(o *Options) { o.Verbosity, o.Action = 0, Delete })},
		{[]string{"-qna14", "dir"}, def(func(o *Options) { o.Verbosity, o.Dry, o.MinAge = 0, true, 14 })},
		{[]string{"-qm", "P", "dir"}, def(func(o *Options) { o.Verbosity, o.Action, o.MoveTo = 0, Move, "P" })},
		{[]string{"-qmP=Q", "dir"}, def(func(o *Options) { o.Verbosity, o.Action, o.MoveTo = 0, Move, "P=Q" })},

		// repeated flags are fine
		{[]string{"-v", "-v", "dir", "-d", "-d"}, def(func(o *Options) { o.Verbosity, o.Action = 2, Delete })},

		// exclusions
		{[]string{"dir", "-x", "a", "--exclude", "b", "--ignore=c", "-xd"}, def(func(o *Options) {
			o.Exclude = []string{"a", "b", "c", "d"}
		})},

		// abbreviations
		{[]string{"dir", "--ag", "3", "--verb", "--del", "--ign", "x", "--dry"}, def(func(o *Options) {
			o.MinAge, o.Verbosity, o.Action, o.Exclude, o.Dry = 3, 2, Delete, []string{"x"}, true
		})},
		{[]string{"dir", "--dry-r", "--mo", "P"}, def(func(o *Options) { o.Dry, o.Action, o.MoveTo = true, Move, "P" })},

		// positional arguments that look like options
		{[]string{"--", "-dir"}, def(func(o *Options) { o.Dir = "-dir" })},
		{[]string{"-v", "--", "--"}, def(func(o *Options) { o.Dir, o.Verbosity = "--", 2 })},
		{[]string{"-1"}, def(func(o *Options) { o.Dir = "-1" })},
		{[]string{"-.5"}, def(func(o *Options) { o.Dir = "-.5" })},
		{[]string{"-"}, def(func(o *Options) { o.Dir = "-" })},
	} {
		got, err := parseArgs(tc.args)
		if err != nil {
			t.Errorf("parseArgs(%q): %v", tc.args, err)
		} else if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("parseArgs(%q) = %+v, want %+v", tc.args, got, tc.want)
		}
	}
}

func TestParseArgsErrors(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"-v"}, "the following arguments are required: directory"},
		{[]string{"--", "a", "b"}, "unrecognized arguments: b"},
		{[]string{"a", "b", "c"}, "unrecognized arguments: b c"},
		{[]string{"a", "--foo", "-z", "b"}, "unrecognized arguments: --foo -z b"},
		{[]string{"a", "-vz"}, "unrecognized arguments: -vz"},
		{[]string{"a", "--d"}, "ambiguous option: --d could match --delete, --dry, --dry-run"},
		{[]string{"a", "--dr"}, "ambiguous option: --dr could match --dry, --dry-run"},
		{[]string{"a", "-a"}, "argument -a/--age: expected one argument"},
		{[]string{"a", "--age"}, "argument -a/--age: expected one argument"},
		{[]string{"a", "-m", "-v"}, "argument -m/--move: expected one argument"},
		{[]string{"a", "-a", "x"}, "argument -a/--age: invalid int value: 'x'"},
		{[]string{"a", "-a", "1.5"}, "argument -a/--age: invalid int value: '1.5'"},
		{[]string{"a", "--delete=yes"}, "argument -d/--delete: ignored explicit argument 'yes'"},
		{[]string{"a", "-v", "-q"}, "argument -q/--quiet: not allowed with argument -v/--verbose"},
		{[]string{"a", "-qv"}, "argument -v/--verbose: not allowed with argument -q/--quiet"},
		{[]string{"a", "-d", "-m", "x"}, "argument -m/--move: not allowed with argument -d/--delete"},
		{[]string{"a", "--move", "x", "--delete"}, "argument -d/--delete: not allowed with argument -m/--move"},
		{[]string{"a", "-h", "-v", "-q"}, errHelp.Error()},
		{[]string{"-v", "-q", "-h"}, "argument -q/--quiet: not allowed with argument -v/--verbose"},
		{[]string{"--he"}, errHelp.Error()},
		{[]string{"--version", "-z"}, errVersion.Error()},
	} {
		_, err := parseArgs(tc.args)
		if err == nil || err.Error() != tc.want {
			t.Errorf("parseArgs(%q) error = %v, want %s", tc.args, err, tc.want)
		}
	}
}

func TestRunCommandLine(t *testing.T) {
	expect(t, runMem(newMemFS()), result{1, help, ""})
	expect(t, runMem(newMemFS(), "-h"), result{0, help, ""})
	expect(t, runMem(newMemFS(), "/", "--help"), result{0, help, ""})
	expect(t, runMem(newMemFS(), "--version"), result{0, "findold " + version + "\n", ""})
	expect(t, runMem(newMemFS(), "/", "-v", "-q"), result{2, "", usage + "findold: error: argument -q/--quiet: not allowed with argument -v/--verbose\n"})
}

func TestHelpDescribesAllOptions(t *testing.T) {
	for _, d := range optDefs {
		names := append([]string{d.short}, d.long...)
		for _, n := range names {
			if n != "" && !strings.Contains(help, " "+n) {
				t.Errorf("help doesn't mention %s", n)
			}
		}
	}
}
