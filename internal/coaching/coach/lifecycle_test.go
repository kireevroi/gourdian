package coach

import (
	"testing"
	"time"

	"gourdian/internal/game/gsi"
)

func matchID(id string) func(*gsi.State) { return func(s *gsi.State) { s.Map.MatchID = id } }

func endMatch(e *Engine, id string, clock int) Result {
	s := state(clock)
	s.Map.MatchID, s.Map.GameState, s.Map.WinTeam = id, gsi.StatePostGame, "radiant"
	return e.Update(s, settings("carry"))
}

// Dota calls every lobby and bot game match "0", so two in a row are still two matches.
func TestBackToBackLobbyGamesAreTwoMatches(t *testing.T) {
	e := newEngine(fakeData{})
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	e.now = func() time.Time { return now }
	set := settings("carry")
	play(e, set, 0, 400, matchID("0"))
	first := endMatch(e, "0", 400).Finished
	if first == nil {
		t.Fatal("the first game wasn't recorded")
	}
	now = now.Add(time.Hour)
	s := state(0)
	s.Map.MatchID = "0"
	if r := e.Update(s, set); !r.NewMatch || r.MatchID == first.MatchID {
		t.Fatalf("second game: new %v, id %q (first %q)", r.NewMatch, r.MatchID, first.MatchID)
	}
	play(e, set, 1, 400, matchID("0"))
	second := endMatch(e, "0", 400).Finished
	if second == nil || second.MatchID == first.MatchID || second.Resumed {
		t.Fatalf("second game recorded as %+v", second)
	}
}

// A match that went quiet long enough to be recorded and then came back is carried on, and
// its real end says it replaces the early record.
func TestMatchThatComesBackReplacesItsEarlyRecord(t *testing.T) {
	e := newEngine(fakeData{})
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	e.now = func() time.Time { return now }
	set := settings("carry")
	play(e, set, 0, 600, nil)
	now = now.Add(10 * time.Minute)
	early := e.Expire(3 * time.Minute)
	if early == nil || early.Result != "unknown" || early.Resumed {
		t.Fatalf("expired match = %+v", early)
	}
	r := e.Update(state(900), set)
	if r.NewMatch || r.MatchID != early.MatchID {
		t.Fatalf("came back as new %v, id %q", r.NewMatch, r.MatchID)
	}
	play(e, set, 901, 1800, nil)
	final := endMatch(e, "123", 1800).Finished
	if final == nil || !final.Resumed || final.Result != "win" || final.DurationSec != 1800 || final.MatchID != early.MatchID {
		t.Fatalf("real end = %+v", final)
	}
}
