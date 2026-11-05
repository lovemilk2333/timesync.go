#include "windows_api.h"

BOOL set_system_time(const SYSTEMTIME* system_time, DWORD* error_code) {
    if (SetSystemTime(system_time)) {
        if (error_code != NULL) {
            *error_code = ERROR_SUCCESS;
        }
        return TRUE;
    }

    if (error_code != NULL) {
        *error_code = GetLastError();
    }
    return FALSE;
}
