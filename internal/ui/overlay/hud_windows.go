//go:build windows

package overlay

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"gourdian/internal/game/dota"
	"gourdian/internal/sys/config"
	"gourdian/internal/sys/hotkey"
	"gourdian/internal/ui/hud"
)

const (
	hotkeyHUD = iota + 1
	hotkeyEdit
	hotkeyDashboard
	// hotkeyPosition+0..4 pick positions 1 to 5; they're only registered while the HUD asks.
	hotkeyPosition
)

type ui struct {
	hudState

	g *gdi

	hwnd      uintptr
	w, h      int32
	visible   bool
	baseScale float64

	fontBig, fontSmall uintptr
	canvas             *canvas

	// Settings arrive on other goroutines and are applied on the UI thread.
	mu        sync.Mutex
	pending   *settingsView
	recording bool

	tray *tray
}

// The window procedures are C callbacks with no user pointer, so they reach the UI through this.
var active *ui

func Run(ctx context.Context, o Options) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pSetProcessDPIAware.Call()

	u := &ui{hudState: hudState{model: newModel(time.Now(), o.Quiet), opts: o, api: newAPI(o.URL), log: o.logger(), editing: o.SnapshotEditing},
		baseScale: uiScale(o.Scale)}
	defaults := config.Default().Settings
	u.layout, u.dashboardWindow = defaults.Overlay, defaults.DashboardWindow
	u.layout.HUDCorner = o.Corner
	if err := u.setupGDI(); err != nil {
		return err
	}
	defer func() { u.g.release(); u.canvas.release() }()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if o.Snapshot != "" {
		return u.snapshot(ctx)
	}

	className, hinst, err := registerClass("GourdianOverlay", wndProc)
	if err != nil {
		return fmt.Errorf("register window class: %w", err)
	}
	x, y := u.position()
	active = u
	hwnd, _, err := pCreateWindowEx.Call(
		wsExLayered|wsExTransparent|wsExTopmost|wsExToolWindow|wsExNoActivate,
		uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Gourdian HUD"))),
		wsPopup, uintptr(x), uintptr(y), uintptr(u.w), uintptr(u.h), 0, 0, hinst, 0)
	if hwnd == 0 {
		return fmt.Errorf("create overlay window: %w", err)
	}
	u.hwnd = hwnd
	u.registerHotkeys(defaults.Hotkeys)
	if o.Tray {
		u.tray = newTray(u)
		if err := u.tray.add(); err != nil {
			u.log.Warn("tray icon unavailable", "err", err)
			u.tray = nil
		} else {
			defer u.tray.remove()
		}
	}
	pSetTimer.Call(hwnd, 1, 1000, 0)

	go stream(ctx, o.URL, u.model, func() { pPostMessage.Call(hwnd, wmApp, 0, 0) }, u.onEvent)
	go startDraftReader(ctx, u.model, &u.api, u.opts.framesDir(), u.log)
	go u.loadSettings(ctx)
	go func() {
		<-ctx.Done()
		pPostMessage.Call(hwnd, wmClose, 0, 0)
	}()
	u.refresh()

	var m msg
	for {
		r, _, err := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		switch int32(r) {
		case -1:
			return fmt.Errorf("overlay message loop: %w", err)
		case 0:
			return nil
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// registerHotkeys swaps the registered shortcuts for hk and tells the trainer which ones
// another program already uses, so the dashboard can say so.
func (u *ui) registerHotkeys(hk config.HotkeySettings) {
	for id := hotkeyHUD; id <= hotkeyDashboard; id++ {
		pUnregisterHotKey.Call(u.hwnd, uintptr(id))
	}
	u.hotkeys = hk
	problems := map[string]string{}
	for _, k := range []struct {
		id      int
		setting string
		combo   string
	}{
		{hotkeyHUD, "hud_toggle", hk.HUDToggle},
		{hotkeyEdit, "hud_edit", hk.HUDEdit},
		{hotkeyDashboard, "dashboard", hk.Dashboard},
	} {
		h, err := hotkey.Parse(k.combo)
		if err != nil {
			problems[k.setting] = err.Error()
			continue
		}
		if r, _, _ := pRegisterHotKey.Call(u.hwnd, uintptr(k.id), uintptr(h.Mods()|modNoRepeat), uintptr(h.VK())); r == 0 {
			problems[k.setting] = h.String() + " is already used by another program"
			u.log.Warn("hotkey is taken by another program", "hotkey", h.String())
		}
	}
	u.model.setHotkeys(hk)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		u.api.do(ctx, "POST", "/api/overlay/status", map[string]any{"hotkey_problems": problems}, nil)
	}()
}

func (u *ui) snapshot(ctx context.Context) error {
	ready := make(chan struct{}, 1)
	go stream(ctx, u.opts.URL, u.model, func() {
		select {
		case ready <- struct{}{}:
		default:
		}
	}, nil)
	select {
	case <-ready:
		time.Sleep(700 * time.Millisecond)
	case <-time.After(5 * time.Second):
	}
	u.loadSettings(ctx)
	u.applyPending()
	h := u.draw(u.model.view(time.Now(), u.editing))
	return u.canvas.savePNG(u.opts.Snapshot, h)
}

func (u *ui) loadSettings(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var view settingsView
	if err := u.api.do(ctx, "GET", "/api/settings", nil, &view); err == nil {
		u.setPending(view)
	}
}

// onEvent handles trainer events meant for the overlay itself: settings changed on the
// dashboard, and the dashboard's "Edit in game" button.
func (u *ui) onEvent(event string, data []byte) {
	switch event {
	case "settings":
		var view settingsView
		if json.Unmarshal(data, &view) == nil {
			u.setPending(view)
		}
	case "hud_edit":
		pPostMessage.Call(u.hwnd, wmEditHUD, 0, 0)
	}
}

func (u *ui) setPending(view settingsView) {
	u.mu.Lock()
	u.pending = &view
	u.recording = view.Recording != ""
	u.mu.Unlock()
	if u.hwnd != 0 {
		pPostMessage.Call(u.hwnd, wmSettings, 0, 0)
	}
}

func (u *ui) applyPending() {
	u.mu.Lock()
	p := u.pending
	u.pending = nil
	u.mu.Unlock()
	if p == nil {
		return
	}
	u.applyLayout(p.Settings.Overlay)
	u.dashboardWindow = p.Settings.DashboardWindow
	if p.Settings.Hotkeys != u.hotkeys && u.hwnd != 0 {
		u.registerHotkeys(p.Settings.Hotkeys)
	}
}

func (u *ui) isRecording() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.recording
}

