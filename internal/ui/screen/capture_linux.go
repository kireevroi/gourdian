//go:build linux

package screen

import (
	"fmt"
	"image"
	"io"
	"log"
	"os"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// Every X connection reads xgb's global logger, so it is silenced once, before any can open.
func init() { xgb.Logger = log.New(io.Discard, "", 0) }

// Size asks the root window: with several monitors the announced size covers pixels X won't return.
func Size() (image.Rectangle, error) {
	conn, screen, err := connect()
	if err != nil {
		return image.Rectangle{}, err
	}
	defer conn.Close()
	return rootSize(conn, screen)
}

func rootSize(conn *xgb.Conn, screen *xproto.ScreenInfo) (image.Rectangle, error) {
	geom, err := xproto.GetGeometry(conn, xproto.Drawable(screen.Root)).Reply()
	if err != nil {
		return image.Rect(0, 0, int(screen.WidthInPixels), int(screen.HeightInPixels)), nil
	}
	return image.Rect(0, 0, int(geom.Width), int(geom.Height)), nil
}

// Grab copies r from the screen; under Wayland the root comes back blank, and the caller is told.
func Grab(r image.Rectangle) (image.Image, error) {
	if r.Empty() {
		return nil, fmt.Errorf("asked for an empty part of the screen")
	}
	conn, screen, err := connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	whole, err := rootSize(conn, screen)
	if err != nil {
		return nil, err
	}
	r = r.Intersect(whole)
	if r.Empty() {
		return nil, fmt.Errorf("that part of the screen is off it")
	}
	reply, err := xproto.GetImage(conn, xproto.ImageFormatZPixmap, xproto.Drawable(screen.Root),
		int16(r.Min.X), int16(r.Min.Y), uint16(r.Dx()), uint16(r.Dy()), 0xffffffff).Reply()
	if err != nil {
		// Usually XWayland or WSLg, where the root window isn't the desktop you can see.
		return nil, fmt.Errorf("the X server wouldn't hand back the screen (%w). "+
			"Under WSL or Wayland the desktop isn't the X root window, so it can't be read from here", err)
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

// Wayland reports whether reading the root window gives nothing, so the caller can say why.
func Wayland() bool {
	return os.Getenv("WAYLAND_DISPLAY") != "" && os.Getenv("XDG_SESSION_TYPE") == "wayland"
}
