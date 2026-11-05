//go:build linux
// +build linux

package timesync

import (
	"time"

	"golang.org/x/sys/unix"
)

func set_system_time(t *time.Time) error {
	return unix.ClockSettime(unix.CLOCK_REALTIME, &unix.Timespec{
		Sec:  t.Unix(),
		Nsec: int64(t.Nanosecond()),
	})
}
