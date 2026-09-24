package server

import (
	"testing"
	"time"

	"gourdian/internal/game/model"
)

// A match recorded when Dota went quiet is replaced by its real end, keeping what was added since.
func TestResumedMatchReplacesItsRecord(t *testing.T) {
	srv, _, _ := newTestServer(t, nil)
	early := model.MatchSummary{MatchID: "local-test", Source: model.SourcePractice, HeroID: 1, Hero: "Anti-Mage",
		Role: "carry", Result: "unknown", EndedAt: time.Now(), DurationSec: 600, Kills: 1}
	srv.recordMatch(&early, srv.cfg.Settings())
	if err := srv.stats.UpdateMatch("local-test", func(m *model.MatchSummary) { m.Ranked = true }); err != nil {
		t.Fatal(err)
	}
	final := early
	final.Result, final.DurationSec, final.Kills, final.Resumed = "win", 1800, 9, true
	srv.recordMatch(&final, srv.cfg.Settings())
	got, err := srv.stats.Match("local-test")
	if err != nil {
		t.Fatal(err)
	}
	if got.Result != "win" || got.DurationSec != 1800 || got.Kills != 9 || !got.Ranked {
		t.Fatalf("recorded %+v", got)
	}
}
