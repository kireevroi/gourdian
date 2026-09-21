// Package autostart turns "start with Windows" on and off through the same HKCU Run value
// the installer writes.
package autostart

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"gourdian/internal/sys/config"
)

var (
	advapi32        = syscall.NewLazyDLL("advapi32.dll")
	pRegOpenKeyEx   = advapi32.NewProc("RegOpenKeyExW")
	pRegQueryValue  = advapi32.NewProc("RegQueryValueExW")
	pRegSetValueEx  = advapi32.NewProc("RegSetValueExW")
	pRegDeleteValue = advapi32.NewProc("RegDeleteValueW")
	pRegCloseKey    = advapi32.NewProc("RegCloseKey")
)

const (
	hkeyCurrentUser = 0x80000001
	keyQueryValue   = 0x0001
	keySetValue     = 0x0002
	regSZ           = 1
	errFileNotFound = 2
	runKey          = `Software\Microsoft\Windows\CurrentVersion\Run`
)

// Exe is the installed app's exe, or "" when there's no installed app to start.
func Exe() string {
	if self, err := os.Executable(); err == nil && filepath.Base(self) == config.AppExe {
		return self
	}
	for _, dir := range config.InstallDirs(os.Getenv("LOCALAPPDATA")) {
		if installed := filepath.Join(dir, config.AppExe); fileExists(installed) {
			return installed
		}
	}
	return ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func openRun(access uintptr) (syscall.Handle, error) {
	var key syscall.Handle
	r, _, _ := pRegOpenKeyEx.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(runKey))), 0, access, uintptr(unsafe.Pointer(&key)))
	if r != 0 {
		return 0, syscall.Errno(r)
	}
	return key, nil
}

func Enabled() bool {
	key, err := openRun(keyQueryValue)
	if err != nil {
		return false
	}
	defer pRegCloseKey.Call(uintptr(key))
	for _, name := range []string{config.AppName, config.LegacyAppName} {
		if r, _, _ := pRegQueryValue.Call(uintptr(key), uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(name))), 0, 0, 0, 0); r == 0 {
			return true
		}
	}
	return false
}

func Set(enable bool) error {
	key, err := openRun(keySetValue)
	if err != nil {
		return err
	}
	defer pRegCloseKey.Call(uintptr(key))
	// The value the app had under its old name goes either way.
	legacy := uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(config.LegacyAppName)))
	if r, _, _ := pRegDeleteValue.Call(uintptr(key), legacy); r != 0 && r != errFileNotFound {
		return syscall.Errno(r)
	}
	name := uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(config.AppName)))
	if !enable {
		if r, _, _ := pRegDeleteValue.Call(uintptr(key), name); r != 0 && r != errFileNotFound {
			return syscall.Errno(r)
		}
		return nil
	}
	exe := Exe()
	if exe == "" {
		return errors.New("install Gourdian to start it with Windows")
	}
	data, err := syscall.UTF16FromString(`"` + exe + `" --background`)
	if err != nil {
		return err
	}
	r, _, _ := pRegSetValueEx.Call(uintptr(key), name, 0, regSZ, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)*2))
	if r != 0 {
		return syscall.Errno(r)
	}
	return nil
}
