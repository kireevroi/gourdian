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

// heroStatsJSON is the shape of OpenDota's /heroStats: the bracket counts are named by number.
const heroStatsJSON = `[
  {"id": 1, "localized_name": "Anti-Mage", "roles": ["Carry", "Escape"], "attack_type": "Melee", "primary_attr": "agi",
   "1_pick": 100, "1_win": 60, "4_pick": 1000, "4_win": 520, "8_pick": 400, "8_win": 180},
  {"id": 26, "localized_name": "Lion", "roles": ["Support", "Disabler"], "attack_type": "Ranged", "primary_attr": "int",
   "4_pick": 800, "4_win": 440}
]`

func metaClient(t *testing.T, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "constants") {
			w.Write([]byte("{}"))
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c := New(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(c.Wait)
	c.SetBaseURL(srv.URL)
	c.Start(context.Background())
	return c
}

// waitForMeta gives the background fetch a moment, since Meta never blocks the caller.
func waitForMeta(t *testing.T, c *Client) map[int]HeroMeta {
	t.Helper()
	for range 100 {
		if m := c.Meta(); m != nil {
			return m
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("hero meta never loaded")
	return nil
}

func TestMetaReadsTheBracketCounts(t *testing.T) {
	meta := waitForMeta(t, metaClient(t, heroStatsJSON))
	am, ok := meta[1]
	if !ok {
		t.Fatalf("Anti-Mage missing from %v", meta)
	}
	if pct, games := am.WinPct(4); pct != 52 || games != 1000 {
		t.Errorf("at Archon = %d%% of %d, want 52%% of 1000", pct, games)
	}
	if pct, games := am.WinPct(8); pct != 45 || games != 400 {
		t.Errorf("at Immortal = %d%% of %d, want 45%% of 400", pct, games)
	}
	if !am.Melee() || am.PrimaryAttr != "agi" {
		t.Errorf("attack type %q, attribute %q", am.AttackType, am.PrimaryAttr)
	}
}

// A bracket with too few games to read falls back to every bracket together, so a rare hero
// still says something instead of nothing.
func TestMetaFallsBackToEveryBracket(t *testing.T) {
	meta := waitForMeta(t, metaClient(t, heroStatsJSON))
	am := meta[1]
	// Herald has 100 games, under metaMinGames, so the answer is all 1500 games together.
	pct, games := am.WinPct(1)
	if games != 1500 || pct != (60+520+180)*100/1500 {
		t.Errorf("at Herald = %d%% of %d, want every bracket together", pct, games)
	}
	if pct, games := am.WinPct(0); games != 1500 || pct == 0 {
		t.Errorf("unknown rank = %d%% of %d, want every bracket together", pct, games)
	}
}

// A hero OpenDota has no games for must not be reported as a 0% pick.
func TestMetaWithNoGamesSaysNothing(t *testing.T) {
	var empty HeroMeta
	if pct, games := empty.WinPct(4); pct != 0 || games != 0 {
		t.Errorf("empty meta = %d%% of %d, want nothing", pct, games)
	}
}
