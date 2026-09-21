package screen

import (
	"fmt"
	"image"
	"testing"

	xdraw "golang.org/x/image/draw"
)

// The guess must land on the geometry actually measured off a 2560x1440 frame.
func TestPredictMatchesWhatWasMeasured(t *testing.T) {
	got := Predict(image.Rect(0, 0, 2560, 1440))
	for _, c := range []struct {
		name      string
		got, want Box
	}{{"left", got.Left, measuredBar.Left}, {"right", got.Right, measuredBar.Right}} {
		for _, d := range []struct {
			what      string
			got, want int
		}{{"x", c.got.X, c.want.X}, {"y", c.got.Y, c.want.Y}, {"width", c.got.W, c.want.W}, {"height", c.got.H, c.want.H}} {
			if off := d.got - d.want; off > 4 || off < -4 {
				t.Errorf("%s %s = %d, measured %d", c.name, d.what, d.got, d.want)
			}
		}
	}
}

// The guess must scale: half the height, half of everything, still centred.
func TestPredictScalesWithHeight(t *testing.T) {
	full := Predict(image.Rect(0, 0, 2560, 1440))
	half := Predict(image.Rect(0, 0, 1280, 720))
	if half.Left.H*2 != full.Left.H && half.Left.H*2 != full.Left.H-1 {
		t.Errorf("height %d at 720p, %d at 1440p", half.Left.H, full.Left.H)
	}
	// Ultrawide is the same height, so the portraits are the same size, just further out.
	wide := Predict(image.Rect(0, 0, 3440, 1440))
	if wide.Left.W != full.Left.W || wide.Left.H != full.Left.H {
		t.Errorf("an ultrawide screen changed the portrait size: %+v against %+v", wide.Left, full.Left)
	}
	if wide.Left.X-full.Left.X != (3440-2560)/2 {
		t.Errorf("the bar didn't stay in the middle: %d against %d", wide.Left.X, full.Left.X)
	}
	if Predict(image.Rect(0, 0, 100, 0)).Ready() {
		t.Error("a screen with no height still gave a bar")
	}
}

// TestPredictOnOtherScreenSizes resamples a real frame to other resolutions, which is close
// to what Dota would draw at them, and checks the guess still reads the same heroes.
func TestPredictOnOtherScreenSizes(t *testing.T) {
	files := loadFrames(t)
	table, names := namedTable(t)
	src := openFrame(t, files[1]) // frame02: the draft, not the desktop
	for _, size := range []image.Point{{2560, 1440}, {1920, 1080}, {1600, 900}, {1366, 768}, {3840, 2160}} {
		shot := image.NewRGBA(image.Rect(0, 0, size.X, size.Y))
		xdraw.CatmullRom.Scale(shot, shot.Bounds(), src, src.Bounds(), xdraw.Src, nil)
		bar := Predict(shot.Bounds())
		right, wrong, unknown := 0, 0, 0
		for slot, want := range truth {
			id, ok := table.Match(Of(shot, bar.Cell(slot)))
			switch {
			case !ok:
				unknown++
			case names[id] == want:
				right++
			default:
				wrong++
			}
		}
		fmt.Printf("%4dx%-4d portrait %3dx%-3d: read %d, unsure %d, WRONG %d\n",
			size.X, size.Y, bar.Left.W/Slots, bar.Left.H, right, unknown, wrong)
		if wrong > 0 {
			t.Errorf("%dx%d read %d slots as the wrong hero", size.X, size.Y, wrong)
		}
	}
}
