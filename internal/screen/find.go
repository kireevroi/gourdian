package screen

import "image"

// LeastRead is how many of the ten portraits have to be made out before a stretch of screen
// is believed to be the bar. A single rectangle can resemble a hero by chance -- menus,
// loading screens and the desktop are full of hero-shaped noise -- but several of them
// resembling heroes at exactly the spacing the bar uses is the bar.
const LeastRead = 2

// LocateLeast is how many heroes a stretch of screen has to yield before it is taken to be
// the bar. It is much higher than LeastRead because the screen a draft happens on is covered
// in hero portraits -- the grid you pick from is nothing else -- so a few of them falling
// where the bar's portraits would is no surprise at all. Locating can afford to wait: it only
// has to happen once, and by the time most of the draft is picked there is no doubt left.
const LocateLeast = 6

// Locate finds the bar when the guess from the screen's height doesn't read anything, which
// is what a hand-scaled interface looks like.
//
// It searches over the bar itself rather than over portraits. Hunting for a portrait anywhere
// on the screen sounds more general and is much worse: at any one place the question is only
// "which hero is this most like", and a dark patch of menu is quite like a dark hero, so the
// answer is pages of heroes that aren't there. Asking instead which whole bar reads the most
// heroes can only be answered by something shaped like the bar.
func Locate(shot image.Image, t Table) (Bar, int, bool) {
	if len(t) == 0 {
		return Bar{}, 0, false
	}
	guess := Predict(shot.Bounds())
	if !guess.Ready() {
		return Bar{}, 0, false
	}
	var best Bar
	bestRead := 0
	middle := (shot.Bounds().Min.X + shot.Bounds().Max.X) / 2
	// Dota's interface can be scaled by hand, and a window may not start at the top of the
	// screen, so the guess is stretched about the middle and slid up and down.
	for scale := 0.60; scale <= 1.50; scale += 0.02 {
		at := func(v int) int { return int(float64(v)*scale + 0.5) }
		w, h := at(guess.Left.W), at(guess.Left.H)
		left := middle + at(guess.Left.X-middle)
		right := middle + at(guess.Right.X-middle)
		for dy := -12; dy <= 24; dy += 2 {
			y := at(guess.Left.Y) + dy
			if y < shot.Bounds().Min.Y {
				continue
			}
			bar := Bar{Left: Box{X: left, Y: y, W: w, H: h}, Right: Box{X: right, Y: y, W: w, H: h}}
			if !bar.Ready() {
				continue
			}
			if read := distinct(bar.Read(shot, t)); read > bestRead {
				best, bestRead = bar, read
			}
		}
	}
	if bestRead < LocateLeast {
		return Bar{}, bestRead, false
	}
	return best, bestRead, true
}

// distinct counts the heroes read, ignoring any read into more than one slot: Dota lets
// nobody take a hero someone else has, so a repeat is a mistake rather than a sighting.
func distinct(slots [2 * Slots]int) int {
	seen, n := map[int]int{}, 0
	for _, id := range slots {
		if id != 0 {
			seen[id]++
		}
	}
	for _, times := range seen {
		if times == 1 {
			n++
		}
	}
	return n
}
