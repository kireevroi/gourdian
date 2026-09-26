package overlay

import (
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"time"

	"gourdian/internal/ui/screen"
)

const keepDrafts = 5

// framesDir is where saved draft frames go, or nothing when the overlay has no data folder.
func (o Options) framesDir() string {
	if o.AppDir == "" {
		return ""
	}
	return filepath.Join(o.AppDir, "draft-frames")
}

// frameKeeper saves the top of each frame the draft reader looked at, and what it read there.
// A whole 1440p frame every two seconds would be hundreds of megabytes a draft.
type frameKeeper struct {
	root string // where each draft gets a folder; empty keeps nothing
	dir  string // this draft's folder, made on the first frame
	n    int
}

type frameNote struct {
	Frame  string     `json:"frame"`
	At     time.Time  `json:"at"`
	Screen string     `json:"screen"` // the whole screen, which the saved strip is the top of
	Bar    screen.Bar `json:"bar"`
	// Seen is this frame alone, Settled every frame so far: radiant 0 to 4, then dire 5 to 9.
	Seen    [2 * screen.Slots]int `json:"seen"`
	Settled [2 * screen.Slots]int `json:"settled"`
	Dire    bool                  `json:"dire"`
}

func stripOf(where image.Rectangle, bar screen.Bar) image.Rectangle {
	h := max(where.Dy()/6, 2*(bar.Y()+bar.Left.H))
	return image.Rect(where.Min.X, where.Min.Y, where.Max.X, min(where.Min.Y+h, where.Max.Y))
}

func (k *frameKeeper) save(shot image.Image, where image.Rectangle, note frameNote) error {
	if k.root == "" {
		return nil
	}
	if k.dir == "" {
		dir := filepath.Join(k.root, time.Now().Format("2006-01-02_15-04-05"))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		k.dir, k.n = dir, 0
		k.prune()
	}
	k.n++
	note.Frame = fmt.Sprintf("frame%03d.png", k.n)
	strip := stripOf(where, note.Bar)
	out := image.NewRGBA(image.Rect(0, 0, strip.Dx(), strip.Dy()))
	draw.Draw(out, out.Bounds(), shot, strip.Min, draw.Src)
	if err := writePNG(filepath.Join(k.dir, note.Frame), out); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(k.dir, "reading.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	err = json.NewEncoder(f).Encode(note)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}

func (k *frameKeeper) done() { k.dir = "" }

// prune keeps the newest drafts; folders are named by start time, so they sort in time order.
func (k *frameKeeper) prune() {
	entries, err := os.ReadDir(k.root)
	if err != nil {
		return
	}
	var drafts []string
	for _, e := range entries {
		if e.IsDir() {
			drafts = append(drafts, e.Name())
		}
	}
	slices.Sort(drafts)
	for _, old := range drafts[:max(0, len(drafts)-keepDrafts)] {
		os.RemoveAll(filepath.Join(k.root, old))
	}
}

func writePNG(name string, img image.Image) error {
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	err = png.Encode(f, img)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}
