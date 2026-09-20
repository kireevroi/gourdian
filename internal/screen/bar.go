package screen

import "image"

// Slots is how many heroes a team has in the bar.
const Slots = 5

// Box is a rectangle in screen pixels, spelled out so it reads plainly in the settings file
// and can be dragged out on the dashboard.
type Box struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

func (b Box) empty() bool { return b.W <= 0 || b.H <= 0 }

// Bar is where the two runs of five hero portraits sit on the player's screen: their own team
// on one side of the clock and the enemy on the other. Which side is which is not the bar's
// business; the trainer knows its own team from the game state.
type Bar struct {
	Left  Box `json:"left"`
	Right Box `json:"right"`
}

// Ready reports whether the bar describes ten portraits big enough to tell apart. A portrait
// smaller than the hash it is shrunk to carries no information.
func (b Bar) Ready() bool {
	for _, side := range []Box{b.Left, b.Right} {
		if side.empty() || side.W < Slots*sigW || side.H < sigH {
			return false
		}
	}
	return true
}

// Cell is where one slot sits: 0 to 4 along the left run, then 5 to 9 along the right.
func (b Bar) Cell(i int) image.Rectangle {
	side, n := b.Left, i
	if i >= Slots {
		side, n = b.Right, i-Slots
	}
	w := side.W / Slots
	// The last cell takes the rounding, so the run always ends where it was said to.
	x := side.X + n*w
	if n == Slots-1 {
		return image.Rect(x, side.Y, side.X+side.W, side.Y+side.H)
	}
	return image.Rect(x, side.Y, x+w, side.Y+side.H)
}

// ReadRun is the hero in each of one run's five slots, 0 where a slot can't be read.
func (b Box) ReadRun(shot image.Image, t Table) [Slots]int {
	var out [Slots]int
	if b.empty() || b.W < Slots*sigW || b.H < sigH {
		return out
	}
	w := b.W / Slots
	for i := range out {
		cell := image.Rect(b.X+i*w, b.Y, b.X+i*w+w, b.Y+b.H)
		if i == Slots-1 {
			cell.Max.X = b.X + b.W
		}
		if !cell.In(shot.Bounds()) {
			continue
		}
		if id, ok := t.Match(Of(shot, cell)); ok {
			out[i] = id
		}
	}
	return out
}

func runCount(slots [Slots]int) int {
	n := 0
	for _, id := range slots {
		if id != 0 {
			n++
		}
	}
	return n
}

// Read is the hero in each of the ten slots, 0 where a slot is empty or can't be read.
func (b Bar) Read(shot image.Image, t Table) [2 * Slots]int {
	var out [2 * Slots]int
	if !b.Ready() || len(t) == 0 {
		return out
	}
	for i := range out {
		cell := b.Cell(i)
		if !cell.In(shot.Bounds()) {
			continue
		}
		if id, ok := t.Match(Of(shot, cell)); ok {
			out[i] = id
		}
	}
	return out
}

// Read counts how many of the ten slots were read, which is how a calibration is judged.
func ReadCount(slots [2 * Slots]int) int {
	n := 0
	for _, id := range slots {
		if id != 0 {
			n++
		}
	}
	return n
}
