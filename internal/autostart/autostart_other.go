//go:build !windows && !linux

package autostart

import "errors"

func Exe() string { return "" }

func Enabled() bool { return false }

func Set(enable bool) error { return errors.New("starting with the computer isn't supported here") }
