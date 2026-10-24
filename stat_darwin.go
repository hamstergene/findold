package main

import (
	"syscall"
	"time"
)

func statTouchTime(st *syscall.Stat_t) time.Time {
	return latest(timespec(st.Mtimespec), timespec(st.Ctimespec), timespec(st.Birthtimespec))
}

func statAccessTime(st *syscall.Stat_t) time.Time {
	return timespec(st.Atimespec)
}
