package screen

import "image"

// Agree is how many times a hero must be read in the same slot before it is believed. One
// confident reading is not enough: a frame of something that isn't Dota at all can produce
// one, and a hero named wrongly is worse than a hero not named.
const Agree = 2

// Reading is what has been made out of the bar so far, gathered over however many frames the
// draft lasts. A settled slot can still change: allies' hovers look like picks.
type Reading struct {
	votes  [2 * Slots]map[int]int
	heroes [2 * Slots]int
	// rival is a different hero read in a settled slot, and how many frames running.
	rival  [2 * Slots]int
	rivals [2 * Slots]int
}

// Add reads the bar in one frame. It returns how many slots it made out, and how many of
// them that settled: a slot can be read for several frames before it is believed.
//
// A frame only counts when it shows several portraits at once. One rectangle of a screen can
// resemble a hero by chance -- a menu, a loading screen, the desktop -- but a whole row of
// them resembling heroes in the places the bar's portraits go is the bar.
func (r *Reading) Add(shot image.Image, bar Bar, t Table) (read, settled int) {
	if !bar.Ready() || len(t) == 0 {
		return 0, 0
	}
	var seen [2 * Slots]int
	found := 0
	for slot := range seen {
		cell := bar.Cell(slot)
		if cell.In(shot.Bounds()) {
			if id, ok := t.MatchAt(shot, cell); ok {
				seen[slot] = id
			}
		}
		// A slot settled earlier is still a portrait in this frame, read or not.
		if seen[slot] != 0 || r.heroes[slot] != 0 {
			found++
		}
	}
	if found < LeastRead {
		return 0, 0
	}
	// Dota lets nobody take a hero someone else has, so the same hero in two slots is a
	// mistake in at least one of them, and there is no telling which.
	twice := map[int]bool{}
	for slot, id := range seen {
		if id == 0 {
			continue
		}
		for other := slot + 1; other < len(seen); other++ {
			if seen[other] == id {
				twice[id] = true
			}
		}
		if r.taken(id, slot) {
			twice[id] = true
		}
	}
	for slot, id := range seen {
		if id != 0 && id == r.heroes[slot] {
			r.rivals[slot] = 0
		}
		if id == 0 || twice[id] || id == r.heroes[slot] {
			continue
		}
		read++
		if r.heroes[slot] != 0 {
			if r.rival[slot] != id {
				r.rival[slot], r.rivals[slot] = id, 0
			}
			if r.rivals[slot]++; r.rivals[slot] >= Agree {
				r.heroes[slot], r.rivals[slot], settled = id, 0, settled+1
			}
			continue
		}
		if r.votes[slot] == nil {
			r.votes[slot] = map[int]int{}
		}
		r.votes[slot][id]++
		if r.votes[slot][id] >= Agree {
			r.heroes[slot], settled = id, settled+1
		}
	}
	return read, settled
}

// taken reports whether a hero has already settled in some other slot.
func (r *Reading) taken(heroID, notSlot int) bool {
	for slot, id := range r.heroes {
		if id == heroID && slot != notSlot {
			return true
		}
	}
	return false
}

// Heroes is what has settled: radiant 0 to 4 then dire 5 to 9, 0 where nothing has.
func (r *Reading) Heroes() [2 * Slots]int { return r.heroes }

// Settled is how many of the ten are known.
func (r *Reading) Settled() int { return ReadCount(r.heroes) }

// Ours and Theirs split the ten by side, leaving out what hasn't settled. Which run is which
// the caller knows from the game state, not from the screen.
func (r *Reading) Ours(dire bool) []int { return r.side(dire) }

func (r *Reading) Theirs(dire bool) []int { return r.side(!dire) }

func (r *Reading) side(dire bool) []int {
	from := 0
	if dire {
		from = Slots
	}
	var out []int
	for _, id := range r.heroes[from : from+Slots] {
		if id != 0 {
			out = append(out, id)
		}
	}
	return out
}

// Forget starts again, for the next draft.
func (r *Reading) Forget() { *r = Reading{} }
