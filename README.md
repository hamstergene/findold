# `findold`: Find and remove unused files

Find, move or delete files that have not been touched for a number of days.

The program takes the most recent of the modification, status change and (on macOS) creation times of a file, subtracts that from now, and considers the file unused when that difference is at least the specified number of days. Access time is not used, because merely listing a directory may update it. The file can then be moved, deleted or just reported.

For directories, the most recent time of the directory and all its content (recursively) is used.

Works on macOS and Linux.

## Example

A cron job to keep your `Downloads` folder clean:

    $ crontab -l
    @weekly ~/go/bin/findold ~/Downloads -q --age 14 --move PendingDelete ; ~/go/bin/findold ~/Downloads/PendingDelete -q --age 64 --delete

Items older than 14 days are moved into `PendingDelete` subfolder and stay there for a couple of months, just in case you recall there was something that you need. Items older than 64 days are deleted forever. If you modify any of the files, the file's life will be prolonged for another 14 days.

As a result, the `Downloads` folder only contains the files downloaded recently, the new downloads are very easy to locate, and you never need to care about cleaning it.

## Usage

    usage: findold [-h] [-a AGE] [-v | -q] [-d | -m MOVE_TO] [-x EXCLUDE] [-n]
                   [--version]
                   directory

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

Only the items directly inside `directory` are considered; a directory is moved or deleted as a whole. Finder's `.DS_Store` and `.localized` files are always ignored, and so are items not owned by the current user (unless running as root).

Moved items never overwrite anything: if the destination already has an item with the same name, a numeric suffix is appended (`name_1`, `name_2`, ...).

Symbolic links are moved or deleted themselves, never their targets. A symbolic link is judged by its target's times, or by its own if the target is missing. Symbolic links inside directories are not descended into.

The exit status is 0 if any unused items were found, 1 if none, and 2 on usage errors or if `directory` can't be read.

## Installation

With [Go](https://go.dev/dl/) 1.22 or newer:

    go install github.com/hamstergene/findold@latest

This puts `findold` into `$(go env GOPATH)/bin`, which is `~/go/bin` by default.

## Development

    go test ./...
