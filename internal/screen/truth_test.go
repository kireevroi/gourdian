package screen

import (
	"encoding/json"
	"fmt"
	"image"
	"os"
	"sort"
	"strings"
	"testing"
)

// truth is what the game's own log recorded as picked in the captured frames: radiant 0 to 4
// then dire 5 to 9.
var truth = games["1"]

// games are the drafts captured so far, each taken from the game's own log rather than read
// off the picture, so the reader can be measured against something certain. TRUTH picks one.
var games = map[string][]string{
	"1": {
		"npc_dota_hero_tinker", "npc_dota_hero_tidehunter", "npc_dota_hero_lion",
		"npc_dota_hero_juggernaut", "npc_dota_hero_vengefulspirit",
		"npc_dota_hero_dragon_knight", "npc_dota_hero_necrolyte", "npc_dota_hero_lich",
		"npc_dota_hero_witch_doctor", "npc_dota_hero_sven",
	},
	"2": {
		"npc_dota_hero_clinkz", "npc_dota_hero_lich", "npc_dota_hero_witch_doctor",
		"npc_dota_hero_viper", "npc_dota_hero_earthshaker",
		"npc_dota_hero_skeleton_king", "npc_dota_hero_zuus", "npc_dota_hero_drow_ranger",
		"npc_dota_hero_lion", "npc_dota_hero_warlock",
	},
}

func init() {
	if g := os.Getenv("TRUTH"); g != "" {
		truth = games[g]
	}
}

// namedTable builds the table the way the trainer does: only heroes OpenDota knows about, so
// the creeps and placeholders that share the hero art folder are left out and can't steal a
// match's margin.
func namedTable(t *testing.T) (Table, map[int]string) {
	raw, err := os.ReadFile(os.Getenv("HEROES"))
	if err != nil {
		t.Skipf("set HEROES to the trainer's cached heroes.json: %v", err)
	}
	var list map[string]struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	names, ids := map[int]string{}, map[string]int{}
	for _, h := range list {
		ids[h.Name], names[h.ID] = h.ID, h.Name
	}
	return TableFor(ids), names
}

// measuredBar is the geometry of a real 2560x1440 frame, fitted to it rather than eyeballed:
// the portraits start below the bar of the player's colour, and the two runs are the same
// size and pitch as each other.
var measuredBar = Bar{
	Left:  Box{X: 277, Y: 8, W: 825, H: 88},
	Right: Box{X: 1459, Y: 8, W: 825, H: 88},
}

func TestAgainstARealFrame(t *testing.T) {
	src := os.Getenv("FRAME")
	if src == "" {
		t.Skip("set FRAME to a captured screen")
	}
	img := openFrame(t, src)
	measuredBar = Predict(img.Bounds())
	table, names := namedTable(t)
	right, unknown, wrong := 0, 0, 0
	for slot, want := range truth {
		cell := measuredBar.Cell(slot)
		s := Of(img, cell)
		type scored struct {
			hero string
			dist int
		}
		var all []scored
		for id, arts := range table {
			near := -1
			for _, a := range arts {
				if d := s.Distance(a); near < 0 || d < near {
					near = d
				}
			}
			all = append(all, scored{names[id], near})
		}
		sort.Slice(all, func(a, b int) bool { return all[a].dist < all[b].dist })
		ratio := float64(all[0].dist) / float64(all[1].dist)
		id, ok := table.Match(s)
		switch {
		case ok && names[id] == want:
			right++
		case ok:
			wrong++
		default:
			unknown++
		}
		mark := " "
		if all[0].hero != want {
			mark = "!"
		}
		fmt.Printf("%s slot %d want %-30s nearest %-30s best %7d next %7d ratio %.2f\n",
			mark, slot, want, all[0].hero, all[0].dist, all[1].dist, ratio)
	}
	// And some places on the screen that hold no portrait at all.
	for _, r := range []image.Rectangle{
		image.Rect(1150, 4, 1315, 100), image.Rect(600, 700, 765, 796), image.Rect(60, 4, 225, 100),
	} {
		s := Of(img, r)
		best, bestName := -1, ""
		for id, arts := range table {
			for _, a := range arts {
				if d := s.Distance(a); best < 0 || d < best {
					best, bestName = d, names[id]
				}
			}
		}
		_, taken := table.Match(s)
		fmt.Printf("  empty %v nearest %-30s %7d taken=%v\n", r.Min, bestName, best, taken)
	}
	fmt.Printf("\nright %d, unknown %d, WRONG %d\n", right, unknown, wrong)
	if wrong > 0 {
		t.Errorf("%d slots read as the wrong hero", wrong)
	}
}

