package screen

import (
	"fmt"
	"image"
	"testing"
)

// matchAt is Table.Match with the confidence line moved, for measuring where it should be.
func matchAt(t Table, s Signature, cut float64) (int, bool) {
	best, second, heroID := -1, -1, 0
	for id, arts := range t {
		near := -1
		for _, a := range arts {
			if d := s.Distance(a); near < 0 || d < near {
				near = d
			}
		}
		switch {
		case near < 0:
		case best < 0 || near < best:
			best, second, heroID = near, best, id
		case second < 0 || near < second:
			second = near
		}
	}
	if best < 0 || second < 0 || float64(best) > cut*float64(second) {
		return 0, false
	}
	return heroID, true
}

// TestWhereTheLineGoes reads a whole draft at several confidence lines, counting what settles
// and what settles wrongly. The frames before the draft, which are the menu and the desktop,
// are left in: they are where a wrong answer would come from.
func TestWhereTheLineGoes(t *testing.T) {
	files := loadFrames(t)
	table, names := namedTable(t)
	for _, cut := range []float64{0.70, 0.80, 0.85, 0.90} {
		for _, agree := range []int{2, 3} {
			votes := make([]map[int]int, 2*Slots)
			for i := range votes {
				votes[i] = map[int]int{}
			}
			settled := make([]int, 2*Slots)
			for _, path := range files {
				img := openFrame(t, path)
				bar := Predict(img.Bounds())
				for slot := range settled {
					if settled[slot] != 0 {
						continue
					}
					cell := bar.Cell(slot)
					if !cell.In(img.Bounds()) {
						continue
					}
					if id, ok := matchAt(table, Of(img, cell).Level(), cut); ok {
						votes[slot][id]++
						if votes[slot][id] >= agree {
							settled[slot] = id
						}
					}
				}
			}
			right, wrong, missing := 0, 0, 0
			var bad []string
			for slot, want := range truth {
				switch {
				case settled[slot] == 0:
					missing++
				case names[settled[slot]] == want:
					right++
				default:
					wrong++
					bad = append(bad, fmt.Sprintf("%s->%s", short(want), short(names[settled[slot]])))
				}
			}
			fmt.Printf("cut %.2f agree %d: right %2d, missing %d, WRONG %d %v\n", cut, agree, right, missing, wrong, bad)
		}
	}
}

var _ = image.Rect
