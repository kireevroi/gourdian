package opendota

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

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

// Background fetches end with the context the client was started with, so quitting the app
// doesn't wait on OpenDota.
func TestFetchesStopWithTheStartContext(t *testing.T) {
	asked := make(chan struct{}, 1)
	gone := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "constants") {
			w.Write([]byte("{}"))
			return
		}
		asked <- struct{}{}
		<-r.Context().Done() // OpenDota taking its time
		close(gone)
	}))
	defer srv.Close()
	c := New(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(c.Wait)
	c.SetBaseURL(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	c.Start(ctx)
	c.ItemTimings(1, "blink")
	<-asked
	cancel()
	select {
	case <-gone:
	case <-time.After(5 * time.Second):
		t.Fatal("the fetch kept going after the app's context ended")
	}
}

// An item carried for minutes before it became something bigger is a timing of its own, so the
// trainer can hold a player to their usual Dragon Lance and not only their usual Pike.
func TestCoreItemTimesKeepsAnItemCarriedBeforeItsUpgrade(t *testing.T) {
	items := map[string]ItemInfo{
		"force_staff":    {Cost: 2200, Qual: "rare", Created: true},
		"dragon_lance":   {Cost: 1900, Qual: "artifact", Created: true},
		"mithril_hammer": {Cost: 1600, Qual: "component"},
		"hurricane_pike": {Cost: 4450, Qual: "epic", Created: true, Components: []string{"force_staff", "dragon_lance", "mithril_hammer"}},
	}
	got := CoreItemTimes(map[string]int{
		"dragon_lance": 860, "mithril_hammer": 1200, "force_staff": 1220, "hurricane_pike": 1260,
	}, items, 1500)
	want := map[string]int{"dragon_lance": 860, "hurricane_pike": 1260}
	if len(got) != len(want) {
		t.Fatalf("core items = %v, want %v", got, want)
	}
	for n, at := range want {
		if got[n] != at {
			t.Fatalf("core items = %v, want %v", got, want)
		}
	}
}
