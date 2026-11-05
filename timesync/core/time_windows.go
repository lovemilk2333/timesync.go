//go:build windows
// +build windows

package core

/*
#include "windows_api.h"
*/
import "C"

import (
	"fmt"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

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

	var system_time C.SYSTEMTIME
	system_time.wYear = C.WORD(utc.Year())
	system_time.wMonth = C.WORD(utc.Month())
	system_time.wDayOfWeek = C.WORD(utc.Weekday())
	system_time.wDay = C.WORD(utc.Day())
	system_time.wHour = C.WORD(utc.Hour())
	system_time.wMinute = C.WORD(utc.Minute())
	system_time.wSecond = C.WORD(utc.Second())
	system_time.wMilliseconds = C.WORD(utc.Nanosecond() / int(time.Millisecond))

	var last_error C.DWORD
	if C.set_system_time(&system_time, &last_error) == 0 {
		return fmt.Errorf("SetSystemTime failed: %w", windows.Errno(last_error))
	}
	return nil
}
