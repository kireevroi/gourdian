package screen

import (
	"fmt"
	"testing"
)

// TestEmptySlots checks the state every draft starts in: ten slots with nobody picked. They
// must read as nobody, or the trainer would open every game by naming ten heroes at random.
func TestEmptySlots(t *testing.T) {
	files := loadFrames(t)
	table, names := namedTable(t)
	img := openFrame(t, files[0])
	bar := Predict(img.Bounds())
	fmt.Println("at the geometry worked out from the screen's height:")
	read := 0
	for slot := range 2 * Slots {
		s := Of(img, bar.Cell(slot))
		best, second, hero := -1, -1, ""
		for id, arts := range table {
			near := -1
			for _, a := range arts {
				if d := s.Distance(a); near < 0 || d < near {
					near = d
				}
			}
			switch {
			case best < 0 || near < best:
				best, second, hero = near, best, names[id]
			case second < 0 || near < second:
				second = near
			}
		}
		_, taken := table.Match(s)
		if taken {
			read++
		}
		fmt.Printf("  slot %d nearest %-28s %8d ratio %.2f taken=%v\n", slot, short(hero), best, float64(best)/float64(second), taken)
	}
	fmt.Printf("read %d of ten empty slots as heroes\n", read)
	if found, n, ok := Locate(img, table); ok {
		fmt.Printf("and the search found a 'bar' reading %d at %+v\n", n, found.Left)
	}
	if read > 0 {
		t.Errorf("%d empty slots were read as heroes", read)
	}
}
