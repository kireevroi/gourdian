package dotadata

import "testing"

func TestGoodTiming(t *testing.T) {
	// OpenDota's Anti-Mage Battle Fury buckets: 12:00 holds 12% of games at a 71% win rate.
	am := []ItemTiming{{450, 13, 8}, {600, 19, 8}, {720, 63, 45}, {900, 263, 154}, {1200, 167, 71}, {1500, 10, 3}, {1800, 1, 0}}
	if got, ok := GoodTiming(am); !ok || got != 720 {
		t.Fatalf("good timing = %d, %v", got, ok)
	}
	rising := []ItemTiming{{600, 40, 10}, {900, 40, 20}, {1200, 40, 30}}
	if got, _ := GoodTiming(rising); got != 900 {
		t.Fatalf("9:00 is the first bucket at the overall 50%% win rate: %d", got)
	}
	if _, ok := GoodTiming([]ItemTiming{{600, 5, 3}}); ok {
		t.Fatal("too few games to trust")
	}
}

func TestCoreItemTimes(t *testing.T) {
	items := map[string]ItemInfo{
		"sange": {Cost: 2050}, "yasha": {Cost: 2050},
		"sange_and_yasha": {Cost: 4100, Components: []string{"sange", "yasha"}},
		"magic_wand":      {Cost: 450},
	}
	got := CoreItemTimes(map[string]int{"sange": 900, "yasha": 1000, "sange_and_yasha": 1010, "magic_wand": 200}, items, 1500)
	if len(got) != 1 || got["sange_and_yasha"] != 1010 {
		t.Fatalf("core items = %v", got)
	}
}
