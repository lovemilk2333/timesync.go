#ifndef TIMESYNC_WINDOWS_API_H
#define TIMESYNC_WINDOWS_API_H

#include <windows.h>

BOOL set_system_time(const SYSTEMTIME *system_time, DWORD *error_code);

#endif
