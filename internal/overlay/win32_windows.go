//go:build windows

package overlay

import (
	"syscall"
	"unsafe"

	"dotatrainer/internal/hud"
)

var (
	user32  = syscall.NewLazyDLL("user32.dll")
	gdi32   = syscall.NewLazyDLL("gdi32.dll")
	kernel  = syscall.NewLazyDLL("kernel32.dll")
	shell32 = syscall.NewLazyDLL("shell32.dll")

	pRegisterClassEx     = user32.NewProc("RegisterClassExW")
	pCreateWindowEx      = user32.NewProc("CreateWindowExW")
	pDefWindowProc       = user32.NewProc("DefWindowProcW")
	pDestroyWindow       = user32.NewProc("DestroyWindow")
	pGetMessage          = user32.NewProc("GetMessageW")
	pTranslateMessage    = user32.NewProc("TranslateMessage")
	pDispatchMessage     = user32.NewProc("DispatchMessageW")
	pPostMessage         = user32.NewProc("PostMessageW")
	pPostQuitMessage     = user32.NewProc("PostQuitMessage")
	pShowWindow          = user32.NewProc("ShowWindow")
	pSetWindowPos        = user32.NewProc("SetWindowPos")
	pFillRect            = user32.NewProc("FillRect")
	pDrawText            = user32.NewProc("DrawTextW")
	pGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
	pSetProcessDPIAware  = user32.NewProc("SetProcessDPIAware")
	pGetDpiForSystem     = user32.NewProc("GetDpiForSystem")
	pRegisterHotKey      = user32.NewProc("RegisterHotKey")
	pUnregisterHotKey    = user32.NewProc("UnregisterHotKey")
	pSetTimer            = user32.NewProc("SetTimer")
	pGetDC               = user32.NewProc("GetDC")
	pReleaseDC           = user32.NewProc("ReleaseDC")
	pSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	pGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	pSetWindowRgn        = user32.NewProc("SetWindowRgn")
	pCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	pAppendMenu          = user32.NewProc("AppendMenuW")
	pTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	pDestroyMenu         = user32.NewProc("DestroyMenu")
	pGetCursorPos        = user32.NewProc("GetCursorPos")
	pGetWindowRect       = user32.NewProc("GetWindowRect")
	pLoadImage           = user32.NewProc("LoadImageW")
	pLoadIcon            = user32.NewProc("LoadIconW")
	pMessageBox          = user32.NewProc("MessageBoxW")
	pGetKeyState         = user32.NewProc("GetKeyState")
	pGetWindowLongPtr    = user32.NewProc("GetWindowLongPtrW")
	pSetWindowLongPtr    = user32.NewProc("SetWindowLongPtrW")

	pCreateFont             = gdi32.NewProc("CreateFontW")
	pCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
	pCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	pCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	pCreateDIBSection       = gdi32.NewProc("CreateDIBSection")
	pSelectObject           = gdi32.NewProc("SelectObject")
	pDeleteObject           = gdi32.NewProc("DeleteObject")
	pDeleteDC               = gdi32.NewProc("DeleteDC")
	pBitBlt                 = gdi32.NewProc("BitBlt")
	pSetBkMode              = gdi32.NewProc("SetBkMode")
	pSetTextColor           = gdi32.NewProc("SetTextColor")
	pRoundRect              = gdi32.NewProc("RoundRect")
	pGetStockObject         = gdi32.NewProc("GetStockObject")
	pCreateRoundRectRgn     = gdi32.NewProc("CreateRoundRectRgn")

	pGetModuleHandle = kernel.NewProc("GetModuleHandleW")

	pShellNotifyIcon = shell32.NewProc("Shell_NotifyIconW")
	pShellExecute    = shell32.NewProc("ShellExecuteW")
)

