//go:build windows
// +build windows

package main

import (
	"os"

	"github.com/lovemilk2333/timesync.go/timesync/core"
)

func prepare() {
	if !core.IsAdmin() {
		core.RunAsAdmin()
		os.Exit(0)
	}
}
