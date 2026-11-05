//go:build windows
// +build windows

package timesync

import (
	"time"

	"github.com/lovemilk2333/timesync.go/timesync/core"
)

func set_system_time(t *time.Time) error {
	return core.SetTime(t)
}
