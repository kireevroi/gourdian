//go:build windows

package overlay

import (
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"unsafe"
)

var (
	pUpdateLayeredWindow = user32.NewProc("UpdateLayeredWindow")
)

const (
	ulwAlpha   = 0x2
	acSrcOver  = 0x0
	acSrcAlpha = 0x1
)

type blendFunction struct {
	BlendOp, BlendFlags, SourceConstantAlpha, AlphaFormat byte
}

type size struct{ cx, cy int32 }

// dib is a top-down 32-bit bitmap selected into its own memory DC, with direct pixel access.
type dib struct {
	w, h          int32
	dc, bmp, prev uintptr
	px            []byte // BGRA
}

func newDIB(w, h int32) (*dib, error) {
	screen, _, _ := pGetDC.Call(0)
	defer pReleaseDC.Call(0, screen)
	dc, _, _ := pCreateCompatibleDC.Call(screen)
	bmi := bitmapInfoHeader{biWidth: w, biHeight: -h, biPlanes: 1, biBitCount: 32}
	bmi.biSize = uint32(unsafe.Sizeof(bmi))
	var bits unsafe.Pointer
	bmp, _, _ := pCreateDIBSection.Call(dc, uintptr(unsafe.Pointer(&bmi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == nil {
		pDeleteDC.Call(dc)
		return nil, errors.New("create HUD bitmap")
	}
	prev, _, _ := pSelectObject.Call(dc, bmp)
	pSetBkMode.Call(dc, transparentBk)
	return &dib{w: w, h: h, dc: dc, bmp: bmp, prev: prev, px: unsafe.Slice((*byte)(bits), int(w)*int(h)*4)}, nil
}

func (d *dib) release() {
	pSelectObject.Call(d.dc, d.prev)
	pDeleteObject.Call(d.bmp)
	pDeleteDC.Call(d.dc)
}

// canvas holds the HUD image in premultiplied alpha, plus a mask that GDI draws text into:
// white text on black gives each pixel's coverage, which is then blended in any colour.
type canvas struct {
	img, mask *dib
}

func newCanvas(w, h int32) (*canvas, error) {
	img, err := newDIB(w, h)
	if err != nil {
		return nil, err
	}
	mask, err := newDIB(w, h)
	if err != nil {
		img.release()
		return nil, err
	}
	pSetTextColor.Call(mask.dc, rgb(255, 255, 255))
	return &canvas{img: img, mask: mask}, nil
}

func (c *canvas) release() {
	c.img.release()
	c.mask.release()
}

func (c *canvas) clear() { clear(c.img.px) }

// blend draws colour col with alpha a (0-255) over the pixel at x, y.
func (c *canvas) blend(x, y int32, col uintptr, a uint32) {
	if a == 0 || x < 0 || y < 0 || x >= c.img.w || y >= c.img.h {
		return
	}
	i := int(y*c.img.w+x) * 4
	p := c.img.px[i : i+4 : i+4]
	inv := 255 - a
	r, g, b := uint32(col&0xFF), uint32(col>>8&0xFF), uint32(col>>16&0xFF)
	p[0] = byte((b*a + uint32(p[0])*inv) / 255)
	p[1] = byte((g*a + uint32(p[1])*inv) / 255)
	p[2] = byte((r*a + uint32(p[2])*inv) / 255)
	p[3] = byte(a + uint32(p[3])*inv/255)
}

func (c *canvas) fillRect(r rect, col uintptr, a uint32) {
	for y := max(r.top, 0); y < min(r.bottom, c.img.h); y++ {
		for x := max(r.left, 0); x < min(r.right, c.img.w); x++ {
			c.blend(x, y, col, a)
		}
	}
}

// fillRound fills a rounded rectangle with anti-aliased corners.
func (c *canvas) fillRound(r rect, radius int32, col uintptr, a uint32) {
	rad := float64(radius)
	for y := max(r.top, 0); y < min(r.bottom, c.img.h); y++ {
		for x := max(r.left, 0); x < min(r.right, c.img.w); x++ {
			cx, cy := -1.0, -1.0
			switch {
			case x < r.left+radius:
				cx = float64(r.left+radius) - float64(x) - 0.5
			case x >= r.right-radius:
				cx = float64(x) + 0.5 - float64(r.right-radius)
			}
			switch {
			case y < r.top+radius:
				cy = float64(r.top+radius) - float64(y) - 0.5
			case y >= r.bottom-radius:
				cy = float64(y) + 0.5 - float64(r.bottom-radius)
			}
			cover := 1.0
			if cx > 0 && cy > 0 {
				cover = min(max(rad+0.5-math.Hypot(cx, cy), 0), 1)
			}
			c.blend(x, y, col, uint32(float64(a)*cover))
		}
	}
}

// measure returns the height text takes when wrapped to width.
func (c *canvas) measure(s string, width int32, font uintptr) int32 {
	pSelectObject.Call(c.mask.dc, font)
	r := rect{0, 0, width, 0}
	pDrawText.Call(c.mask.dc, uintptr(unsafe.Pointer(utf16(s))), ^uintptr(0), uintptr(unsafe.Pointer(&r)), dtWordBreak|dtCalcRect|dtNoPrefix)
	return r.bottom - r.top
}

// textWidth is the width one line of text takes.
func (c *canvas) textWidth(s string, font uintptr) int32 {
	pSelectObject.Call(c.mask.dc, font)
	r := rect{0, 0, c.mask.w, 0}
	pDrawText.Call(c.mask.dc, uintptr(unsafe.Pointer(utf16(s))), ^uintptr(0), uintptr(unsafe.Pointer(&r)), dtSingleLine|dtCalcRect|dtNoPrefix)
	return r.right - r.left
}

// text draws s into r in colour col, with a dark shadow when shadow is set.
func (c *canvas) text(s string, r rect, font, col uintptr, format uintptr, shadow bool) {
	m := c.mask
	area := rect{max(r.left-2, 0), max(r.top-2, 0), min(r.right+2, m.w), min(r.bottom+2, m.h)}
	for y := area.top; y < area.bottom; y++ {
		clear(m.px[int(y*m.w+area.left)*4 : int(y*m.w+area.right)*4])
	}
	pSelectObject.Call(m.dc, font)
	pDrawText.Call(m.dc, uintptr(unsafe.Pointer(utf16(s))), ^uintptr(0), uintptr(unsafe.Pointer(&r)), format|dtNoPrefix)
	coverage := func(x, y int32) uint32 {
		if x < area.left || y < area.top || x >= area.right || y >= area.bottom {
			return 0
		}
		i := int(y*m.w+x) * 4
		return uint32(max(m.px[i], m.px[i+1], m.px[i+2]))
	}
	if shadow {
		for y := area.top; y < area.bottom; y++ {
			for x := area.left; x < area.right; x++ {
				a := max(coverage(x-1, y-1), coverage(x-1, y), coverage(x, y-1))
				c.blend(x, y, 0, a*200/255)
			}
		}
	}
	for y := area.top; y < area.bottom; y++ {
		for x := area.left; x < area.right; x++ {
			c.blend(x, y, col, coverage(x, y))
		}
	}
}

// present shows the top h rows of the canvas on the layered window at the given opacity.
func (c *canvas) present(hwnd uintptr, h int32, opacity uintptr) {
	screen, _, _ := pGetDC.Call(0)
	defer pReleaseDC.Call(0, screen)
	sz := size{c.img.w, min(max(h, 1), c.img.h)}
	var src point
	blend := blendFunction{BlendOp: acSrcOver, SourceConstantAlpha: byte(opacity), AlphaFormat: acSrcAlpha}
	pUpdateLayeredWindow.Call(hwnd, screen, 0, uintptr(unsafe.Pointer(&sz)), c.img.dc,
		uintptr(unsafe.Pointer(&src)), 0, uintptr(unsafe.Pointer(&blend)), ulwAlpha)
}

// savePNG writes the top h rows with their real transparency.
func (c *canvas) savePNG(path string, h int32) error {
	w := c.img.w
	h = min(max(h, 1), c.img.h)
	out := image.NewNRGBA(image.Rect(0, 0, int(w), int(h)))
	for y := range h {
		for x := range w {
			i := int(y*w+x) * 4
			p := c.img.px[i : i+4]
			a := uint32(p[3])
			if a == 0 {
				continue
			}
			out.SetNRGBA(int(x), int(y), color.NRGBA{byte(uint32(p[2]) * 255 / a), byte(uint32(p[1]) * 255 / a), byte(uint32(p[0]) * 255 / a), byte(a)})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, out)
}
