# `findold`: Find and remove unused files

Find, move or delete files that have not been accessed for a number of days.

The program calculates the most recent of creation/change, modification, last access and inode creation (when available) times of a file, subtracts that from now, and considers the file unused when that difference is more than specified number of days. The file can then be moved, deleted or just reported.

For directories, the most recent time of a directory and all its content (recursively) is used.

Requires Python 3.

## Example

A cron job to keep your `Downloads` folder clean:

    $ crontab -l
    @weekly ~/bin/findold ~/Downloads -q --age 14 --move PendingDelete ; ~/bin/findold ~/Downloads/PendingDelete -q --age 64 --delete

Items older than 14 days are moved into `PendingDelete` subfolder and stay there for a couple of months, just in case you recall there was something that you need. Items older than 64 days are deleted forever. If you modify any of the files, or (if your OS does update last access time) simply open it, the file's life will be prolonged for another 14 days.

As a result, the `Downloads` folder only contains the files downloaded recently, the new downloads are very easy to locate, and you never need to care about cleaning it.

## Usage

    usage: findold [-h] [-a AGE] [-v | -q] [-d | -m MOVE] [-x EXCLUDE] [--dry]
                       directory

    positional arguments:
      directory             directory to scan for unused files

    optional arguments:
      -h, --help            show this help message and exit
      -a AGE, --age AGE     minimum age in days to be considered unused (default 7
                            days)
      -v, --verbose         print file names (default when neither -d nor -m
                            specified)
      -q, --quiet           silently ignore errors
      -d, --delete          delete unused items
      -m MOVE, --move MOVE  Move unused item to this location. The location is
                            relative to <directory>. It will be created if it does
                            not exist.
      -x EXCLUDE, --exclude EXCLUDE, --ignore EXCLUDE
                            Ignore item with this name. May appear multiple times.
                            The -m argument is always ignored automatically.
      --dry, --dry-run      Don't actually do anything, just print action to be
                            taken

## Installation

macOS via Homebrew:

    brew install hamstergene/tap/findold

Or you can just download and place the script wherever you like.
