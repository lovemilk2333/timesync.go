//go:build windows
// +build windows

package core

import (
	"fmt"
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// https://github.com/atomicgo/isadmin/blob/main/isadmin_windows.go
func IsAdmin() bool {
	var sid *windows.SID

	_ = windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid)

	admin, _ := windows.Token(0).IsMember(sid)

	return admin
}

func RunAsAdmin() {
	verb := "runas"
	exe, _ := os.Executable()
	cwd, _ := os.Getwd()
	args := strings.Join(os.Args[1:], " ")

	verb_ptr, _ := syscall.UTF16PtrFromString(verb)
	exe_ptr, _ := syscall.UTF16PtrFromString(exe)
	cwd_ptr, _ := syscall.UTF16PtrFromString(cwd)
	arg_ptr, _ := syscall.UTF16PtrFromString(args)

	var show_cmd int32 = 1 //SwNormal

	err := windows.ShellExecute(0, verb_ptr, exe_ptr, arg_ptr, cwd_ptr, show_cmd)
	if err != nil {
		fmt.Println(err)
	}
}
