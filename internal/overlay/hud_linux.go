//go:build linux

package overlay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/randr"
	"github.com/jezek/xgb/shape"
	"github.com/jezek/xgb/xproto"

	"gourdian/internal/config"
	"gourdian/internal/hotkey"
)

// X modifier masks.
const (
	xShift   = 1
	xLock    = 2
	xControl = 4
	xMod1    = 8  // Alt
	xMod2    = 16 // Num Lock
	xMod4    = 64 // Super
	xModKeys = xShift | xControl | xMod1 | xMod4
)

type action int

const (
	actToggle action = iota + 1
	actEdit
	actDashboard
	actPosition // actPosition+0..4 pick positions 1 to 5
)

type grab struct {
	code xproto.Keycode
	mods uint16
}

// xui is the HUD on Linux: an override-redirect window with a 32-bit visual over the game,
// which X11 desktops and XWayland (where Dota itself runs on Wayland desktops) both show.
type xui struct {
	model *model
	opts  Options
	api   api
	log   *slog.Logger

	conn     *xgb.Conn
	root     xproto.Window
	win      xproto.Window
	gc       xproto.Gcontext
	maxReq   int
	msbFirst bool
	shaped   bool
	monitor  image.Rectangle // the screen the HUD sits on, in root coordinates
	keycodes map[uint32]xproto.Keycode

	base    float64
	p       *painter
	layout  config.OverlaySettings
	x, y    int
	w, h    int
	mapped  bool
	hidden  bool
	editing bool
	last    *image.RGBA
	lastKey string
	pixels  []byte

	hotkeys         config.HotkeySettings
	grabs           map[grab]action
	positionKeys    bool
	dashboardWindow bool
	lastOpen        time.Time

	drag *drag

	wake     chan struct{}
	settings chan settingsView
	editReq  chan struct{}
}

type drag struct{ rootX, rootY, x, y int }

func Run(ctx context.Context, o Options) error {
	u := &xui{model: newModel(time.Now(), o.Quiet), opts: o, api: newAPI(o.URL), log: o.logger(),
		wake: make(chan struct{}, 1), settings: make(chan settingsView, 1), editReq: make(chan struct{}, 1), grabs: map[grab]action{}}
	defaults := config.Default().Settings
	u.layout, u.dashboardWindow, u.editing = defaults.Overlay, defaults.DashboardWindow, o.SnapshotEditing
	u.layout.HUDCorner = cmpOr(o.Corner, u.layout.HUDCorner)
	if o.Snapshot != "" {
		return u.snapshot(ctx)
	}
	xgb.Logger = slog.NewLogLogger(u.log.Handler(), slog.LevelDebug)
	err := u.connect()
	u.report(err)
	if err != nil {
		return err
	}
	defer u.conn.Close()
	u.registerHotkeys(defaults.Hotkeys)

	events := make(chan xgb.Event, 64)
	go func() {
		defer close(events)
		for {
			ev, err := u.conn.WaitForEvent()
			if ev == nil && err == nil {
				return
			}
			if err != nil {
				u.log.Debug("X error", "err", err)
				continue
			}
			events <- ev
		}
	}()
	go stream(ctx, o.URL, u.model, u.poke, u.onEvent)
	go u.loadSettings(ctx)

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	u.refresh()
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-events:
			if !ok {
				return errors.New("the X server closed the HUD's connection")
			}
			u.handle(ev)
		case <-u.wake:
			u.refresh()
		case v := <-u.settings:
			u.apply(v)
		case <-u.editReq:
			u.setEditing(!u.editing)
		case <-tick.C:
			u.refresh()
		}
	}
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// report tells the trainer whether the HUD could open, for the dashboard's setup checks.
func (u *xui) report(err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		u.api.do(ctx, "POST", "/api/overlay/status", map[string]any{"hud_error": msg}, nil)
	}()
}

