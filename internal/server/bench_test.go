package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"gourdian/internal/coach"
	"gourdian/internal/config"
	"gourdian/internal/gsi"
	"gourdian/internal/sim"
	"gourdian/internal/stats"
)

// seed fills a store with matches, tips and samples, to measure the pages against a season
// of play rather than an empty file.
func seed(t testing.TB, srv *Server, matches int) {
	for i := range matches {
		id := fmt.Sprintf("m%04d", i)
		srv.stats.AppendMatch(stats.MatchSummary{
			MatchID: id, Hero: "Lion", HeroID: 26, Role: config.RoleHardSupport, Result: "win",
			Source: stats.SourceLive, EndedAt: time.Now().Add(-time.Duration(matches-i) * time.Hour),
			Kills: 5, Deaths: 6, Assists: 20, LastHits: 90, GPM: 400, XPM: 500,
			LastHitsAt: map[string]int{"10:00": 40}, TipCounts: map[string]int{"no_tp": 2},
		})
		tips := make([]stats.TipRecord, 12)
		for j := range tips {
			tips[j] = stats.TipRecord{At: time.Now(), MatchID: id, Clock: j * 60, Rule: "no_tp", Habit: true, Text: "No TP scroll"}
		}
		srv.stats.AppendTips(tips)
		samples := make([]stats.Sample, 60)
		for j := range samples {
			samples[j] = stats.Sample{MatchID: id, Clock: j * 30, Gold: 500, LastHits: j}
		}
		srv.stats.AppendSamples(samples)
	}
}

func benchServer(b *testing.B, matches int) (*Server, config.Settings) {
	srv, h, _ := newTestServer(b, nil)
	seed(b, srv, matches)
	set := srv.cfg.Settings()
	set.Drill = "no_tp"
	srv.cfg.UpdateSettings(set)
	postState(b, h, payload(300, func(s *gsi.State) { s.Hero.ID = 26 }))
	return srv, srv.cfg.Settings()
}

func BenchmarkSnapshot(b *testing.B) {
	srv, set := benchServer(b, 200)
	b.ResetTimer()
	for b.Loop() {
		srv.snapshot(set)
	}
}

func BenchmarkDrill(b *testing.B) {
	srv, _ := benchServer(b, 200)
	b.ResetTimer()
	for b.Loop() {
		srv.drill()
	}
}

func BenchmarkRulesResponse(b *testing.B) {
	srv, _ := benchServer(b, 200)
	b.ResetTimer()
	for b.Loop() {
		srv.rulesResponse()
	}
}

func BenchmarkHUDPayload(b *testing.B) {
	srv, _ := benchServer(b, 200)
	b.ResetTimer()
	for b.Loop() {
		srv.hudPayload()
	}
}

func BenchmarkSnapshotWhilePicking(b *testing.B) {
	srv, h, _ := newTestServer(b, nil)
	seed(b, srv, 200)
	postState(b, h, payload(-60, func(s *gsi.State) {
		s.Hero = &gsi.Hero{}
		s.Map.GameState = gsi.StateHeroSelection
	}))
	set := srv.cfg.Settings()
	b.ResetTimer()
	for b.Loop() {
		srv.snapshot(set)
	}
}

func BenchmarkPickHelp(b *testing.B) {
	srv, _ := benchServer(b, 200)
	b.ResetTimer()
	for b.Loop() {
		srv.pickHelp(config.RoleHardSupport)
	}
}

// BenchmarkPickHelpLongHistory is BenchmarkPickHelp over several seasons of matches.
func BenchmarkPickHelpLongHistory(b *testing.B) {
	srv, _ := benchServer(b, 1000)
	for b.Loop() {
		srv.pickHelp(config.RoleHardSupport)
	}
}

// BenchmarkGSI is one game-state post from Dota, start to finish, with several seasons of
// matches stored.
func BenchmarkGSI(b *testing.B) {
	srv, h, _ := newTestServer(b, nil)
	seed(b, srv, 1000)
	var bodies [][]byte
	for clock := 0; clock < 3600; clock++ {
		body, err := json.Marshal(payload(clock, func(s *gsi.State) { s.Hero.ID = 26 }))
		if err != nil {
			b.Fatal(err)
		}
		bodies = append(bodies, body)
	}
	i := 0
	for b.Loop() {
		post(b, h, bodies[i%len(bodies)])
		i++
	}
}

// BenchmarkRulesOverAMatch runs every rule over a simulated 40-minute match, one state per game
// second: what a whole match costs the rules engine.
func BenchmarkRulesOverAMatch(b *testing.B) {
	var states []*gsi.State
	for s := range sim.States(sim.Options{From: -60, To: 2400}) {
		states = append(states, s)
	}
	set := config.Default().Settings
	set.Role = config.RoleCarry
	for b.Loop() {
		e := coach.New(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
		for _, s := range states {
			e.Update(s, set)
		}
	}
}