func wndProc(hwnd, message, wParam, lParam uintptr) uintptr {
	u := active
	switch message {
	case wmNCHitTest:
		if !u.editing {
			return ^uintptr(0) // HTTRANSPARENT: clicks go to the game
		}
		if u.overDone(lParam) {
			return htClient
		}
		return htCaption // dragging anywhere else moves the HUD
	case wmLButtonUp:
		if u.editing {
			u.setEditing(false)
		}
		return 0
	case wmMouseActivate:
		return maNoActivate // editing the HUD must not take focus from the game
	case wmMouseWheel:
		if u.editing {
			up := int16(wParam>>16) > 0
			u.saveLayout(wheelLayout(u.layout, up, wParam&mkControl != 0))
		}
		return 0
	case wmExitSizeMove:
		u.savePosition()
		return 0
	case wmApp, wmTimer:
		u.refresh()
		return 0
	case wmSettings:
		u.applyPending()
		return 0
	case wmEditHUD:
		u.setEditing(!u.editing)
		return 0
	case wmHotKey:
		switch wParam {
		case hotkeyHUD:
			u.toggleHUD()
		case hotkeyEdit:
			u.setEditing(!u.editing)
		case hotkeyDashboard:
			OpenDashboard(u.opts.URL+"/", u.dashboardWindow)
		default:
			if i := int(wParam) - hotkeyPosition; i >= 0 && i < len(dota.Roles) {
				u.pickPosition(i)
			}
		}
		return 0
	case wmClose:
		if u.tray != nil {
			pDestroyWindow.Call(u.tray.hwnd)
		}
		pDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, message, wParam, lParam)
	return r
}

// setupGDI makes the fonts and the drawing canvas for the current size and width.
func (u *ui) setupGDI() error {
	u.g = newGDI(u.baseScale * float64(u.layout.HUDScale) / 100)
	u.w = u.g.px(float64(u.layout.HUDWidth))
	u.fontBig = u.g.font(22, 600)
	u.fontSmall = u.g.font(17, 500)
	sh, _, _ := pGetSystemMetrics.Call(smCYScreen)
	c, err := newCanvas(u.w, min(u.g.px(1100), max(int32(sh), 400)))
	if err != nil {
		return err
	}
	u.canvas = c
	return nil
}

