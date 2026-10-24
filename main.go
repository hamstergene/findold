// Command findold finds files older than a given number of days, optionally moving or deleting them.
package main

import (
	"os"
	"time"
)

func main() {
	os.Exit(run(os.Args[1:], Env{
		FS:     osFS{},
		Now:    time.Now(),
		Uid:    os.Getuid(),
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}))
}
