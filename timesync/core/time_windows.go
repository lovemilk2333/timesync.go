//go:build windows
// +build windows

package core

import (
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

type system_time struct {
	Year         uint16
	Month        uint16
	DayOfWeek    uint16
	Day          uint16
	Hour         uint16
	Minute       uint16
	Second       uint16
	Milliseconds uint16
}

var set_system_time_proc = syscall.NewLazyDLL("kernel32.dll").NewProc("SetSystemTime")

func IsRTCUTC() (bool, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\TimeZoneInformation`, registry.QUERY_VALUE)
	if err != nil {
		return false, err
	}

	defer k.Close()
	val, _, err := k.GetIntegerValue("RealTimeIsUniversal")

	if err != nil {
		if err == registry.ErrNotExist {
			// not exist means use local timezone time for RTC
			return false, nil
		} else {
			return false, err
		}
	}

	return val != 0, nil
}

// https://learn.microsoft.com/en-us/windows/win32/api/sysinfoapi/nf-sysinfoapi-setsystemtime
/*
set windows time from `t`. RTC to local timezone if `local_timezone`
*/
func SetTime(t *time.Time) error {
	if t == nil {
		return fmt.Errorf("set system time: time is nil")
	}

	// SetSystemTime accepts UTC. Its interpretation of the RTC is handled by
	// Windows, so local_timezone does not change the instant passed to it.
	utc := t.UTC()
	if year := utc.Year(); year < 1601 || year > 30827 {
		return fmt.Errorf("set system time: year %d is outside the SYSTEMTIME range", year)
	}

	st := system_time{
		Year:         uint16(utc.Year()),
		Month:        uint16(utc.Month()),
		DayOfWeek:    uint16(utc.Weekday()),
		Day:          uint16(utc.Day()),
		Hour:         uint16(utc.Hour()),
		Minute:       uint16(utc.Minute()),
		Second:       uint16(utc.Second()),
		Milliseconds: uint16(utc.Nanosecond() / int(time.Millisecond)),
	}
	result, _, call_err := set_system_time_proc.Call(uintptr(unsafe.Pointer(&st)))
	if result == 0 {
		if errno, ok := call_err.(syscall.Errno); ok && errno != 0 {
			return fmt.Errorf("SetSystemTime failed: %w", windows.Errno(errno))
		}
		return fmt.Errorf("SetSystemTime failed: %w", windows.ERROR_GEN_FAILURE)
	}
	runtime.KeepAlive(&st)
	return nil
}