const (
	wsPopup         = 0x80000000
	wsExLayered     = 0x00080000
	wsExTransparent = 0x00000020
	wsExTopmost     = 0x00000008
	wsExToolWindow  = 0x00000080
	wsExNoActivate  = 0x08000000

	wmNull          = 0x0000
	wmDestroy       = 0x0002
	wmActivate      = 0x0006
	wmPaint         = 0x000F
	wmClose         = 0x0010
	wmEraseBkgnd    = 0x0014
	wmNCHitTest     = 0x0084
	wmMouseActivate = 0x0021
	maNoActivate    = 3
	gwlExStyle      = ^uintptr(19) // -20
	wmKeyDown       = 0x0100
	wmTimer         = 0x0113
	wmMouseMove     = 0x0200
	wmLButtonDown   = 0x0201
	wmLButtonUp     = 0x0202
	wmRButtonUp     = 0x0205
	wmHotKey        = 0x0312
	wmExitSizeMove  = 0x0232
	wmMouseWheel    = 0x020A
	mkControl       = 0x0008
	htClient        = 1
	htCaption       = 2
	wmApp           = 0x8000
	wmSettings      = wmApp + 1
	wmEditHUD       = wmApp + 3
	wmTray          = wmApp + 2

	swHide           = 0
	swShowNormal     = 1
	swShowNoActivate = 4
	swpNoSize        = 0x0001
	swpNoMove        = 0x0002
	swpNoActivate    = 0x0010
	swpShowWindow    = 0x0040
	hwndTopmost      = ^uintptr(0)
	modControl       = 0x2
	modShift         = 0x4
	modNoRepeat      = 0x4000
	vkTab            = 0x09
	vkShift          = 0x10
	vkReturn         = 0x0D
	vkEscape         = 0x1B
	vkSpace          = 0x20
	vkLeft           = 0x25
	vkUp             = 0x26
	vkRight          = 0x27
	vkDown           = 0x28
	vkF9             = 0x78
	vkF10            = 0x79
	vkF11            = 0x7A
	smCXScreen       = 0
	smCYScreen       = 1
	smCXSmIcon       = 49
	smCYSmIcon       = 50
	transparentBk    = 1
	nullPen          = 8
	srcCopy          = 0x00CC0020
	antialiased      = 4
	imageIcon        = 1
	idiApplication   = 32512

	dtCenter     = 0x0001
	dtRight      = 0x0002
	dtVCenter    = 0x0004
	dtWordBreak  = 0x0010
	dtSingleLine = 0x0020
	dtCalcRect   = 0x0400
	dtNoPrefix   = 0x0800
	dtEllipsis   = 0x8000

	mfString    = 0x0000
	mfChecked   = 0x0008
	mfSeparator = 0x0800
	tpmRightBtn = 0x0002
	tpmReturn   = 0x0100
	tpmNoNotify = 0x0080

	nimAdd     = 0
	nimModify  = 1
	nimDelete  = 2
	nifMessage = 0x1
	nifIcon    = 0x2
	nifTip     = 0x4
	nifInfo    = 0x10
	niifInfo   = 0x1

	mbOK        = 0x0
	mbIconError = 0x10
	mbIconInfo  = 0x40
)

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type point struct{ x, y int32 }

type msg struct {
	hwnd     uintptr
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       point
	lPrivate uint32
}

type rect struct{ left, top, right, bottom int32 }

type bitmapInfoHeader struct {
	biSize          uint32
	biWidth         int32
	biHeight        int32
	biPlanes        uint16
	biBitCount      uint16
	biCompression   uint32
	biSizeImage     uint32
	biXPelsPerMeter int32
	biYPelsPerMeter int32
	biClrUsed       uint32
	biClrImportant  uint32
}

type notifyIconData struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         [16]byte
	hBalloonIcon     uintptr
}

func rgb(r, g, b byte) uintptr { return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16 }

// alpha converts an opacity percentage for SetLayeredWindowAttributes.
func alpha(percent int) uintptr { return uintptr(min(max(percent, 0), 100) * 255 / 100) }

var (
	colorPanel  = rgb(14, 17, 22)
	colorRow    = rgb(28, 34, 48)
	colorLine   = rgb(38, 45, 58)
	colorAccent = rgb(224, 83, 61)
	kindColors  = map[string]uintptr{
		hud.KindText:   rgb(230, 233, 239),
		hud.KindMuted:  rgb(139, 149, 167),
		hud.KindGood:   rgb(63, 185, 122),
		hud.KindInfo:   rgb(91, 156, 240),
		hud.KindWarn:   rgb(229, 169, 59),
		hud.KindUrgent: rgb(239, 74, 74),
		hud.KindCoach:  rgb(144, 133, 233),
	}
)

