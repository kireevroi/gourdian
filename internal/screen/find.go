package screen

import "image"

// Finding the bar is a search, so it is bounded: the portraits sit along the very top of the
// screen, and are drawn at a size that follows the screen's own.
const (
	// TopFraction is how much of the screen's height the bar can be within.
	TopFraction = 0.10
	// MinCellH and MaxCellH are how tall one portrait can be, as a fraction of the screen.
	MinCellH = 0.020
	MaxCellH = 0.070
	// AspectMin and AspectMax are how wide a portrait is against its height.
	AspectMin = 1.0
	AspectMax = 2.2
)

// Found is where one portrait sits and how far off the match was.
type Found struct {
	Cell     image.Rectangle
	Distance int
}

// Find looks for one portrait it already knows in a picture of the screen. It is how the
// trainer works out where the bar is without being told: in a match it knows which hero is
// its own, so it has one certain thing to look for among the ten.
//
// The search is coarse and then fine, since a portrait is dozens of pixels across and a
// near-miss still scores well.
func Find(shot image.Image, want Signature) (Found, bool) {
	b := shot.Bounds()
	if b.Dx() < 40 || b.Dy() < 40 {
		return Found{}, false
	}
	top := b.Min.Y + int(float64(b.Dy())*TopFraction)
	minH, maxH := max(8, int(float64(b.Dy())*MinCellH)), int(float64(b.Dy())*MaxCellH)

	best := Found{Distance: -1}
	// Coarse: every fourth height, every eighth pixel across.
	for h := minH; h <= maxH; h += max(1, (maxH-minH)/8) {
		for _, aspect := range []float64{AspectMin, 1.3, 1.6, AspectMax} {
			w := int(float64(h) * aspect)
			if w < 8 {
				continue
			}
			step := max(4, w/4)
			for y := b.Min.Y; y+h <= top; y += max(2, h/4) {
				for x := b.Min.X; x+w <= b.Max.X; x += step {
					probe(shot, image.Rect(x, y, x+w, y+h), want, &best)
				}
			}
		}
	}
	if best.Distance < 0 {
		return Found{}, false
	}
	// Fine: walk around the best guess a pixel and a couple of sizes at a time.
	for range 3 {
		start := best
		for dy := -3; dy <= 3; dy++ {
			for dx := -3; dx <= 3; dx++ {
				for dh := -2; dh <= 2; dh++ {
					for dw := -3; dw <= 3; dw++ {
						r := image.Rect(start.Cell.Min.X+dx, start.Cell.Min.Y+dy,
							start.Cell.Max.X+dx+dw, start.Cell.Max.Y+dy+dh)
						if r.Dx() < 8 || r.Dy() < 8 || !r.In(b) {
							continue
						}
						probe(shot, r, want, &best)
					}
				}
			}
		}
		if best.Cell == start.Cell {
			break
		}
	}
	return best, true
}

func probe(shot image.Image, r image.Rectangle, want Signature, best *Found) {
	if d := Of(shot, r).Distance(want); best.Distance < 0 || d < best.Distance {
		*best = Found{Cell: r, Distance: d}
	}
}

// LeastRead is how many of the ten portraits a bar has to read before it is believed to be
// the bar. Two is enough: nothing has ever been read as the wrong hero, so two agreeing
// answers in the right places are not a coincidence.
const LeastRead = 2

// Fit turns the rough position of one portrait into the bar itself.
//
// What Find lands on is the art, not the slot it sits in: Dota frames each portrait and
// paints a health bar across it, so the rectangle that matches best is a little inside the
// one the portraits are spaced by. Rather than reason about the frame, this tries the
// geometries around it and keeps whichever reads the most heroes, which is the thing that
// actually matters. The other team's run is then searched for separately instead of being
// assumed to mirror this one, since where the clock sits between them is Dota's business.
func Fit(shot image.Image, t Table, around image.Rectangle, slot int) (Bar, int, bool) {
	n := slot
	if slot >= Slots {
		n = slot - Slots
	}
	mine, mineRead := fitRun(shot, t, around, n)
	if mineRead == 0 {
		return Bar{}, 0, false
	}
	theirs, theirsRead := findRun(shot, t, mine, slot < Slots)

	bar := Bar{Left: mine, Right: theirs}
	if slot >= Slots {
		bar = Bar{Left: theirs, Right: mine}
	}
	read := mineRead + theirsRead
	if read < LeastRead {
		return Bar{}, read, false
	}
	return bar, read, true
}

// fitRun settles the run holding the portrait that was found: its height, the width of a
// slot, and where the run starts.
func fitRun(shot image.Image, t Table, around image.Rectangle, n int) (Box, int) {
	var best Box
	bestRead := 0
	for dh := 0; dh <= 10; dh++ {
		h := around.Dy() + dh
		for dw := 0; dw <= 16; dw++ {
			w := around.Dx() + dw
			// Keep the portrait's middle where it was found, so widening looks both ways.
			x, y := around.Min.X-dw/2, around.Min.Y-dh/2
			for ox := -3; ox <= 3; ox++ {
				for oy := -3; oy <= 3; oy++ {
					run := Box{X: x + ox - n*w, Y: y + oy, W: Slots * w, H: h}
					if run.X < shot.Bounds().Min.X || run.X+run.W > shot.Bounds().Max.X {
						continue
					}
					if read := runCount(run.ReadRun(shot, t)); read > bestRead {
						best, bestRead = run, read
					}
				}
			}
		}
	}
	return best, bestRead
}

// findRun looks for the other team's five, which sit at the same height and size somewhere on
// the far side of the clock.
func findRun(shot image.Image, t Table, like Box, toTheRight bool) (Box, int) {
	b := shot.Bounds()
	from, to := b.Min.X, like.X-like.W/Slots
	if toTheRight {
		from, to = like.X+like.W, b.Max.X-like.W
	}
	var best Box
	bestRead := 0
	step := max(2, like.W/Slots/8)
	for x := from; x <= to; x += step {
		run := Box{X: x, Y: like.Y, W: like.W, H: like.H}
		if read := runCount(run.ReadRun(shot, t)); read > bestRead {
			best, bestRead = run, read
		}
	}
	if bestRead == 0 {
		return Box{}, 0
	}
	// Walk the best position a pixel at a time now that the neighbourhood is known.
	for x := best.X - step; x <= best.X+step; x++ {
		run := Box{X: x, Y: like.Y, W: like.W, H: like.H}
		if read := runCount(run.ReadRun(shot, t)); read > bestRead {
			best, bestRead = run, read
		}
	}
	return best, bestRead
}
