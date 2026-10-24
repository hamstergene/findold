package main

import (
	"syscall"
	"testing"
	"time"
)

func TestStatTouchTime(t *testing.T) {
	ts := func(sec int64) syscall.Timespec { return syscall.NsecToTimespec(sec * 1e9) }
	for _, st := range []syscall.Stat_t{
		{Atimespec: ts(9), Mtimespec: ts(4), Ctimespec: ts(2), Birthtimespec: ts(1)},
		{Atimespec: ts(9), Mtimespec: ts(1), Ctimespec: ts(4), Birthtimespec: ts(2)},
		{Atimespec: ts(9), Mtimespec: ts(2), Ctimespec: ts(1), Birthtimespec: ts(4)},
	} {
		if got := statTouchTime(&st); !got.Equal(time.Unix(4, 0)) {
			t.Errorf("statTouchTime(%+v) = %v, want 4s", st, got.Unix())
		}
		if got := statAccessTime(&st); !got.Equal(time.Unix(9, 0)) {
			t.Errorf("statAccessTime(%+v) = %v, want 9s", st, got.Unix())
		}
	}
}