// gdi caches brushes and fonts for a window; GDI objects must be freed explicitly.
type gdi struct {
	scale   float64
	brushes map[uintptr]uintptr
	fonts   []uintptr
}

func newGDI(scale float64) *gdi { return &gdi{scale: scale, brushes: map[uintptr]uintptr{}} }

func (g *gdi) px(v float64) int32 { return int32(v * g.scale) }

func (g *gdi) brush(c uintptr) uintptr {
	if b, ok := g.brushes[c]; ok {
		return b
	}
	b, _, _ := pCreateSolidBrush.Call(c)
	g.brushes[c] = b
	return b
}

func (g *gdi) font(size float64, weight int) uintptr {
	f, _, _ := pCreateFont.Call(uintptr(-g.px(size)), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, antialiased, 0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Segoe UI"))))
	g.fonts = append(g.fonts, f)
	return f
}

func (g *gdi) release() {
	for _, b := range g.brushes {
		pDeleteObject.Call(b)
	}
	for _, f := range g.fonts {
		pDeleteObject.Call(f)
	}
}

func (g *gdi) fill(dc uintptr, r rect, c uintptr) {
	pFillRect.Call(dc, uintptr(unsafe.Pointer(&r)), g.brush(c))
}

func (g *gdi) text(dc uintptr, s string, r rect, font, c uintptr, format uintptr) int32 {
	pSelectObject.Call(dc, font)
	pSetTextColor.Call(dc, c)
	h, _, _ := pDrawText.Call(dc, uintptr(unsafe.Pointer(utf16(s))), ^uintptr(0), uintptr(unsafe.Pointer(&r)), format|dtNoPrefix)
	return int32(h)
}

// uiScale combines Windows display scaling with screen height, since large screens at 100%
// scaling would otherwise get tiny text.
func uiScale(user float64) float64 {
	dpi := 1.0
	if pGetDpiForSystem.Find() == nil {
		if d, _, _ := pGetDpiForSystem.Call(); d != 0 {
			dpi = float64(d) / 96
		}
	}
	screenH, _, _ := pGetSystemMetrics.Call(smCYScreen)
	return max(user, 0.5) * max(dpi, float64(screenH)/1200)
}

func registerClass(name string, proc func(hwnd, message, wParam, lParam uintptr) uintptr) (*uint16, uintptr, error) {
	hinst, _, _ := pGetModuleHandle.Call(0)
	className := syscall.StringToUTF16Ptr(name)
	wc := wndClassEx{lpfnWndProc: syscall.NewCallback(proc), hInstance: hinst, lpszClassName: className}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	if r, _, err := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return nil, 0, err
	}
	return className, hinst, nil
}

func stockObject(id int) uintptr {
	o, _, _ := pGetStockObject.Call(uintptr(id))
	return o
}

func utf16(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		p, _ = syscall.UTF16PtrFromString("?")
	}
	return p
}

func copyUTF16(dst []uint16, s string) {
	src, _ := syscall.UTF16FromString(s)
	n := copy(dst[:len(dst)-1], src)
	dst[n] = 0
}

// ShellOpen opens a URL or folder with its default Windows handler.
func ShellOpen(target string) {
	pShellExecute.Call(0, uintptr(unsafe.Pointer(utf16("open"))), uintptr(unsafe.Pointer(utf16(target))), 0, 0, swShowNormal)
}

// ShowMessage shows a Windows message box, for errors a windowless app can't print.
func ShowMessage(title, text string, isError bool) {
	flags := uintptr(mbOK | mbIconInfo)
	if isError {
		flags = mbOK | mbIconError
	}
	pMessageBox.Call(0, uintptr(unsafe.Pointer(utf16(text))), uintptr(unsafe.Pointer(utf16(title))), flags)
}
