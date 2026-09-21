package screen

import "testing"

// numbered gives every hero Dota draws a number, the way the trainer does from OpenDota.
func numbered() (Table, map[int]string) {
	ids, names := map[string]int{}, map[int]string{}
	for name := range Known() {
		id := len(ids) + 1
		ids[name], names[id] = id, name
	}
	return TableFor(ids), names
}

// Dimming a portrait moves it nearer the game's darkest heroes than itself: measured over
// every portrait, half-strength art was read wrongly 38 times before brightness was taken out.
func TestADimmedPortraitIsNeverAnotherHero(t *testing.T) {
	tab, names := numbered()
	for _, light := range []float64{1, 0.8, 0.6, 0.5} {
		right, unread := 0, 0
		var wrong []string
		for name, sigs := range Known() {
			for _, s := range sigs {
				var dim Signature
				for i, v := range s {
					dim[i] = uint8(float64(v) * light)
				}
				switch got, ok := tab.Match(dim); {
				case !ok:
					unread++
				case names[got] == name:
					right++
				default:
					wrong = append(wrong, short(name)+" read as "+short(names[got]))
				}
			}
		}
		t.Logf("%3.0f%% brightness: %d right, %d wrong, %d unread", light*100, right, len(wrong), unread)
		if len(wrong) > 0 {
			t.Errorf("at %.0f%% brightness %d portraits were read as the wrong hero: %v",
				light*100, len(wrong), wrong[:min(len(wrong), 6)])
		}
	}
}
