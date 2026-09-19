package main

import (
	"os"
	"syscall"
)

// attachConsole lets the windowless app print when it's run from a terminal, e.g. `"Gourdian.exe" doctor`.
func attachConsole() {
	if _, err := os.Stdout.Stat(); err == nil {
		return
	}
	attach := syscall.NewLazyDLL("kernel32.dll").NewProc("AttachConsole")
	if r, _, _ := attach.Call(^uintptr(0)); r == 0 {
		return
	}
	if out, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout, os.Stderr = out, out
	}
}
