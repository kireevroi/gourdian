package screen

import (
	"fmt"
	"image"
	"math/rand/v2"
	"sort"
	"testing"
)

// ratioOf is how much closer the nearest hero is than the nearest other hero.
func ratioOf(img image.Image, r image.Rectangle, t Table) float64 {
	s := Of(img, r)
	best, second := -1, -1
	for _, arts := range t {
		near := -1
		for _, a := range arts {
			if d := s.Distance(a); near < 0 || d < near {
				near = d
			}
		}
		switch {
		case best < 0 || near < best:
			best, second = near, best
		case second < 0 || near < second:
			second = near
		}
	}
	if second <= 0 {
		return 1
	}
	return float64(best) / float64(second)
}

// TestThreshold puts the ratios of real portraits beside the ratios of places on the screen
// that hold no portrait at all, so the line between them is drawn from measurement rather
// than from taste.
func TestThreshold(t *testing.T) {
	files := loadFrames(t)
	table, _ := namedTable(t)
	var real, empty []float64
	for _, path := range files[2:] { // the first frames are the menu, not the bar
		img := openFrame(t, path)
		bar := Predict(img.Bounds())
		for slot := range truth {
			real = append(real, ratioOf(img, bar.Cell(slot), table))
		}
		// Rectangles the size of a portrait, anywhere but along the bar.
		rng := rand.New(rand.NewPCG(1, uint64(len(path))))
		w, h := bar.Left.W/Slots, bar.Left.H
		for range 60 {
			x := rng.IntN(img.Bounds().Dx() - w)
			y := bar.Y() + h + rng.IntN(img.Bounds().Dy()-bar.Y()-2*h)
			empty = append(empty, ratioOf(img, image.Rect(x, y, x+w, y+h), table))
		}
		// And along the bar's own line, but between the two runs where the clock is.
		mid := img.Bounds().Dx() / 2
		for _, x := range []int{mid - w/2, mid - w, mid + w/2} {
			empty = append(empty, ratioOf(img, image.Rect(x, bar.Y(), x+w, bar.Y()+h), table))
		}
	}
	sort.Float64s(real)
	sort.Float64s(empty)
	fmt.Printf("portraits (%d): worst %.2f, 90th %.2f, median %.2f\n",
		len(real), real[len(real)-1], real[len(real)*9/10], real[len(real)/2])
	fmt.Printf("not portraits (%d): best %.2f, 5th %.2f, 25th %.2f, median %.2f\n",
		len(empty), empty[0], empty[len(empty)/20], empty[len(empty)/4], empty[len(empty)/2])
	for _, cut := range []float64{0.70, 0.80, 0.85, 0.90, 0.95} {
		taken, false_ := 0, 0
		for _, r := range real {
			if r <= cut {
				taken++
			}
		}
		for _, r := range empty {
			if r <= cut {
				false_++
			}
		}
		fmt.Printf("  cut %.2f: portraits taken %3d/%d, not-portraits taken %2d/%d\n",
			cut, taken, len(real), false_, len(empty))
	}
}
