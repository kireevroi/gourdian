package coach

import (
	"testing"

	"gourdian/internal/dota"
	"gourdian/internal/dotadata"
	"gourdian/internal/gsi"
)

func TestNoSpendGoldAlertWhileSavingForTheNextItem(t *testing.T) {
	data := fakeData{
		items: map[string]dotadata.ItemInfo{"black_king_bar": {DName: "Black King Bar", Cost: 4050}},
		build: &dotadata.Build{Items: []dotadata.BuildItem{{Name: "black_king_bar", DName: "Black King Bar", Cost: 4050, Phase: dotadata.PhaseMid}}},
	}
	holding := func(gold int) func(*gsi.State) {
		return func(s *gsi.State) { s.Player.Gold, s.Player.GPM = gold, 500 }
	}
	if got := byRule(play(newEngine(data), settings(dota.Carry), 900, 1000, holding(2767)), "unspent_gold"); len(got) != 0 {
		t.Fatalf("saving for Black King Bar isn't unspent gold: %+v", got)
	}
	if got := byRule(play(newEngine(data), settings(dota.Carry), 900, 1000, holding(6000)), "unspent_gold"); len(got) != 1 {
		t.Fatalf("6000 gold is Black King Bar and plenty more: want one alert, got %+v", got)
	}
}
