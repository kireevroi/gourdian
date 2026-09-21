package screen

import "image"

// Dota lays its interface out as though the screen were always 1080 high and scales that to
// whatever it really is, keeping the bar centred. So where the portraits sit can be worked
// out from the screen's height alone, in those same units, measured once off a real frame at
// 2560x1440 and divided back down.
//
// This is only a first guess. A player can scale Dota's interface themselves, and a guess
// that is a few pixels out reads nothing, so the trainer checks it and searches when it
// doesn't work. What the guess buys is not having to search on most screens.
const (
	unitHeight = 1080.0
	// unitCellW and unitCellH are one portrait's slot.
	unitCellW = 123.75
	unitCellH = 66.0
	// unitTop is how far below the top of the screen the portraits start.
	unitTop = 6.0
	// unitLeftEdge and unitRightEdge are where each run begins, measured from the middle of
	// the screen, where the clock is.
	unitLeftEdge  = -752.25
	unitRightEdge = 134.25
)

// Predict is where the portraits probably are on a screen of this size.
func Predict(screen image.Rectangle) Bar {
	if screen.Dy() <= 0 {
		return Bar{}
	}
	scale := float64(screen.Dy()) / unitHeight
	at := func(units float64) int { return int(units*scale + 0.5) }
	middle := (screen.Min.X + screen.Max.X) / 2
	w, h, y := at(unitCellW*Slots), at(unitCellH), at(unitTop)
	return Bar{
		Left:  Box{X: middle + at(unitLeftEdge), Y: y, W: w, H: h},
		Right: Box{X: middle + at(unitRightEdge), Y: y, W: w, H: h},
	}
}