func (u *xui) connect() error {
	conn, err := xgb.NewConn()
	if err != nil {
		return fmt.Errorf("no X display (DISPLAY=%q), and the HUD needs an X11 or XWayland session: %w", os.Getenv("DISPLAY"), err)
	}
	u.conn = conn
	setup := xproto.Setup(conn)
	scr := setup.DefaultScreen(conn)
	u.root = scr.Root
	u.maxReq = int(setup.MaximumRequestLength) * 4
	u.msbFirst = setup.ImageByteOrder == xproto.ImageOrderMSBFirst
	var visual xproto.Visualid
	for _, d := range scr.AllowedDepths {
		for _, v := range d.Visuals {
			if d.Depth == 32 && v.Class == xproto.VisualClassTrueColor && visual == 0 {
				visual = v.VisualId
			}
		}
	}
	if visual == 0 {
		conn.Close()
		return errors.New("the X server has no 32-bit visual, so the HUD can't be see-through")
	}
	u.monitor = monitorOf(conn, scr)
	u.base = xScale(conn, scr, u.opts.Scale, u.monitor)
	if u.p, err = newPainter(u.base, u.layout); err != nil {
		conn.Close()
		return err
	}
	u.w, u.h = u.p.width, 1

	cmap, err := xproto.NewColormapId(conn)
	if err != nil {
		conn.Close()
		return err
	}
	if err := xproto.CreateColormapChecked(conn, xproto.ColormapAllocNone, cmap, u.root, visual).Check(); err != nil {
		conn.Close()
		return fmt.Errorf("create colormap: %w", err)
	}
	if u.win, err = xproto.NewWindowId(conn); err != nil {
		conn.Close()
		return err
	}
	u.x, u.y = u.position()
	events := uint32(xproto.EventMaskExposure | xproto.EventMaskButtonPress | xproto.EventMaskButtonRelease | xproto.EventMaskPointerMotion)
	mask := uint32(xproto.CwBackPixel | xproto.CwBorderPixel | xproto.CwOverrideRedirect | xproto.CwEventMask | xproto.CwColormap)
	if err := xproto.CreateWindowChecked(conn, 32, u.win, u.root, int16(u.x), int16(u.y), uint16(u.w), uint16(u.h), 0,
		xproto.WindowClassInputOutput, visual, mask, []uint32{0, 0, 1, events, uint32(cmap)}).Check(); err != nil {
		conn.Close()
		return fmt.Errorf("create HUD window: %w", err)
	}
	u.setProperties()
	if shape.Init(conn) == nil {
		u.shaped = true
		u.clickThrough(true)
	} else {
		u.log.Warn("the X server has no SHAPE extension, so the HUD takes clicks")
	}
	if u.gc, err = xproto.NewGcontextId(conn); err != nil {
		conn.Close()
		return err
	}
	xproto.CreateGC(conn, u.gc, xproto.Drawable(u.win), 0, nil)
	u.loadKeycodes()
	return nil
}

func (u *xui) atom(name string) xproto.Atom {
	r, err := xproto.InternAtom(u.conn, false, uint16(len(name)), name).Reply()
	if err != nil {
		return 0
	}
	return r.Atom
}

// setProperties names the window, and asks compositors to keep compositing it and to treat it
// like a notification, which they don't decorate or shadow.
func (u *xui) setProperties() {
	title := "Gourdian HUD"
	utf8 := u.atom("UTF8_STRING")
	xproto.ChangeProperty(u.conn, xproto.PropModeReplace, u.win, xproto.AtomWmName, xproto.AtomString, 8, uint32(len(title)), []byte(title))
	xproto.ChangeProperty(u.conn, xproto.PropModeReplace, u.win, u.atom("_NET_WM_NAME"), utf8, 8, uint32(len(title)), []byte(title))
	class := "gourdian\x00Gourdian\x00"
	xproto.ChangeProperty(u.conn, xproto.PropModeReplace, u.win, xproto.AtomWmClass, xproto.AtomString, 8, uint32(len(class)), []byte(class))
	kind := u.atom("_NET_WM_WINDOW_TYPE_NOTIFICATION")
	xproto.ChangeProperty(u.conn, xproto.PropModeReplace, u.win, u.atom("_NET_WM_WINDOW_TYPE"), xproto.AtomAtom, 32, 1, u32(uint32(kind)))
	xproto.ChangeProperty(u.conn, xproto.PropModeReplace, u.win, u.atom("_NET_WM_BYPASS_COMPOSITOR"), xproto.AtomCardinal, 32, 1, u32(2))
}

func u32(v uint32) []byte { b := make([]byte, 4); xgb.Put32(b, v); return b }

