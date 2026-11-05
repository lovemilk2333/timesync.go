//go:build !windows && !linux

package timesync

import (
	"fmt"
	"time"
)

func set_system_time(*time.Time) error {
	return fmt.Errorf("setting the system clock is unsupported on this platform")
}
