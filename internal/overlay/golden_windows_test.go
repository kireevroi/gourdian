package overlay

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDrawGolden writes the Windows renderer's frames to GOLDEN_DIR, to compare with an
// earlier run's.
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
		if err := u.canvas.savePNG(filepath.Join(dir, "draw-"+g.name+".png"), h); err != nil {
			t.Fatal(err)
		}
	}
}
