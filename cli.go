package main

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const version = "1.2.0"

// The command line mimics the Python argparse-based original, including its help text and error messages.

const usage = `usage: findold [-h] [-a AGE] [-v | -q] [-d | -m MOVE_TO] [-x EXCLUDE] [-n]
               [--version]
               directory
`

const help = usage + `
positional arguments:
  directory             directory to scan for unused files

options:
  -h, --help            show this help message and exit
  -a AGE, --age AGE     minimum age in days to be considered unused (default 7
                        days)
  -v, --verbose         print file names (default when neither -d nor -m
                        specified)
  -q, --quiet           silently ignore errors
  -d, --delete          delete unused items
  -m MOVE_TO, --move MOVE_TO
                        Move unused item to this location. The location is
                        relative to <directory>. It will be created if it does
                        not exist.
  -x EXCLUDE, --exclude EXCLUDE, --ignore EXCLUDE
                        Ignore item with this name. May appear multiple times.
                        The -m argument is always ignored automatically.
  -n, --dry, --dry-run  Don't actually do anything, just print action to be
                        taken. The output will be a valid shell script.
  --version             show program's version number and exit
`

var (
	errHelp    = errors.New("help requested")
	errVersion = errors.New("version requested")
)

type optDef struct {
	short   string   // e.g. "-a", empty if none
	long    []string // e.g. "--age"
	metavar string   // non-empty if the option takes a value
	group   int      // options in the same non-zero group are mutually exclusive
}

func (d *optDef) name() string {
	names := d.long
	if d.short != "" {
		names = append([]string{d.short}, names...)
	}
	return strings.Join(names, "/")
}

var optDefs = []*optDef{
	{short: "-h", long: []string{"--help"}},
	{short: "-a", long: []string{"--age"}, metavar: "AGE"},
	{short: "-v", long: []string{"--verbose"}, group: 1},
	{short: "-q", long: []string{"--quiet"}, group: 1},
	{short: "-d", long: []string{"--delete"}, group: 2},
	{short: "-m", long: []string{"--move"}, metavar: "MOVE_TO", group: 2},
	{short: "-x", long: []string{"--exclude", "--ignore"}, metavar: "EXCLUDE"},
	{short: "-n", long: []string{"--dry", "--dry-run"}},
	{long: []string{"--version"}},
}

func lookupShort(c byte) *optDef {
	for _, d := range optDefs {
		if d.short == "-"+string(c) {
			return d
		}
	}
	return nil
}

// lookupLong finds a long option by its name or an unambiguous prefix of it.
func lookupLong(name string) (*optDef, error) {
	var matches []string
	var match *optDef
	for _, d := range optDefs {
		for _, l := range d.long {
			if l == name {
				return d, nil
			}
			if strings.HasPrefix(l, name) {
				matches = append(matches, l)
				match = d
			}
		}
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("ambiguous option: %s could match %s", name, strings.Join(matches, ", "))
	}
	return match, nil
}

var negativeNumber = regexp.MustCompile(`^-\d+$|^-\d*\.\d+$`)

// isOption reports whether arg looks like an option rather than a value or a positional argument.
func isOption(arg string) bool {
	return len(arg) > 1 && arg[0] == '-' && !negativeNumber.MatchString(arg)
}

// parseArgs parses the command line. It returns errHelp or errVersion if the user asked for those.
func parseArgs(args []string) (Options, error) {
	opts := Options{MinAge: 7}
	var verbose, quiet, del bool
	var positional, unrecognized []string
	seen := map[int]*optDef{}

	apply := func(d *optDef, value string) error {
		if d.group != 0 {
			if prev := seen[d.group]; prev != nil && prev != d {
				return fmt.Errorf("argument %s: not allowed with argument %s", d.name(), prev.name())
			}
			seen[d.group] = d
		}
		switch d.long[0] {
		case "--help":
			return errHelp
		case "--age":
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return fmt.Errorf("argument %s: invalid int value: '%s'", d.name(), value)
			}
			opts.MinAge = n
		case "--verbose":
			verbose = true
		case "--quiet":
			quiet = true
		case "--delete":
			del = true
		case "--move":
			opts.MoveTo = value
		case "--exclude":
			opts.Exclude = append(opts.Exclude, value)
		case "--dry":
			opts.Dry = true
		case "--version":
			return errVersion
		}
		return nil
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		// takeValue returns the next argument as the value of option d.
		takeValue := func(d *optDef) (string, error) {
			if i+1 == len(args) || isOption(args[i+1]) {
				return "", fmt.Errorf("argument %s: expected one argument", d.name())
			}
			i++
			return args[i], nil
		}

		switch {
		case arg == "--":
			positional = append(positional, args[i+1:]...)
			i = len(args)

		case strings.HasPrefix(arg, "--"):
			name, value, hasValue := strings.Cut(arg, "=")
			d, err := lookupLong(name)
			if err != nil {
				return opts, err
			}
			if d == nil {
				unrecognized = append(unrecognized, arg)
				continue
			}
			if d.metavar == "" && hasValue {
				return opts, fmt.Errorf("argument %s: ignored explicit argument '%s'", d.name(), value)
			}
			if d.metavar != "" && !hasValue {
				if value, err = takeValue(d); err != nil {
					return opts, err
				}
			}
			if err := apply(d, value); err != nil {
				return opts, err
			}

		case isOption(arg):
			// A cluster of short options, such as -qd or -a14.
			for j := 1; j < len(arg); j++ {
				d := lookupShort(arg[j])
				if d == nil {
					unrecognized = append(unrecognized, arg)
					break
				}
				if d.metavar == "" {
					if err := apply(d, ""); err != nil {
						return opts, err
					}
					continue
				}
				value := arg[j+1:]
				if j == 1 {
					value = strings.TrimPrefix(value, "=")
				}
				if value == "" {
					var err error
					if value, err = takeValue(d); err != nil {
						return opts, err
					}
				}
				if err := apply(d, value); err != nil {
					return opts, err
				}
				break
			}

		default:
			positional = append(positional, arg)
		}
	}

	if len(positional) == 0 {
		return opts, errors.New("the following arguments are required: directory")
	}
	unrecognized = append(unrecognized, positional[1:]...)
	if len(unrecognized) > 0 {
		return opts, fmt.Errorf("unrecognized arguments: %s", strings.Join(unrecognized, " "))
	}

	opts.Dir = positional[0]
	if opts.Dir == "" {
		opts.Dir = "."
	}
	opts.Verbosity = 1
	if verbose {
		opts.Verbosity = 2
	}
	if quiet {
		opts.Verbosity = 0
	}
	switch {
	case del:
		opts.Action = Delete
	case opts.MoveTo != "":
		opts.Action = Move
	}
	return opts, nil
}

// run executes the command line and returns the exit status:
// 0 if anything old was found, 1 if not, and 2 on usage or fatal errors.
func run(args []string, env Env) int {
	if len(args) == 0 {
		fmt.Fprint(env.Stdout, help)
		return 1
	}
	opts, err := parseArgs(args)
	switch {
	case err == errHelp:
		fmt.Fprint(env.Stdout, help)
		return 0
	case err == errVersion:
		fmt.Fprintf(env.Stdout, "findold %s\n", version)
		return 0
	case err != nil:
		fmt.Fprintf(env.Stderr, "%sfindold: error: %v\n", usage, err)
		return 2
	}

	found, err := findOld(env, opts)
	if err != nil {
		fmt.Fprintf(env.Stderr, "findold: %v\n", err)
		return 2
	}
	if !found {
		return 1
	}
	return 0
}
