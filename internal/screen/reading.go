package screen

import "image"

// Agree is how many times a hero must be read in the same slot before it is believed. One
// confident reading is not enough: a frame of something that isn't Dota at all can produce
// one, and a hero named wrongly is worse than a hero not named.
const Agree = 2

// Reading is what has been made out of the bar so far, gathered over however many frames the
// draft lasts. A slot's hero doesn't change once it is picked, so a settled slot is left
// alone and the reading only ever fills in.
type Reading struct {
	votes  [2 * Slots]map[int]int
	heroes [2 * Slots]int
}

// Add reads the bar in one frame and returns how many slots that settled.
func (r *Reading) Add(shot image.Image, bar Bar, t Table) int {
	if !bar.Ready() || len(t) == 0 {
		return 0
	}
	settled := 0
	for slot := range r.heroes {
		if r.heroes[slot] != 0 {
			continue
		}
		cell := bar.Cell(slot)
		if !cell.In(shot.Bounds()) {
			continue
		}
		id, ok := t.Match(Of(shot, cell))
		if !ok {
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
	return settled
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
