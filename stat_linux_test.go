package main

import (
	"syscall"
	"testing"
	"time"
)

func TestStatTouchTime(t *testing.T) {
	ts := func(sec int64) syscall.Timespec { return syscall.NsecToTimespec(sec * 1e9) }
	for _, st := range []syscall.Stat_t{
		{Atim: ts(9), Mtim: ts(4), Ctim: ts(2)},
		{Atim: ts(9), Mtim: ts(2), Ctim: ts(4)},
	} {
		if got := statTouchTime(&st); !got.Equal(time.Unix(4, 0)) {
			t.Errorf("statTouchTime(%+v) = %v, want 4s", st, got.Unix())
		}
		if got := statAccessTime(&st); !got.Equal(time.Unix(9, 0)) {
			t.Errorf("statAccessTime(%+v) = %v, want 9s", st, got.Unix())
		}
	}
}