// applyLayout applies saved HUD settings: position, size, width and look.
func (u *ui) applyLayout(o config.OverlaySettings) {
	if o.HUDScale == 0 || o == u.layout {
		return // not loaded yet, or unchanged
	}
	old := u.layout
	u.layout = o
	if o.HUDScale != old.HUDScale || o.HUDWidth != old.HUDWidth {
		u.g.release()
		u.canvas.release()
		if err := u.setupGDI(); err != nil {
			u.log.Error("resize HUD", "err", err)
		}
	}
	if u.hwnd == 0 {
		return
	}
	x, y := u.position()
	pSetWindowPos.Call(u.hwnd, hwndTopmost, uintptr(x), uintptr(y), 0, 0, swpNoSize|swpNoActivate)
	u.refresh()
}

// setEditing turns layout editing on or off. While editing, the HUD takes mouse input and
// shows its whole area with sample content, even outside a match.
func (u *ui) setEditing(on bool) {
	if u.editing == on {
		return
	}
	u.editing = on
	style, _, _ := pGetWindowLongPtr.Call(u.hwnd, gwlExStyle)
	if on {
		style &^= wsExTransparent
	} else {
		style |= wsExTransparent
	}
	pSetWindowLongPtr.Call(u.hwnd, gwlExStyle, style)
	u.refresh()
}

func (u *ui) savePosition() {
	var r rect
	pGetWindowRect.Call(u.hwnd, uintptr(unsafe.Pointer(&r)))
	if x, y := u.position(); x == r.left && y == r.top {
		return
	}
	o := u.layout
	o.HUDPlaced, o.HUDX, o.HUDY = true, int(r.left), int(r.top)
	u.saveLayout(o)
}

// saveLayout applies a layout change right away and sends it to the trainer.
func (u *ui) saveLayout(o config.OverlaySettings) {
	u.applyLayout(o)
	sendLayout(u.api, u.log, o)
}

func (u *ui) toggleHUD() {
	u.hidden = !u.hidden
	u.refresh()
}

func (u *ui) position() (int32, int32) {
	sw, _, _ := pGetSystemMetrics.Call(smCXScreen)
	sh, _, _ := pGetSystemMetrics.Call(smCYScreen)
	w, h := int32(sw), int32(sh)
	if o := u.layout; o.HUDPlaced {
		// Keep at least part of it on screen in case the resolution changed.
		return min(max(int32(o.HUDX), -u.w/2), w-u.w/2), min(max(int32(o.HUDY), 0), h-u.g.px(60))
	}
	y := h*12/100 + u.g.px(float64(u.opts.OffsetY))
	margin := u.g.px(24 + float64(u.opts.OffsetX))
	switch u.layout.HUDCorner {
	case "top-left":
		return margin, y
	case "top-center":
		return (w-u.w)/2 + u.g.px(float64(u.opts.OffsetX)), y
	default:
		return w - u.w - margin, y
	}
}

// setPositionKeys registers Ctrl+Shift+1..5 only while the HUD asks for the position, so
// they don't block those shortcuts for other programs the rest of the time.
func (u *ui) setPositionKeys(on bool) {
	if on == u.positionKeys {
		return
	}
	u.positionKeys = on
	for i := range dota.Roles {
		if !on {
			pUnregisterHotKey.Call(u.hwnd, uintptr(hotkeyPosition+i))
			continue
		}
		// A key another program holds leaves the player pressing it to no effect, and the
		// pick advice waiting for a position it will never hear.
		if r, _, _ := pRegisterHotKey.Call(u.hwnd, uintptr(hotkeyPosition+i), modControl|modShift|modNoRepeat, uintptr('1'+i)); r == 0 {
			u.log.Warn("position hotkey is taken by another program", "hotkey", fmt.Sprintf("Ctrl+Shift+%d", i+1))
		}
	}
}

func (u *ui) pickPosition(i int) { sendPosition(u.api, u.log, i) }

