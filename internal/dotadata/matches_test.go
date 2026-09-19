package dotadata

import (
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sync/atomic"
	"testing"
)

func loadMatch(t *testing.T, name string) *Match {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var m Match
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return &m
}

func TestExtractParsedMatch(t *testing.T) {
	m := loadMatch(t, "match_parsed.json")
	if !m.Parsed() {
		t.Fatal("fixture should be parsed")
	}
	d, ok := Extract(m, 100000001, 0)
	if !ok {
		t.Fatal("player not found by account id")
	}
	if d.HeroID != 44 || d.LaneRole != LaneSafe || d.Win || d.NetWorth != 4412 || d.ObsPlaced != 2 {
		t.Fatalf("unexpected detail: %+v", d)
	}
	if d.LastHitsAt[5] != 11 {
		t.Fatalf("last hits at 5:00 = %d, want 11 (from lh_t)", d.LastHitsAt[5])
	}
	if _, ok := d.LastHitsAt[10]; ok {
		t.Fatal("a 499-second match has no 10:00 sample")
	}
	if d.ItemTimes["magic_wand"] != 22 || len(d.DeathTimes) != 4 || d.DeathTimes[0] != 111 {
		t.Fatalf("items/deaths: %v %v", d.ItemTimes["magic_wand"], d.DeathTimes)
	}
	if math.Abs(d.Percentiles["gold_per_min"]-0.0604) > 0.001 {
		t.Fatalf("gpm percentile = %v", d.Percentiles["gold_per_min"])
	}
	if len(d.Allies) != 4 || len(d.Enemies) != 5 || !slices.Equal(d.LaneOpponents, []int{98, 15}) || d.NetWorthRank != 3 {
		t.Fatalf("team context: allies %v enemies %v lane %v nw rank %d", d.Allies, d.Enemies, d.LaneOpponents, d.NetWorthRank)
	}
}

func TestExtractFallsBackToHeroAndHandlesUnparsed(t *testing.T) {
	m := loadMatch(t, "match_basic.json")
	if m.Parsed() {
		t.Fatal("basic fixture should not be parsed")
	}
	d, ok := Extract(m, 0, 21)
	if !ok || d.HeroID != 21 || d.Parsed || len(d.LastHitsAt) != 0 || len(d.LaneOpponents) != 0 {
		t.Fatalf("unexpected detail: %+v, %v", d, ok)
	}
	if _, ok := Extract(m, 1, 999); ok {
		t.Fatal("unknown player should not match")
	}
}

func TestMatchCachesOnlyParsedMatches(t *testing.T) {
	parsed, _ := os.ReadFile("testdata/match_parsed.json")
	basic, _ := os.ReadFile("testdata/match_basic.json")
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/matches/1":
			w.Write(parsed)
		case "/matches/2":
			w.Write(basic)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := New(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.base = srv.URL

	for range 2 {
		if m, err := c.Match(t.Context(), "1"); err != nil || !m.Parsed() {
			t.Fatalf("match 1: %v", err)
		}
		if _, err := c.Match(t.Context(), "2"); err != nil {
			t.Fatalf("match 2: %v", err)
		}
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("server calls = %d, want 3 (parsed match cached, unparsed fetched twice)", got)
	}
	if _, err := c.Match(t.Context(), "sim-123"); err == nil {
		t.Fatal("simulated match ids must be rejected")
	}
}
