package overlay

import (
	"os"
	"testing"

	"gourdian/internal/sys/config"
	"gourdian/internal/ui/hud"
)

func TestPaintLaysTheHUDOut(t *testing.T) {
	o := config.Default().Settings.Overlay
	p, err := newPainter(1, o)
	if err != nil {
		t.Fatal(err)
	}
	v := View{View: hud.SampleIn(config.DefaultWidgets(), "ru")}
	img := p.paint(v, false)
	if img.Bounds().Dx() != o.HUDWidth || img.Bounds().Dy() < 300 {
		t.Fatalf("painted %v", img.Bounds())
	}
	if a := img.RGBAAt(img.Bounds().Dx()-1, 0).A; a != 0 {
		t.Fatalf("rounded corners are see-through, got alpha %d", a)
	}
	if a := img.RGBAAt(img.Bounds().Dx()/2, 30).A; a == 0 {
		t.Fatal("panels have a background")
	}
	editing := p.paint(View{}, true)
	if editing.Bounds().Dy() != p.px(200) {
		t.Fatalf("an empty HUD being edited still shows its area: %v", editing.Bounds())
	}
	o.HUDBackground = 0
	p, _ = newPainter(1, o)
	clear := p.paint(v, false)
	if a := clear.RGBAAt(clear.Bounds().Dx()-20, 30).A; a != 0 {
		t.Fatalf("at 0%% background only the text shows, got alpha %d", a)
	}
	if out := os.Getenv("PAINT_PNG"); out != "" {
		o.HUDBackground = 60
		p, _ = newPainter(1, o)
		if err := savePaintedPNG(out, p.paint(v, false)); err != nil {
			t.Fatal(err)
		}
		if err := savePaintedPNG(out+".edit.png", p.paint(v, true)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWrapKeepsWordsWhole(t *testing.T) {
	p, _ := newPainter(1, config.Default().Settings.Overlay)
	lines := wrap(p.small, "Next item: Battle Fury · 650g to go · carry, 180 won pro games", 200)
	if len(lines) < 2 {
		t.Fatalf("lines = %q", lines)
	}
	for _, l := range lines {
		if l == "" || l[0] == ' ' {
			t.Fatalf("lines = %q", lines)
		}
	}
	if got := wrap(p.small, "Supercalifragilisticexpialidocious", 60); len(got) < 2 {
		t.Fatalf("a word wider than the HUD is split: %q", got)
	}
}