func (u *ui) refresh() {
	u.setPositionKeys(u.model.askingPosition())
	v := u.model.view(time.Now(), u.editing)
	show := u.editing || !u.hidden && !v.Empty()
	if show {
		u.h = u.draw(v)
		u.canvas.present(u.hwnd, u.h, alpha(u.layout.HUDOpacity))
		// Re-assert topmost: the game can end up above us after alt-tab.
		pSetWindowPos.Call(u.hwnd, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
	}
	if show != u.visible {
		cmd := uintptr(swHide)
		if show {
			cmd = swShowNoActivate
		}
		pShowWindow.Call(u.hwnd, cmd)
		u.visible = show
	}
}

type block struct {
	lines   []hud.Line
	accent  uintptr
	font    uintptr
	heights []int32
	height  int32
}

// draw lays the view out on the canvas and returns the height used.
func (u *ui) draw(v View) int32 {
	g, c := u.g, u.canvas
	c.clear()
	pad, gap, bar := g.px(hudPad), g.px(hudGap), g.px(hudBar)
	textW := u.w - bar - 2*pad

	var blocks []*block
	if v.Banner != "" {
		blocks = append(blocks, &block{lines: []hud.Line{{Text: v.Banner, Kind: hud.KindMuted}}, accent: kindColors[hud.KindMuted], font: u.fontSmall})
	}
	if v.Alert != nil {
		a := *v.Alert
		if v.More > 0 {
			a.Text += fmt.Sprintf("  (+%d)", v.More)
		}
		blocks = append(blocks, &block{lines: []hud.Line{a}, accent: kindColors[v.Alert.Kind], font: u.fontBig})
	}
	if len(v.Rows) > 0 {
		blocks = append(blocks, &block{lines: v.Rows, accent: colorLine, font: u.fontSmall})
	}
	content := int32(0)
	for _, b := range blocks {
		b.height = 2 * pad
		for i, l := range b.lines {
			h := c.measure(l.Text, textW, b.font)
			b.heights = append(b.heights, h)
			b.height += h
			if i > 0 {
				b.height += gap
			}
		}
		content += b.height + g.px(hudSpacing)
	}

	total := content
	if u.editing {
		total = max(content+g.px(editBottom), g.px(editMin))
		c.fillRect(rect{0, 0, u.w, total}, colorRow, 170)
	}
	total = min(total, c.img.h)

	bg := uint32(u.layout.HUDBackground) * 255 / 100
	shadow := u.layout.HUDShadow
	y := int32(0)
	for _, b := range blocks {
		if y+b.height > total {
			break
		}
		c.fillRound(rect{0, y, u.w, y + b.height}, g.px(hudRadius), colorPanel, bg)
		c.fillRect(rect{0, y + g.px(hudBarInset), bar, y + b.height - g.px(hudBarInset)}, b.accent, 255)
		ty := y + pad
		for i, l := range b.lines {
			c.text(l.Text, rect{bar + pad, ty, bar + pad + textW, ty + b.heights[i]}, b.font, kindColors[l.Kind], dtWordBreak, shadow)
			ty += b.heights[i] + gap
		}
		y += b.height + g.px(hudSpacing)
	}
	if u.editing {
		u.h = total
		u.drawFrame(total)
	}
	return max(total, 1)
}

func (u *ui) doneRect() rect {
	g := u.g
	return rect{u.w - g.px(96), u.h - g.px(44), u.w - g.px(12), u.h - g.px(12)}
}

// hintFor is the edit-mode hint, shortened until it fits in width.
func (u *ui) hintFor(width int32) string {
	return editHint(u.layout, func(s string) bool { return u.canvas.textWidth(s, u.fontSmall) <= width })
}

// overDone reports whether a WM_NCHITTEST position (screen coordinates) is on the Done button.
func (u *ui) overDone(lParam uintptr) bool {
	var win rect
	pGetWindowRect.Call(u.hwnd, uintptr(unsafe.Pointer(&win)))
	x, y := int32(int16(lParam))-win.left, int32(int16(lParam>>16))-win.top
	d := u.doneRect()
	return x >= d.left && x < d.right && y >= d.top && y < d.bottom
}

// drawFrame outlines the HUD's area while editing and adds the hint and Done button.
func (u *ui) drawFrame(h int32) {
	g, c := u.g, u.canvas
	border := g.px(2)
	for _, r := range []rect{{0, 0, u.w, border}, {0, h - border, u.w, h}, {0, 0, border, h}, {u.w - border, 0, u.w, h}} {
		c.fillRect(r, colorAccent, 255)
	}
	d := u.doneRect()
	area := rect{g.px(12), d.top, d.left - g.px(8), d.bottom}
	// A narrow HUD cuts the hint off, so the longest wording that fits is used.
	label := u.hintFor(area.right - area.left)
	c.text(label, area, u.fontSmall, kindColors[hud.KindText], dtSingleLine|dtVCenter|dtEllipsis, true)
	c.fillRound(d, g.px(6), colorAccent, 255)
	c.text("Done", d, u.fontSmall, kindColors[hud.KindText], dtSingleLine|dtVCenter|dtCenter, false)
}
