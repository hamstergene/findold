# `find-unused`: Find and remove unused files

Find, move or delete files that were not accessed for a number of days.

The program calculates the most recent of creation/change, modification, last access and inode creation (when available) times of a file, subtracts that from now, and considers the file unused when that difference is more than specified number of days. The file can be moved, deleted or simply listed then.

For directories, the most recent time of a directory and all its content (recursively) is used.

## Example

A cron job to keep your `Downloads` folder clean:

    $ crontab -l
    @weekly ~/bin/find-unused ~/Downloads -q --age 14 --move PendingDelete -x PendingDelete ; ~/bin/find-unused ~/Downloads/PendingDelete -q --age 64 --delete

Items older than 14 days are moved into `PendingDelete` subfolder and stay there for a couple of months, just in case you recall there was something that you need. Items older than 64 days are deleted forever. If you modify any of the files, or (if your OS does update last access time) simply open it, the file's life will be prolonged for another 14 days.

As a consequence, the `Downloads` folder only contains the files downloaded recently, the new downloads are very easy to locate, and you never need to care about cleaning it.

## Usage

    usage: find-unused [-h] [-a AGE] [-q] [-qq] [-d | -m MOVE] [-x EXCLUDE]
                        directory

    positional arguments:
      directory             directory to scan for unused files

    optional arguments:
      -h, --help            show this help message and exit
      -a AGE, --age AGE     minimum age in days to be considered unused (default 7
                            days)
      -q, --quiet           don't print file names
      -qq, --quieter        don't print file names and ignore errors
      -d, --delete          delete unused items
      -m MOVE, --move MOVE  Move unused item to this location. The location is
                            relative to <directory>. It will be created if it does
                            not exist.
      -x EXCLUDE, --exclude EXCLUDE, --ignore EXCLUDE
                            Ignore item with this name. May appear multiple times.

