package overlay

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"gourdian/internal/config"
	"gourdian/internal/hud"
)

// goldenViews are fixed HUD contents for comparing renders before and after a change.
var goldenViews = []struct {
	name    string
	view    View
	editing bool
}{
	{"alert", View{View: hud.View{Alert: &hud.Line{Text: "Power rune in 15 seconds", Kind: hud.KindWarn}, More: 2,
		Rows: []hud.Line{line("Mid · safe lane", hud.KindCoach), line("Last hits 43 of 50 by 10:00", hud.KindGood), line("Blink Dagger by 14:00: 820 gold to go", hud.KindInfo)}}}, false},
	{"kinds", View{View: hud.View{Rows: []hud.Line{line("text", hud.KindText), line("muted", hud.KindMuted), line("good", hud.KindGood),
		line("info", hud.KindInfo), line("warn", hud.KindWarn), line("urgent: no buyback gold", hud.KindUrgent), line("coach says hi", hud.KindCoach)}}}, false},
	{"banner", View{Banner: "Gourdian · Ctrl+Shift+F10 move HUD · Ctrl+Shift+F11 dashboard"}, false},
	{"editing", View{View: hud.View{Rows: []hud.Line{line("a row that is long enough to wrap onto a second line of the HUD", hud.KindText)}}}, true},
}

func line(text, kind string) hud.Line { return hud.Line{Text: text, Kind: kind} }

func goldenLayout() config.OverlaySettings { return config.Default().Settings.Overlay }

// TestPaintGolden renders the fixed views and compares them pixel by pixel with the PNGs in
// testdata, so any change to the HUD's layout, colours or text has to be looked at and
// accepted. Run with UPDATE_GOLDEN=1 to accept the current renders, then look at the diff.
func TestPaintGolden(t *testing.T) {
	for _, g := range goldenViews {
		t.Run(g.name, func(t *testing.T) {
			p, err := newPainter(1, goldenLayout())
			if err != nil {
				t.Fatal(err)
			}
			got := p.paint(g.view, g.editing)
			path := filepath.Join("testdata", "paint-"+g.name+".png")
			if os.Getenv("UPDATE_GOLDEN") != "" {
				if err := savePaintedPNG(path, got); err != nil {
					t.Fatal(err)
				}
				t.Log("wrote " + path)
				return
			}
			want, err := readPNG(path)
			if err != nil {
				t.Fatalf("%v; run the test with UPDATE_GOLDEN=1 to make it", err)
			}
			if err := samePixels(paintedNRGBA(got), want); err != nil {
				out := filepath.Join(t.TempDir(), "paint-"+g.name+".png")
				savePaintedPNG(out, got)
				t.Fatalf("%v\nthe render is at %s; if the change is meant, rerun with UPDATE_GOLDEN=1", err, out)
			}
		})
	}
}

func readPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

// samePixels says where two renders first differ, in the HUD's own coordinates.
func samePixels(got, want image.Image) error {
	if got.Bounds() != want.Bounds() {
		return fmt.Errorf("the render is %v, the golden is %v", got.Bounds(), want.Bounds())
	}
	for y := got.Bounds().Min.Y; y < got.Bounds().Max.Y; y++ {
		for x := got.Bounds().Min.X; x < got.Bounds().Max.X; x++ {
			gr, gg, gb, ga := got.At(x, y).RGBA()
			wr, wg, wb, wa := want.At(x, y).RGBA()
			if gr != wr || gg != wg || gb != wb || ga != wa {
				return fmt.Errorf("pixel %d,%d is %v, the golden has %v", x, y, got.At(x, y), want.At(x, y))
			}
		}
	}
	return nil
}
