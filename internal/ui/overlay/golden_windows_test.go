package overlay

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDrawGolden writes the Windows renderer's frames to GOLDEN_DIR, to compare with an
// earlier run's by eye: GDI draws with the machine's own fonts, so the pixels differ from one
// Windows to another and can't be a golden file like the pure-Go renderer's.
func TestDrawGolden(t *testing.T) {
	dir := os.Getenv("GOLDEN_DIR")
	if dir == "" {
		t.Skip("set GOLDEN_DIR to write the frames")
	}
	for _, g := range goldenViews {
		u := &ui{model: newModel(time.Time{}, false), baseScale: 1, layout: goldenLayout(), editing: g.editing}
		if err := u.setupGDI(); err != nil {
			t.Fatal(err)
		}
		h := u.draw(g.view)
		defer func() { u.g.release(); u.canvas.release() }()
		if err := u.canvas.savePNG(filepath.Join(dir, "draw-"+g.name+".png"), h); err != nil {
			t.Fatal(err)
		}
	}
}
