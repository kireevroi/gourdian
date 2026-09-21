//go:build windows

package overlay

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const swRestore = 9

var (
	pEnumWindows     = user32.NewProc("EnumWindows")
	pGetWindowText   = user32.NewProc("GetWindowTextW")
	pIsWindowVisible = user32.NewProc("IsWindowVisible")
	pIsIconic        = user32.NewProc("IsIconic")
)

// OpenDashboard brings an open dashboard window to the front, or opens the dashboard: in its
// own Edge app window when appWindow is set, otherwise in the default browser.
func OpenDashboard(url string, appWindow bool) {
	openMu.Lock()
	defer openMu.Unlock()
	if hwnd := findDashboard(); hwnd != 0 {
		if iconic, _, _ := pIsIconic.Call(hwnd); iconic != 0 {
			pShowWindow.Call(hwnd, swRestore)
		}
		pSetForegroundWindow.Call(hwnd)
		return
	}
	if time.Since(lastOpen) < opening {
		return
	}
	lastOpen = time.Now()
	if appWindow {
		if edge := edgePath(); edge != "" && exec.Command(edge, "--app="+url).Start() == nil {
			return
		}
	}
	ShellOpen(url)
}

var (
	openMu   sync.Mutex
	lastOpen time.Time
)

// Windows allows a limited number of callbacks per process, so this one is created once.
var (
	findMu          sync.Mutex
	findResult      uintptr
	findDashboardCB = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		if visible, _, _ := pIsWindowVisible.Call(hwnd); visible == 0 {
			return 1
		}
		buf := make([]uint16, 256)
		n, _, _ := pGetWindowText.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if strings.HasPrefix(syscall.UTF16ToString(buf[:n]), dashboardTitle) {
			findResult = hwnd
			return 0
		}
		return 1
	})
)

func findDashboard() uintptr {
	findMu.Lock()
	defer findMu.Unlock()
	findResult = 0
	pEnumWindows.Call(findDashboardCB, 0)
	return findResult
}

func edgePath() string {
	for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "LOCALAPPDATA"} {
		if dir := os.Getenv(env); dir != "" {
			p := filepath.Join(dir, "Microsoft", "Edge", "Application", "msedge.exe")
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}