// clickThrough empties the window's input region so clicks reach the game, or restores it.
func (u *xui) clickThrough(on bool) {
	if !u.shaped {
		return
	}
	if on {
		shape.Rectangles(u.conn, shape.SoSet, shape.SkInput, xproto.ClipOrderingUnsorted, u.win, 0, 0, nil)
	} else {
		shape.Mask(u.conn, shape.SoSet, shape.SkInput, u.win, 0, 0, xproto.PixmapNone)
	}
}

// monitorOf is the primary monitor, or the largest when none is primary, as XWayland reports.
func monitorOf(conn *xgb.Conn, scr *xproto.ScreenInfo) image.Rectangle {
	full := image.Rect(0, 0, int(scr.WidthInPixels), int(scr.HeightInPixels))
	if randr.Init(conn) != nil {
		return full
	}
	res, err := randr.GetScreenResourcesCurrent(conn, scr.Root).Reply()
	if err != nil {
		return full
	}
	var primary randr.Output
	if p, err := randr.GetOutputPrimary(conn, scr.Root).Reply(); err == nil {
		primary = p.Output
	}
	var best image.Rectangle
	for _, out := range res.Outputs {
		info, err := randr.GetOutputInfo(conn, out, res.ConfigTimestamp).Reply()
		if err != nil || info.Crtc == 0 {
			continue
		}
		crtc, err := randr.GetCrtcInfo(conn, info.Crtc, res.ConfigTimestamp).Reply()
		if err != nil || crtc.Width == 0 || crtc.Height == 0 {
			continue
		}
		r := image.Rect(int(crtc.X), int(crtc.Y), int(crtc.X)+int(crtc.Width), int(crtc.Y)+int(crtc.Height))
		if out == primary {
			return r
		}
		if r.Dx()*r.Dy() > best.Dx()*best.Dy() {
			best = r
		}
	}
	if best.Empty() {
		return full
	}
	return best
}

// xScale follows the desktop's Xft.dpi, and grows the HUD on tall screens as on Windows.
func xScale(conn *xgb.Conn, scr *xproto.ScreenInfo, user float64, mon image.Rectangle) float64 {
	dpi := 1.0
	if r, err := xproto.GetProperty(conn, false, scr.Root, xproto.AtomResourceManager, xproto.AtomString, 0, 1<<16).Reply(); err == nil {
		for _, line := range strings.Split(string(r.Value), "\n") {
			if v, ok := strings.CutPrefix(line, "Xft.dpi:"); ok {
				if d, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && d > 0 {
					dpi = d / 96
				}
			}
		}
	}
	return max(user, 0.5) * max(dpi, float64(mon.Dy())/1200)
}

func (u *xui) position() (int, int) {
	s := u.monitor
	if o := u.layout; o.HUDPlaced {
		// Keep at least part of it on screen in case the resolution changed.
		return min(max(o.HUDX, s.Min.X-u.w/2), s.Max.X-u.w/2), min(max(o.HUDY, s.Min.Y), s.Max.Y-u.p.px(60))
	}
	y := s.Min.Y + s.Dy()*12/100 + u.p.px(float64(u.opts.OffsetY))
	margin := u.p.px(24 + float64(u.opts.OffsetX))
	switch u.layout.HUDCorner {
	case "top-left":
		return s.Min.X + margin, y
	case "top-center":
		return s.Min.X + (s.Dx()-u.w)/2 + u.p.px(float64(u.opts.OffsetX)), y
	}
	return s.Max.X - u.w - margin, y
}

func (u *xui) poke() {
	select {
	case u.wake <- struct{}{}:
	default:
	}
}

func (u *xui) onEvent(event string, data []byte) {
	switch event {
	case "settings":
		var view settingsView
		if json.Unmarshal(data, &view) == nil {
			u.queueSettings(view)
		}
	case "hud_edit":
		select {
		case u.editReq <- struct{}{}:
		default:
		}
	}
}

// queueSettings keeps only the newest settings for the UI loop.
func (u *xui) queueSettings(view settingsView) {
	for {
		select {
		case u.settings <- view:
			return
		default:
			select {
			case <-u.settings:
			default:
			}
		}
	}
}

func (u *xui) loadSettings(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var view settingsView
	if err := u.api.do(ctx, "GET", "/api/settings", nil, &view); err == nil {
		u.queueSettings(view)
	}
}

