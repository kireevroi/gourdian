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
	"3": {
		"npc_dota_hero_windrunner", "npc_dota_hero_earthshaker", "npc_dota_hero_necrolyte",
		"npc_dota_hero_drow_ranger", "npc_dota_hero_warlock",
		"npc_dota_hero_lion", "npc_dota_hero_death_prophet", "npc_dota_hero_skeleton_king",
		"npc_dota_hero_lich", "npc_dota_hero_juggernaut",
	},
	// Ranked, match 9016917266: every portrait carries a rank banner and flag.
	"4": {
		"npc_dota_hero_medusa", "npc_dota_hero_silencer", "npc_dota_hero_viper",
		"npc_dota_hero_rubick", "npc_dota_hero_dawnbreaker",
		"npc_dota_hero_undying", "npc_dota_hero_shadow_shaman", "npc_dota_hero_riki",
		"npc_dota_hero_dragon_knight", "npc_dota_hero_skeleton_king",
	},
	// Ranked, match 9017405558, from the start: empty slots with rank flags and teammates'
	// greyed-out hovers, the two that were read as Faceless Void.
	"5": {
		"npc_dota_hero_undying", "npc_dota_hero_shredder", "npc_dota_hero_faceless_void",
		"npc_dota_hero_queenofpain", "npc_dota_hero_skywrath_mage",
		"npc_dota_hero_razor", "npc_dota_hero_troll_warlord", "npc_dota_hero_venomancer",
		"npc_dota_hero_sniper", "npc_dota_hero_furion",
	},
}

func init() {
	if g := os.Getenv("TRUTH"); g != "" {
		truth = games[g]
	}
}

// namedTable keeps only heroes OpenDota knows, as the trainer does, so creeps and placeholders
// in the hero art folder can't steal a match's margin.
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

// measuredBar is the geometry fitted to a real 2560x1440 frame.
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
	measuredBar = barOf(t, src, img)
	table, names := namedTable(t)
	right, unknown, wrong := 0, 0, 0
	for slot, want := range truth {
		cell := measuredBar.Cell(slot)
		raw := Of(img, cell)
		s := raw.Level()
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
		id, ok := table.Match(raw)
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
		s := Of(img, r).Level()
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

// TestAcrossEveryFrame reads each slot over the whole draft and settles on two agreeing reads.
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
			id, ok := table.Match(Of(img, barOf(t, path, img).Cell(slot)))
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
		seen.Add(img, barOf(t, path, img), table)
		if seen.Settled() == 2*Slots && first == 0 {
			first = i + 1
		}
	}
	got := seen.Heroes()
	wrong := 0
	for slot, want := range truth {
		switch {
		case got[slot] == 0:
			t.Logf("slot %d never settled (%s)", slot, short(want))
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
