//go:build windows

package screen

import (
	"fmt"
	"image"
	"syscall"
	"unsafe"
)

var (
	user32 = syscall.NewLazyDLL("user32.dll")
	gdi32  = syscall.NewLazyDLL("gdi32.dll")

	pGetDC              = user32.NewProc("GetDC")
	pReleaseDC          = user32.NewProc("ReleaseDC")
	pGetSystemMetrics   = user32.NewProc("GetSystemMetrics")
	pCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	pCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	pSelectObject       = gdi32.NewProc("SelectObject")
	pBitBlt             = gdi32.NewProc("BitBlt")
	pDeleteObject       = gdi32.NewProc("DeleteObject")
	pDeleteDC           = gdi32.NewProc("DeleteDC")
)

const (
	smCXScreen   = 0
	smCYScreen   = 1
	biRGB        = 0
	dibRGBColors = 0
	srcCopy      = 0x00CC0020
	captureBlt   = 0x40000000 // include layered windows, so the game's own HUD is there
)

type bitmapInfoHeader struct {
	Size          uint32
	Width, Height int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// Size is how big the screen is.
func Size() (image.Rectangle, error) {
	w, _, _ := pGetSystemMetrics.Call(smCXScreen)
	h, _, _ := pGetSystemMetrics.Call(smCYScreen)
	if w == 0 || h == 0 {
		return image.Rectangle{}, fmt.Errorf("Windows reports no screen")
	}
	return image.Rect(0, 0, int(w), int(h)), nil
}

// Wayland is never true on Windows; it is here so callers don't have to care which system
// they are on.
func Wayland() bool { return false }

// Grab copies the part of the screen inside r. It reads the desktop as anyone taking a
// screenshot would; nothing is read from the game itself.
func Grab(r image.Rectangle) (image.Image, error) {
	if r.Empty() {
		return nil, fmt.Errorf("asked for an empty part of the screen")
	}
	screen, _, _ := pGetDC.Call(0)
	if screen == 0 {
		return nil, fmt.Errorf("couldn't read the screen")
	}
	defer pReleaseDC.Call(0, screen)

	memory, _, _ := pCreateCompatibleDC.Call(screen)
	if memory == 0 {
		return nil, fmt.Errorf("couldn't make a place to copy the screen into")
	}
	defer pDeleteDC.Call(memory)

	// A negative height asks for the rows top down, the way an image wants them.
	info := bitmapInfoHeader{
		Size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), Width: int32(r.Dx()), Height: int32(-r.Dy()),
		Planes: 1, BitCount: 32, Compression: biRGB,
	}
	var pixels unsafe.Pointer
	bitmap, _, _ := pCreateDIBSection.Call(memory, uintptr(unsafe.Pointer(&info)), dibRGBColors,
		uintptr(unsafe.Pointer(&pixels)), 0, 0)
	if bitmap == 0 || pixels == nil {
		return nil, fmt.Errorf("couldn't make a bitmap of %dx%d", r.Dx(), r.Dy())
	}
	defer pDeleteObject.Call(bitmap)

	old, _, _ := pSelectObject.Call(memory, bitmap)
	defer pSelectObject.Call(memory, old)

	ok, _, err := pBitBlt.Call(memory, 0, 0, uintptr(r.Dx()), uintptr(r.Dy()),
		screen, uintptr(r.Min.X), uintptr(r.Min.Y), srcCopy|captureBlt)
	if ok == 0 {
		return nil, fmt.Errorf("couldn't copy the screen: %w", err)
	}

	// Windows hands back blue, green, red, unused; an image wants red, green, blue, alpha.
	raw := unsafe.Slice((*byte)(pixels), r.Dx()*r.Dy()*4)
	img := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	for i := 0; i < len(raw); i += 4 {
		img.Pix[i] = raw[i+2]
		img.Pix[i+1] = raw[i+1]
		img.Pix[i+2] = raw[i]
		img.Pix[i+3] = 255
	}
	return img, nil
}