func (u *xui) apply(v settingsView) {
	u.applyLayout(v.Settings.Overlay)
	u.dashboardWindow = v.Settings.DashboardWindow
	if v.Settings.Hotkeys != u.hotkeys {
		u.registerHotkeys(v.Settings.Hotkeys)
	}
}

func (u *xui) applyLayout(o config.OverlaySettings) {
	if o.HUDScale == 0 || o == u.layout {
		return // not loaded yet, or unchanged
	}
	p, err := newPainter(u.base, o)
	if err != nil {
		u.log.Error("resize HUD", "err", err)
		return
	}
	u.layout, u.p, u.w, u.lastKey = o, p, p.width, ""
	if u.drag == nil {
		u.x, u.y = u.position()
	}
	u.refresh()
}

// saveLayout applies a layout change right away and sends it to the trainer.
func (u *xui) saveLayout(o config.OverlaySettings) {
	u.applyLayout(o)
	p := patch{"overlay": patch{"hud_placed": o.HUDPlaced, "hud_x": o.HUDX, "hud_y": o.HUDY, "hud_scale": o.HUDScale, "hud_background": o.HUDBackground}}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := u.api.do(ctx, "PUT", "/api/settings", p, nil); err != nil {
			u.log.Warn("couldn't save the HUD layout", "err", err)
		}
	}()
}

func (u *xui) refresh() {
	u.setPositionKeys(u.model.askingPosition())
	v := u.model.view(time.Now(), u.editing)
	show := u.editing || !u.hidden && !v.Empty()
	if show {
		key, _ := json.Marshal(struct {
			V       View
			Editing bool
		}{v, u.editing})
		if string(key) != u.lastKey {
			u.lastKey = string(key)
			u.last = u.p.paint(v, u.editing)
			u.present(u.last, true)
		}
		if u.drag == nil {
			// Stay above the game, which the window manager may raise after alt-tab.
			xproto.ConfigureWindow(u.conn, u.win, xproto.ConfigWindowStackMode, []uint32{xproto.StackModeAbove})
		}
	}
	if show != u.mapped {
		if show {
			xproto.MapWindow(u.conn, u.win)
		} else {
			xproto.UnmapWindow(u.conn, u.win)
		}
		u.mapped = show
	}
}

// present puts the image on the window, resizing and placing it first when move is set.
func (u *xui) present(img *image.RGBA, move bool) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if move {
		u.w, u.h = w, h
		if u.drag == nil {
			u.x, u.y = u.position()
		}
		xproto.ConfigureWindow(u.conn, u.win, xproto.ConfigWindowX|xproto.ConfigWindowY|xproto.ConfigWindowWidth|xproto.ConfigWindowHeight,
			[]uint32{uint32(int32(u.x)), uint32(int32(u.y)), uint32(w), uint32(h)})
	}
	if cap(u.pixels) < len(img.Pix) {
		u.pixels = make([]byte, len(img.Pix))
	}
	buf := u.pixels[:len(img.Pix)]
	for i := 0; i < len(img.Pix); i += 4 {
		r, g, b, a := img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]
		if u.msbFirst {
			buf[i], buf[i+1], buf[i+2], buf[i+3] = a, r, g, b
		} else {
			buf[i], buf[i+1], buf[i+2], buf[i+3] = b, g, r, a
		}
	}
	rows := max(1, (u.maxReq-32)/(w*4))
	for top := 0; top < h; top += rows {
		n := min(rows, h-top)
		xproto.PutImage(u.conn, xproto.ImageFormatZPixmap, xproto.Drawable(u.win), u.gc, uint16(w), uint16(n), 0, int16(top), 0, 32, buf[top*w*4:(top+n)*w*4])
	}
}

