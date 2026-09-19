//go:build windows

package overlay

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"gourdian/internal/buildinfo"
)

const (
	trayDashboard = iota + 1
	trayStats
	trayEditHUD
	trayHideHUD
	trayRecord
	trayFolder
	trayQuit
)

// tray owns a hidden window: tray menus need a foreground owner, which the HUD can't be.
type tray struct {
	u    *ui
	hwnd uintptr
	data notifyIconData
}

func newTray(u *ui) *tray { return &tray{u: u} }

func (t *tray) add() error {
	className, hinst, err := registerClass("GourdianTray", trayWndProc)
	if err != nil {
		return fmt.Errorf("register tray window class: %w", err)
	}
	hwnd, _, err := pCreateWindowEx.Call(wsExToolWindow, uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Gourdian"))), wsPopup, 0, 0, 0, 0, 0, 0, hinst, 0)
	if hwnd == 0 {
		return fmt.Errorf("create tray window: %w", err)
	}
	t.hwnd = hwnd

	cx, _, _ := pGetSystemMetrics.Call(smCXSmIcon)
	cy, _, _ := pGetSystemMetrics.Call(smCYSmIcon)
	icon, _, _ := pLoadImage.Call(hinst, uintptr(unsafe.Pointer(utf16("APP"))), imageIcon, cx, cy, 0)
	if icon == 0 {
		icon, _, _ = pLoadIcon.Call(0, idiApplication)
	}
	d := &t.data
	d.cbSize = uint32(unsafe.Sizeof(*d))
	d.hWnd = hwnd
	d.uID = 1
	d.uFlags = nifMessage | nifIcon | nifTip
	d.uCallbackMessage = wmTray
	d.hIcon = icon
	copyUTF16(d.szTip[:], "Gourdian "+buildinfo.Version)
	if !t.u.opts.Quiet {
		d.uFlags |= nifInfo
		copyUTF16(d.szInfoTitle[:], "Gourdian is running")
		copyUTF16(d.szInfo[:], "Click this icon for the dashboard. In game: "+t.u.hotkeys.HUDEdit+" moves the HUD, "+t.u.hotkeys.Dashboard+" opens the dashboard.")
		d.dwInfoFlags = niifInfo
	}
	if r, _, err := pShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(d))); r == 0 {
		return errors.Join(errors.New("Shell_NotifyIcon failed"), err)
	}
	d.uFlags &^= nifInfo
	return nil
}

func (t *tray) remove() {
	pShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&t.data)))
}

func trayWndProc(hwnd, message, wParam, lParam uintptr) uintptr {
	if message == wmTray && active != nil && active.tray != nil {
		switch lParam & 0xFFFF {
		case wmLButtonUp:
			OpenDashboard(active.opts.URL+"/", active.dashboardWindow)
		case wmRButtonUp:
			active.tray.popup()
		}
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, message, wParam, lParam)
	return r
}

func (t *tray) popup() {
	u := t.u
	h, _, _ := pCreatePopupMenu.Call()
	defer pDestroyMenu.Call(h)
	item := func(id int, label string, checked bool) {
		flags := uintptr(mfString)
		if checked {
			flags |= mfChecked
		}
		pAppendMenu.Call(h, flags, uintptr(id), uintptr(unsafe.Pointer(utf16(label))))
	}
	separator := func() { pAppendMenu.Call(h, mfSeparator, 0, 0) }
	item(trayDashboard, "Open dashboard\t"+u.hotkeys.Dashboard, false)
	item(trayStats, "Open stats", false)
	separator()
	item(trayEditHUD, "Move and resize HUD\t"+u.hotkeys.HUDEdit, u.editing)
	item(trayHideHUD, "Hide HUD\t"+u.hotkeys.HUDToggle, u.hidden)
	item(trayRecord, "Record game data", u.isRecording())
	separator()
	if u.opts.AppDir != "" {
		item(trayFolder, "Open app folder", false)
	}
	item(trayQuit, "Quit Gourdian", false)

	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	// A popup menu only closes on outside clicks if its owner is the foreground window.
	pSetForegroundWindow.Call(t.hwnd)
	cmd, _, _ := pTrackPopupMenu.Call(h, tpmReturn|tpmRightBtn|tpmNoNotify, uintptr(pt.x), uintptr(pt.y), 0, t.hwnd, 0)
	pPostMessage.Call(t.hwnd, wmNull, 0, 0)

	base := u.opts.URL
	switch int(cmd) {
	case trayDashboard:
		OpenDashboard(base+"/", u.dashboardWindow)
	case trayStats:
		OpenDashboard(base+"/stats.html", u.dashboardWindow)
	case trayEditHUD:
		u.setEditing(!u.editing)
	case trayHideHUD:
		u.toggleHUD()
	case trayRecord:
		on := !u.isRecording()
		go t.call("POST", "/api/recording", map[string]bool{"on": on})
	case trayFolder:
		ShellOpen(u.opts.AppDir)
	case trayQuit:
		go t.call("POST", "/api/quit", nil)
	}
}

func (t *tray) call(method, path string, body any) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := t.u.api.do(ctx, method, path, body, nil); err != nil {
		t.u.log.Warn("tray action failed", "path", path, "err", err)
	}
}
