//go:build linux

package screen

import (
	"fmt"
	"image"
	"os"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// Size is how big the screen is.
func Size() (image.Rectangle, error) {
	conn, screen, err := connect()
	if err != nil {
		return image.Rectangle{}, err
	}
	defer conn.Close()
	return image.Rect(0, 0, int(screen.WidthInPixels), int(screen.HeightInPixels)), nil
}

// Grab copies the part of the screen inside r. Under Wayland the root window is not the
// desktop and comes back blank, which the caller is told about rather than left to wonder at.
func Grab(r image.Rectangle) (image.Image, error) {
	if r.Empty() {
		return nil, fmt.Errorf("asked for an empty part of the screen")
	}
	conn, screen, err := connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	whole := image.Rect(0, 0, int(screen.WidthInPixels), int(screen.HeightInPixels))
	r = r.Intersect(whole)
	if r.Empty() {
		return nil, fmt.Errorf("that part of the screen is off it")
	}
	reply, err := xproto.GetImage(conn, xproto.ImageFormatZPixmap, xproto.Drawable(screen.Root),
		int16(r.Min.X), int16(r.Min.Y), uint16(r.Dx()), uint16(r.Dy()), 0xffffffff).Reply()
	if err != nil {
		return nil, fmt.Errorf("couldn't read the screen: %w", err)
	}
	if len(reply.Data) < r.Dx()*r.Dy()*4 {
		return nil, fmt.Errorf("the X server returned %d bytes for a %dx%d picture", len(reply.Data), r.Dx(), r.Dy())
	}
	// X hands back blue, green, red, unused on the usual true-colour visual.
	img := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	for i := 0; i+3 < len(img.Pix); i += 4 {
		img.Pix[i] = reply.Data[i+2]
		img.Pix[i+1] = reply.Data[i+1]
		img.Pix[i+2] = reply.Data[i]
		img.Pix[i+3] = 255
	}
	return img, nil
}

func connect() (*xgb.Conn, *xproto.ScreenInfo, error) {
	conn, err := xgb.NewConn()
	if err != nil {
		return nil, nil, fmt.Errorf("no X display (DISPLAY=%q), so the screen can't be read: %w", os.Getenv("DISPLAY"), err)
	}
	return conn, xproto.Setup(conn).DefaultScreen(conn), nil
}

// Wayland reports whether the session is one where reading the root window gives nothing. The
// caller uses it to say why rather than showing an empty picture.
func Wayland() bool {
	return os.Getenv("WAYLAND_DISPLAY") != "" && os.Getenv("XDG_SESSION_TYPE") == "wayland"
}