func (u *xui) handle(ev xgb.Event) {
	switch e := ev.(type) {
	case xproto.ExposeEvent:
		if e.Count == 0 && u.last != nil {
			u.present(u.last, false)
		}
	case xproto.KeyPressEvent:
		if a, ok := u.grabs[grab{e.Detail, e.State & xModKeys}]; ok {
			u.do(a)
		}
	case xproto.MappingNotifyEvent:
		u.loadKeycodes()
		u.registerHotkeys(u.hotkeys)
	case xproto.ButtonPressEvent:
		if !u.editing {
			return
		}
		switch e.Detail {
		case 1:
			if (image.Point{int(e.EventX), int(e.EventY)}).In(u.p.doneRect(u.h)) {
				u.setEditing(false)
				return
			}
			u.drag = &drag{int(e.RootX), int(e.RootY), u.x, u.y}
			xproto.GrabPointer(u.conn, false, u.win, xproto.EventMaskButtonRelease|xproto.EventMaskPointerMotion,
				xproto.GrabModeAsync, xproto.GrabModeAsync, xproto.WindowNone, xproto.CursorNone, xproto.TimeCurrentTime)
		case 4, 5:
			u.saveLayout(wheelLayout(u.layout, e.Detail == 4, e.State&xControl != 0))
		}
	case xproto.MotionNotifyEvent:
		if d := u.drag; d != nil {
			u.x, u.y = d.x+int(e.RootX)-d.rootX, d.y+int(e.RootY)-d.rootY
			xproto.ConfigureWindow(u.conn, u.win, xproto.ConfigWindowX|xproto.ConfigWindowY, []uint32{uint32(int32(u.x)), uint32(int32(u.y))})
		}
	case xproto.ButtonReleaseEvent:
		if e.Detail == 1 && u.drag != nil {
			u.drag = nil
			xproto.UngrabPointer(u.conn, xproto.TimeCurrentTime)
			o := u.layout
			o.HUDPlaced, o.HUDX, o.HUDY = true, u.x, u.y
			u.saveLayout(o)
		}
	}
}

func (u *xui) do(a action) {
	switch a {
	case actToggle:
		u.hidden = !u.hidden
		u.refresh()
	case actEdit:
		u.setEditing(!u.editing)
	case actDashboard:
		u.openDashboard()
	default:
		if i := int(a - actPosition); i >= 0 && i < len(config.Roles) {
			u.pickPosition(i)
		}
	}
}

// setEditing lets the HUD take the mouse, so it can be dragged and resized, and shows its whole
// area with sample content even outside a match.
func (u *xui) setEditing(on bool) {
	if u.editing == on {
		return
	}
	u.editing = on
	u.clickThrough(!on)
	u.refresh()
}

func (u *xui) pickPosition(i int) {
	role := config.Roles[i]
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := u.api.do(ctx, "POST", "/api/role", map[string]string{"role": role}, nil); err != nil {
			u.log.Warn("couldn't set the position", "err", err)
		}
	}()
}

func (u *xui) loadKeycodes() {
	setup := xproto.Setup(u.conn)
	count := int(setup.MaxKeycode) - int(setup.MinKeycode) + 1
	r, err := xproto.GetKeyboardMapping(u.conn, setup.MinKeycode, byte(count)).Reply()
	if err != nil {
		u.log.Warn("couldn't read the keyboard map; hotkeys are off", "err", err)
		return
	}
	per := int(r.KeysymsPerKeycode)
	u.keycodes = map[uint32]xproto.Keycode{}
	for i := range count {
		for j := range min(per, 2) {
			sym := uint32(r.Keysyms[i*per+j])
			if _, ok := u.keycodes[sym]; !ok && sym != 0 {
				u.keycodes[sym] = xproto.Keycode(int(setup.MinKeycode) + i)
			}
		}
	}
}

func xMods(h hotkey.Hotkey) uint16 {
	var m uint16
	if h.Ctrl {
		m |= xControl
	}
	if h.Shift {
		m |= xShift
	}
	if h.Alt {
		m |= xMod1
	}
	if h.Win {
		m |= xMod4
	}
	return m
}

// grabKey takes the combination for the whole desktop, with Caps Lock and Num Lock on or off.
func (u *xui) grabKey(h hotkey.Hotkey, a action) error {
	code, ok := u.keycodes[h.Keysym()]
	if !ok {
		return fmt.Errorf("%s isn't on this keyboard", h)
	}
	g := grab{code, xMods(h)}
	for _, extra := range []uint16{0, xLock, xMod2, xLock | xMod2} {
		if err := xproto.GrabKeyChecked(u.conn, true, u.root, g.mods|extra, code, xproto.GrabModeAsync, xproto.GrabModeAsync).Check(); err != nil {
			u.ungrab(g)
			return fmt.Errorf("%s is already used by another program", h)
		}
	}
	u.grabs[g] = a
	return nil
}

