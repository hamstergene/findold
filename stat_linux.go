package main

import (
	"syscall"
	"time"
)

// Linux stat has no creation time, but it can't be later than the status change time anyway.
func statTouchTime(st *syscall.Stat_t) time.Time {
	return latest(timespec(st.Mtim), timespec(st.Ctim))
}

func statAccessTime(st *syscall.Stat_t) time.Time {
	return timespec(st.Atim)
}
