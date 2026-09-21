//go:build !windows && !linux

package overlay

import (
	"context"
	"errors"
)

func Run(ctx context.Context, o Options) error {
	return errors.New("the overlay is a Windows program: run gourdian.exe overlay")
}

func ShellOpen(target string) {}

func OpenDashboard(url string, appWindow bool) {}

func ShowMessage(title, text string, isError bool) {}