var _ = image.Rect

// TestAcrossEveryFrame is how the trainer really works: it reads the same slot many times
// over a draft. The first captured frame is not Dota at all -- it caught the desktop before
// the player switched across -- which is exactly why one confident answer is not enough: a
// hero has to be read the same way twice before it is believed.
func TestAcrossEveryFrame(t *testing.T) {
	files := loadFrames(t)
	table, names := namedTable(t)
	const agree = 2
	votes := make([]map[string]int, len(truth))
	for i := range votes {
		votes[i] = map[string]int{}
	}
	locked := make([]string, len(truth))
	for _, path := range files {
		img := openFrame(t, path)
		for slot := range truth {
			if locked[slot] != "" {
				continue
			}
			id, ok := table.Match(Of(img, measuredBar.Cell(slot)))
			if !ok {
				continue
			}
			votes[slot][names[id]]++
			if votes[slot][names[id]] >= agree {
				locked[slot] = names[id]
			}
		}
	}
	read, wrong := 0, 0
	for slot, want := range truth {
		switch locked[slot] {
		case want:
			read++
		case "":
			fmt.Printf("  slot %d never settled (%s) votes %v\n", slot, short(want), shortVotes(votes[slot]))
		default:
			wrong++
			fmt.Printf("  slot %d SETTLED ON %s, is %s\n", slot, short(locked[slot]), short(want))
		}
	}
	fmt.Printf("\nover %d frames, agreeing %d times: read %d/10, WRONG %d\n", len(files), agree, read, wrong)
	if wrong > 0 {
		t.Errorf("%d slots read as the wrong hero", wrong)
	}
}

func short(hero string) string { return strings.TrimPrefix(hero, "npc_dota_hero_") }

func shortVotes(v map[string]int) map[string]int {
	out := map[string]int{}
	for hero, n := range v {
		out[short(hero)] = n
	}
	return out
}

// TestReadingOverTheFrames runs the real accumulating reader over the captured frames, the
// way the trainer will during a draft, including the first frame which is not Dota at all.
func TestReadingOverTheFrames(t *testing.T) {
	files := loadFrames(t)
	table, names := namedTable(t)
	var seen Reading
	first := 0
	for i, path := range files {
		img := openFrame(t, path)
		bar := Predict(img.Bounds())
		seen.Add(img, bar, table)
		if seen.Settled() == 2*Slots && first == 0 {
			first = i + 1
		}
	}
	got := seen.Heroes()
	wrong := 0
	for slot, want := range truth {
		switch {
		case got[slot] == 0:
			t.Errorf("slot %d never settled (%s)", slot, short(want))
		case names[got[slot]] != want:
			wrong++
			t.Errorf("slot %d settled on %s, is %s", slot, short(names[got[slot]]), short(want))
		}
	}
	fmt.Printf("all ten settled after %d frames, %d wrong\n", first, wrong)
	// The enemy five are what the pick advice is for; the player was radiant here.
	fmt.Printf("theirs:")
	for _, id := range seen.Theirs(false) {
		fmt.Printf(" %s", short(names[id]))
	}
	fmt.Println()
}
