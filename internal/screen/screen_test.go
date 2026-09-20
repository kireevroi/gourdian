package screen

import (
	"image"
	"image/color"
	"image/draw"
	"math/rand/v2"
	"testing"

	xdraw "golang.org/x/image/draw"
)

// portrait invents a hero's art: a low-frequency pattern, so two heroes look as different to
// a difference hash as two real portraits do, and the same hero stays itself at any size.
func portrait(heroID int) image.Image {
	rng := rand.New(rand.NewPCG(uint64(heroID), 7))
	seed := image.NewRGBA(image.Rect(0, 0, 12, 10))
	for y := range 10 {
		for x := range 12 {
			v := uint8(rng.IntN(256))
			seed.Set(x, y, color.RGBA{v, uint8(rng.IntN(256)), uint8(rng.IntN(256)), 255})
		}
	}
	big := image.NewRGBA(image.Rect(0, 0, 256, 144))
	xdraw.CatmullRom.Scale(big, big.Bounds(), seed, seed.Bounds(), xdraw.Src, nil)
	return big
}

func table(heroes []int) Table {
	t := Table{}
	for _, id := range heroes {
		t.Add(id, portrait(id))
	}
	return t
}

var heroes = []int{1, 2, 8, 11, 14, 17, 22, 26, 35, 41, 44, 49, 53, 62, 74, 76, 86, 94, 101, 114}

// screenshot paints a 1920x1080 screen with the given heroes in the bar, drawn small, framed
// in team colours and with a health bar over the bottom, the way the game does.
func screenshot(bar Bar, slots [2 * Slots]int) image.Image {
	shot := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	draw.Draw(shot, shot.Bounds(), image.NewUniform(color.RGBA{18, 20, 26, 255}), image.Point{}, draw.Src)
	for i, id := range slots {
		if id == 0 {
			continue
		}
		cell := bar.Cell(i)
		frame := color.RGBA{60, 160, 70, 255}
		if i >= Slots {
			frame = color.RGBA{190, 60, 50, 255}
		}
		draw.Draw(shot, cell, image.NewUniform(frame), image.Point{}, draw.Src)
		art := cell.Inset(3)
		xdraw.ApproxBiLinear.Scale(shot, art, portrait(id), portrait(id).Bounds(), xdraw.Src, nil)
		// A health bar along the bottom, as Dota paints over the portrait.
		bar := image.Rect(art.Min.X, art.Max.Y-3, art.Max.X, art.Max.Y)
		draw.Draw(shot, bar, image.NewUniform(color.RGBA{40, 200, 60, 255}), image.Point{}, draw.Src)
	}
	return shot
}

var bar = Bar{Left: Box{X: 560, Y: 6, W: 380, H: 46}, Right: Box{X: 1000, Y: 6, W: 380, H: 46}}

func TestReadingAFullBar(t *testing.T) {
	want := [2 * Slots]int{1, 8, 14, 26, 35, 44, 53, 74, 86, 101}
	got := bar.Read(screenshot(bar, want), table(heroes))
	if got != want {
		t.Errorf("read %v, want %v", got, want)
	}
}

// Mid-draft most slots are still empty, and an empty slot must come back empty rather than as
// whichever hero it happens to resemble.
func TestEmptySlotsStayEmpty(t *testing.T) {
	want := [2 * Slots]int{1, 0, 0, 0, 0, 44, 53, 0, 0, 0}
	got := bar.Read(screenshot(bar, want), table(heroes))
	if got != want {
		t.Errorf("read %v, want %v", got, want)
	}
}

// A hero the table has never seen must not be read as the nearest one it has.
func TestAnUnknownHeroIsNotGuessed(t *testing.T) {
	known := table([]int{1, 8, 14})
	shot := screenshot(bar, [2 * Slots]int{99, 8, 0, 0, 0, 0, 0, 0, 0, 0})
	got := bar.Read(shot, known)
	if got[0] != 0 {
		t.Errorf("an unknown hero was read as %d", got[0])
	}
	if got[1] != 8 {
		t.Errorf("the known hero beside it read as %d, want 8", got[1])
	}
}

