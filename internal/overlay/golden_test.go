package overlay

import (
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

// TestPaintGolden writes the pure-Go renderer's frames to GOLDEN_DIR, to compare with an
// earlier run's.
func TestPaintGolden(t *testing.T) {
	dir := os.Getenv("GOLDEN_DIR")
	if dir == "" {
		t.Skip("set GOLDEN_DIR to write the frames")
	}
	for _, g := range goldenViews {
		p, err := newPainter(1, goldenLayout())
		if err != nil {
			t.Fatal(err)
		}
		if err := savePaintedPNG(filepath.Join(dir, "paint-"+g.name+".png"), p.paint(g.view, g.editing)); err != nil {
			t.Fatal(err)
		}
	}
}
