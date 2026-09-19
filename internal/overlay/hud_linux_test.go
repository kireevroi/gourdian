//go:build linux

package overlay

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/composite"
	"github.com/jezek/xgb/shape"
	"github.com/jezek/xgb/xproto"

	"gourdian/internal/config"
	"gourdian/internal/hud"
)

// TestX11HUD shows the HUD on the real X display for a few seconds and reads it back.
func TestX11HUD(t *testing.T) {
	if os.Getenv("X11_HUD") == "" {
		t.Skip("set X11_HUD=1 to open the HUD on this X display")
	}
	editNow := make(chan struct{})
	var mu sync.Mutex
	var statuses []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/settings":
			json.NewEncoder(w).Encode(settingsView{Settings: config.Default().Settings})
		case "/api/overlay/status":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			statuses = append(statuses, body)
			mu.Unlock()
		case "/events":
			w.Header().Set("Content-Type", "text/event-stream")
			snap, _ := json.Marshal(map[string]any{"in_match": true, "clock": 300})
			payload, _ := json.Marshal(hud.Payload{Live: hud.SampleIn(config.DefaultWidgets(), "ru")})
			fmt.Fprintf(w, "event: snapshot\ndata: %s\n\nevent: hud\ndata: %s\n\n", snap, payload)
			w.(http.Flusher).Flush()
			select {
			case <-editNow:
				fmt.Fprint(w, "event: hud_edit\ndata: {}\n\n")
				w.(http.Flusher).Flush()
			case <-r.Context().Done():
			}
			<-r.Context().Done()
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{URL: srv.URL, Scale: 1, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	}()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()

	conn, err := xgb.NewConn()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	win := findHUD(t, conn)
	time.Sleep(500 * time.Millisecond)
	attrs, _ := xproto.GetWindowAttributes(conn, win).Reply()
	geo, _ := xproto.GetGeometry(conn, xproto.Drawable(win)).Reply()
	t.Logf("HUD window %d: %dx%d at %d,%d, depth %d, override-redirect %v", win, geo.Width, geo.Height, geo.X, geo.Y, geo.Depth, attrs.OverrideRedirect)
	if geo.Depth != 32 || !attrs.OverrideRedirect || geo.Height < 300 {
		t.Fatal("the HUD should be a tall 32-bit override-redirect window")
	}
	if err := shape.Init(conn); err == nil {
		rects, err := shape.GetRectangles(conn, win, shape.SkInput).Reply()
		if err != nil || len(rects.Rectangles) != 0 {
			t.Fatalf("clicks should go through the HUD: input region %v (%v)", rects, err)
		}
	}
	img := readBack(t, conn, win, geo)
	alpha := func(x, y int) byte { return img.RGBAAt(x, y).A }
	if alpha(int(geo.Width)-1, 0) != 0 || alpha(int(geo.Width)/2, 30) == 0 {
		t.Fatalf("corner alpha %d, panel alpha %d: the HUD should be see-through around its panels", alpha(int(geo.Width)-1, 0), alpha(int(geo.Width)/2, 30))
	}
	close(editNow)
	var editable bool
	for deadline := time.Now().Add(3 * time.Second); !editable && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		rects, err := shape.GetRectangles(conn, win, shape.SkInput).Reply()
		editable = err == nil && len(rects.Rectangles) == 1
	}
	if !editable {
		t.Fatal("while its layout is edited, the HUD takes the mouse")
	}
	mu.Lock()
	defer mu.Unlock()
	t.Logf("status reports: %v", statuses)
	for _, s := range statuses {
		if e, ok := s["hud_error"]; ok && e != "" {
			t.Fatalf("the HUD reported an error: %v", e)
		}
	}
}

func findHUD(t *testing.T, conn *xgb.Conn) xproto.Window {
	t.Helper()
	root := xproto.Setup(conn).DefaultScreen(conn).Root
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		tree, err := xproto.QueryTree(conn, root).Reply()
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range tree.Children {
			name, err := xproto.GetProperty(conn, false, c, xproto.AtomWmName, xproto.AtomString, 0, 64).Reply()
			if err != nil || string(name.Value) != "Gourdian HUD" {
				continue
			}
			if a, err := xproto.GetWindowAttributes(conn, c).Reply(); err == nil && a.MapState == xproto.MapStateViewable {
				return c
			}
		}
	}
	t.Fatal("no HUD window was mapped")
	return 0
}

// readBack gets the window's own pixels through Composite, wherever it is on screen.
func readBack(t *testing.T, conn *xgb.Conn, win xproto.Window, geo *xproto.GetGeometryReply) *image.RGBA {
	t.Helper()
	if err := composite.Init(conn); err != nil {
		t.Skip("no Composite extension to read the HUD back:", err)
	}
	pix, _ := xproto.NewPixmapId(conn)
	if err := composite.NameWindowPixmapChecked(conn, win, pix).Check(); err != nil {
		t.Fatal(err)
	}
	defer xproto.FreePixmap(conn, pix)
	r, err := xproto.GetImage(conn, xproto.ImageFormatZPixmap, xproto.Drawable(pix), 0, 0, geo.Width, geo.Height, ^uint32(0)).Reply()
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, int(geo.Width), int(geo.Height)))
	for i := 0; i+3 < len(r.Data) && i+3 < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = r.Data[i+2], r.Data[i+1], r.Data[i], r.Data[i+3]
	}
	return img
}

// TestGrabHUD saves what a running trainer's HUD shows, for looking at it.
func TestGrabHUD(t *testing.T) {
	out := os.Getenv("GRAB_HUD")
	if out == "" {
		t.Skip("set GRAB_HUD to a PNG path to save the running HUD")
	}
	conn, err := xgb.NewConn()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	win := findHUD(t, conn)
	geo, err := xproto.GetGeometry(conn, xproto.Drawable(win)).Reply()
	if err != nil {
		t.Fatal(err)
	}
	if err := savePaintedPNG(out, readBack(t, conn, win, geo)); err != nil {
		t.Fatal(err)
	}
	t.Logf("HUD %dx%d at %d,%d saved to %s", geo.Width, geo.Height, geo.X, geo.Y, out)
}