func (u *xui) ungrab(g grab) {
	for _, extra := range []uint16{0, xLock, xMod2, xLock | xMod2} {
		xproto.UngrabKey(u.conn, g.code, u.root, g.mods|extra)
	}
	delete(u.grabs, g)
}

// registerHotkeys swaps the grabbed shortcuts for hk and tells the trainer which ones another
// program already uses, so the dashboard can say so.
func (u *xui) registerHotkeys(hk config.HotkeySettings) {
	for g, a := range u.grabs {
		if a < actPosition {
			u.ungrab(g)
		}
	}
	u.hotkeys = hk
	problems := map[string]string{}
	for _, k := range []struct {
		a       action
		setting string
		combo   string
	}{{actToggle, "hud_toggle", hk.HUDToggle}, {actEdit, "hud_edit", hk.HUDEdit}, {actDashboard, "dashboard", hk.Dashboard}} {
		h, err := hotkey.Parse(k.combo)
		if err == nil {
			err = u.grabKey(h, k.a)
		}
		if err != nil {
			problems[k.setting] = err.Error()
			u.log.Warn("hotkey unavailable", "hotkey", k.combo, "err", err)
		}
	}
	u.model.setHotkeys(hk)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		u.api.do(ctx, "POST", "/api/overlay/status", map[string]any{"hotkey_problems": problems}, nil)
	}()
}

// setPositionKeys grabs Ctrl+Shift+1..5 only while the HUD asks for the position.
func (u *xui) setPositionKeys(on bool) {
	if on == u.positionKeys {
		return
	}
	u.positionKeys = on
	for i := range config.Roles {
		h := hotkey.Hotkey{Ctrl: true, Shift: true, Key: strconv.Itoa(i + 1)}
		if on {
			if err := u.grabKey(h, actPosition+action(i)); err != nil {
				u.log.Debug("position hotkey unavailable", "err", err)
			}
		} else if code, ok := u.keycodes[h.Keysym()]; ok {
			u.ungrab(grab{code, xMods(h)})
		}
	}
}

// openDashboard brings an open dashboard window to the front, or opens the dashboard.
func (u *xui) openDashboard() {
	if u.activateDashboard() {
		return
	}
	if time.Since(u.lastOpen) < opening {
		return
	}
	u.lastOpen = time.Now()
	OpenDashboard(u.opts.URL+"/", u.dashboardWindow)
}

// activateDashboard asks the window manager to show a browser window whose title is a
// dashboard page's. Browsers running natively on Wayland aren't listed, so it may find none.
func (u *xui) activateDashboard() bool {
	list, err := xproto.GetProperty(u.conn, false, u.root, u.atom("_NET_CLIENT_LIST"), xproto.AtomWindow, 0, 1024).Reply()
	if err != nil {
		return false
	}
	name, utf8 := u.atom("_NET_WM_NAME"), u.atom("UTF8_STRING")
	for i := 0; i+4 <= len(list.Value); i += 4 {
		w := xproto.Window(xgb.Get32(list.Value[i:]))
		t, err := xproto.GetProperty(u.conn, false, w, name, utf8, 0, 256).Reply()
		if err != nil || !strings.HasPrefix(string(t.Value), dashboardTitle) {
			continue
		}
		data := xproto.ClientMessageDataUnionData32New([]uint32{2, 0, 0, 0, 0})
		ev := xproto.ClientMessageEvent{Format: 32, Window: w, Type: u.atom("_NET_ACTIVE_WINDOW"), Data: data}
		xproto.SendEvent(u.conn, false, u.root, xproto.EventMaskSubstructureRedirect|xproto.EventMaskSubstructureNotify, string(ev.Bytes()))
		return true
	}
	return false
}

// snapshot paints the HUD to a PNG without a display, for checking its look.
func (u *xui) snapshot(ctx context.Context) error {
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
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var view settingsView
	if err := u.api.do(ctx, "GET", "/api/settings", nil, &view); err == nil && view.Settings.Overlay.HUDScale != 0 {
		u.layout = view.Settings.Overlay
	}
	p, err := newPainter(max(u.opts.Scale, 0.5), u.layout)
	if err != nil {
		return err
	}
	return savePaintedPNG(u.opts.Snapshot, p.paint(u.model.view(time.Now(), u.editing), u.editing))
}