// The same portrait at another size is the same hero: the game is drawn at whatever scale the
// player's screen and UI settings give it.
func TestTheHashSurvivesScaling(t *testing.T) {
	t.Parallel()
	for _, size := range []image.Rectangle{
		image.Rect(0, 0, 64, 36), image.Rect(0, 0, 128, 72), image.Rect(0, 0, 512, 288),
	} {
		small := image.NewRGBA(size)
		xdraw.ApproxBiLinear.Scale(small, size, portrait(26), portrait(26).Bounds(), xdraw.Src, nil)
		full := portrait(26)
		near := Of(small, size).Distance(Of(full, full.Bounds()))
		far := Of(small, size).Distance(Of(portrait(44), portrait(44).Bounds()))
		if float64(near) > Ratio*float64(far) {
			t.Errorf("at %v the same portrait is %d away and a different one %d: too close to tell apart", size.Max, near, far)
		}
	}
}

// Every hero must read as itself from a painted bar, whatever else is on it.
func TestEveryHeroReadsAsItself(t *testing.T) {
	tab := table(heroes)
	for _, id := range heroes {
		var slots [2 * Slots]int
		slots[0] = id
		if got := bar.Read(screenshot(bar, slots), tab)[0]; got != id {
			t.Errorf("hero %d read as %d", id, got)
		}
	}
}

// A bar pointed at the wrong part of the screen reads nothing, which is what tells the
// trainer its calibration is wrong.
func TestAMisplacedBarReadsNothing(t *testing.T) {
	shot := screenshot(bar, [2 * Slots]int{1, 8, 14, 26, 35, 44, 53, 74, 86, 101})
	wrong := Bar{Left: Box{X: 560, Y: 400, W: 380, H: 46}, Right: Box{X: 1000, Y: 400, W: 380, H: 46}}
	if n := ReadCount(wrong.Read(shot, table(heroes))); n != 0 {
		t.Errorf("a bar over empty screen read %d heroes", n)
	}
}

func TestABarMustDescribeTenReadablePortraits(t *testing.T) {
	for name, b := range map[string]Bar{
		"nothing at all":    {},
		"only one side":     {Left: bar.Left},
		"too small to read": {Left: Box{X: 0, Y: 0, W: 10, H: 4}, Right: Box{X: 20, Y: 0, W: 10, H: 4}},
	} {
		if b.Ready() {
			t.Errorf("%s was accepted as a bar", name)
		}
	}
	if !bar.Ready() {
		t.Error("a sound bar was rejected")
	}
}

// The trainer finds the bar by looking for the one portrait it is sure of: in a match it
// knows its own hero and which of the ten slots that hero sits in.
func TestFindingTheBarFromOneKnownPortrait(t *testing.T) {
	tab := table(heroes)
	want := [2 * Slots]int{1, 8, 14, 26, 35, 44, 53, 74, 86, 101}
	shot := screenshot(bar, want)

	const slot = 2 // the third portrait on the left run
	found, ok := Find(shot, tab[want[slot]][0])
	if !ok {
		t.Fatal("the portrait wasn't found at all")
	}
	// Find lands on the art, which sits inside the slot the portraits are spaced by; Fit is
	// what turns that into the geometry.
	if truth := bar.Cell(slot); !found.Cell.In(truth) {
		t.Errorf("found the portrait at %v, which is not inside its slot %v", found.Cell, truth)
	}

	got, read, ok := Fit(shot, tab, found.Cell, slot)
	if !ok {
		t.Fatalf("no bar could be fitted around %v", found.Cell)
	}
	if read != 2*Slots {
		t.Errorf("the bar it worked out reads %d of the ten heroes: %+v", read, got)
	}
}

// A screen with no portrait on it must not yield a bar that reads anything.
func TestFindingNothingOnAnEmptyScreen(t *testing.T) {
	shot := screenshot(bar, [2 * Slots]int{})
	found, ok := Find(shot, table(heroes)[26][0])
	if !ok {
		return // nothing found at all is a fine answer
	}
	if _, read, ok := Fit(shot, table(heroes), found.Cell, 0); ok {
		t.Errorf("a bar fitted to an empty screen read %d heroes", read)
	}
}
