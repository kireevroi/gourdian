package screen

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/png"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	xdraw "golang.org/x/image/draw"
)

// The measurements behind the constants in this package. They need the real hero art, which
// is far too much to keep in the repository, so they skip unless PORTRAITS points at a folder
// of <hero id>.png files:
//
//	PORTRAITS=/tmp/portraits go test ./internal/screen -run TestRecall -v
//
// Fetch them from the same place the dashboard does:
// https://cdn.cloudflare.steamstatic.com/apps/dota2/images/dota_react/heroes/<name>.png
func realArt(t *testing.T) (Table, map[int]image.Image, []int) {
	files, _ := filepath.Glob(filepath.Join(os.Getenv("PORTRAITS"), "*.png"))
	if len(files) == 0 {
		t.Skip("set PORTRAITS to a folder of hero art")
	}
	tab, arts := Table{}, map[int]image.Image{}
	for _, f := range files {
		id, err := strconv.Atoi(filepath.Base(f[:len(f)-4]))
		if err != nil {
			continue
		}
		fh, _ := os.Open(f)
		img, _, err := image.Decode(fh)
		fh.Close()
		if err != nil {
			t.Fatal(err)
		}
		tab.Add(id, img)
		arts[id] = img
	}
	ids := make([]int, 0, len(arts))
	for id := range arts {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return tab, arts, ids
}

// frame paints one portrait into a cell, jittered the way successive frames of a live capture
// differ: a pixel of drift and a little dimming.
func frame(art image.Image, cellW int, rng *rand.Rand) (*image.RGBA, image.Rectangle) {
	cellH := cellW * 9 / 16
	shot := image.NewRGBA(image.Rect(0, 0, cellW+40, cellH+40))
	draw.Draw(shot, shot.Bounds(), image.NewUniform(color.RGBA{18, 20, 26, 255}), image.Point{}, draw.Src)
	dx, dy := rng.IntN(3)-1, rng.IntN(3)-1
	cell := image.Rect(20+dx, 20+dy, 20+dx+cellW, 20+dy+cellH)
	draw.Draw(shot, cell, image.NewUniform(color.RGBA{60, 160, 70, 255}), image.Point{}, draw.Src)
	in := cell.Inset(2)
	xdraw.ApproxBiLinear.Scale(shot, in, art, art.Bounds(), xdraw.Src, nil)
	hp := image.Rect(in.Min.X, in.Max.Y-2, in.Max.X, in.Max.Y)
	draw.Draw(shot, hp, image.NewUniform(color.RGBA{40, 200, 60, 255}), image.Point{}, draw.Src)
	// The calibration is one pixel off from where the portrait landed.
	return shot, image.Rect(20, 20, 20+cellW, 20+cellH)
}

// Reading twice a second for a whole draft is many chances at the same slot, and a slot never
// changes hero once it is picked. This measures what that is worth.
func TestRecallOverAWholeDraft(t *testing.T) {
	tab, arts, ids := realArt(t)
	for _, cellW := range []int{40, 48, 60, 76} {
		for _, frames := range []int{1, 5, 20, 60} {
			everRead, everWrong := 0, 0
			for _, id := range ids {
				rng := rand.New(rand.NewPCG(uint64(id), 1))
				got, wrong := 0, false
				for range frames {
					shot, cell := frame(arts[id], cellW, rng)
					if read, ok := tab.Match(Of(shot, cell)); ok {
						if read == id {
							got = read
						} else {
							wrong = true
						}
					}
				}
				if got != 0 {
					everRead++
				}
				if wrong {
					everWrong++
				}
			}
			fmt.Printf("cell %3dpx over %2d frames: read %3d/%d heroes, wrong on %d\n", cellW, frames, everRead, len(ids), everWrong)
		}
		fmt.Println()
	}
}
