package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gourdian/internal/stats"
)

func TestMMRPromptAfterARealMatch(t *testing.T) {
	srv, _, _ := newTestServer(t, nil)
	if err := srv.stats.AppendMMR(stats.MMREntry{Date: time.Now().Add(-time.Hour), MMR: 3000}); err != nil {
		t.Fatal(err)
	}
	srv.askForMMR(&stats.MatchSummary{MatchID: "123", Hero: "Lion", Result: "win", Source: stats.SourceLive})
	p := srv.pendingMMR()
	if p == nil || p.Last != 3000 || p.MatchID != "123" {
		t.Fatalf("prompt = %+v", p)
	}

	srv.askForMMR(&stats.MatchSummary{MatchID: "local-1", Hero: "Lion", Source: stats.SourcePractice})
	if srv.pendingMMR().MatchID != "123" {
		t.Fatal("a practice game replaced the prompt")
	}

	srv.confirmRanked("123", false)
	if srv.pendingMMR() != nil {
		t.Fatal("an unranked match should take the prompt away")
	}
}

func TestMMRChangeCountsEachMatchOnce(t *testing.T) {
	srv, _, _ := newTestServer(t, nil)
	if err := srv.stats.AppendMMR(stats.MMREntry{Date: time.Now().Add(-time.Hour), MMR: 3000}); err != nil {
		t.Fatal(err)
	}
	if err := srv.stats.AppendMatch(stats.MatchSummary{MatchID: "123", Hero: "Lion", Result: "win", Source: stats.SourceLive, EndedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	post := func(path, body string) int {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
		return rec.Code
	}
	last := func() int {
		entries, _ := srv.stats.MMR()
		return entries[len(entries)-1].MMR
	}
	srv.askForMMR(&stats.MatchSummary{MatchID: "123", Hero: "Lion", Result: "win", Source: stats.SourceLive})
	if code := post("/api/mmr/change", `{"change":25,"note":"win"}`); code != http.StatusOK || last() != 3025 {
		t.Fatalf("first press: status %d, mmr %d", code, last())
	}
	if code := post("/api/mmr/change", `{"change":25,"note":"win"}`); code != http.StatusConflict || last() != 3025 {
		t.Fatalf("a double click added the win twice: status %d, mmr %d", code, last())
	}
	for range 2 {
		if code := post("/api/matches/123/mmr", `{"change":25}`); code != http.StatusOK {
			t.Fatalf("status %d", code)
		}
	}
	if last() != 3025 {
		t.Fatalf("logging a match again stepped from its own entry: %d", last())
	}
	srv.askForMMR(&stats.MatchSummary{MatchID: "123", Hero: "Lion", Result: "win", Source: stats.SourceLive})
	if p := srv.pendingMMR(); p.Last != 3000 {
		t.Fatalf("the prompt should step from the MMR before this match, not %d", p.Last)
	}
}

func TestAMatchMarkedRankedStaysRanked(t *testing.T) {
	srv, _, _ := newTestServer(t, nil)
	if err := srv.stats.AppendMatch(stats.MatchSummary{MatchID: "123", Hero: "Lion", Source: stats.SourceLive, EndedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/matches/123/ranked", strings.NewReader(`{"ranked":true}`)))
	if rec.Code != http.StatusOK || srv.pendingMMR() == nil {
		t.Fatalf("status %d, prompt %+v", rec.Code, srv.pendingMMR())
	}
	srv.saveRanked("123", 0)
	if m, _ := srv.stats.Match("123"); !m.Ranked {
		t.Fatal("OpenDota's lobby type undid the player's own mark")
	}
	if p := srv.pendingMMR(); p == nil || !p.Ranked {
		t.Fatalf("the prompt for a match marked ranked went away: %+v", p)
	}
}

// The prompt is encoded for the dashboard while OpenDota's answer marks it ranked; that must
// not race (run with -race).
func TestConfirmingRankedDoesNotRaceWithReaders(t *testing.T) {
	srv, _, _ := newTestServer(t, nil)
	srv.mmr.prompt = &mmrPrompt{MatchID: "m1"}
	var wg sync.WaitGroup
	wg.Go(func() {
		for range 200 {
			srv.confirmRanked("m1", true)
		}
	})
	wg.Go(func() {
		for range 200 {
			if _, err := json.Marshal(srv.pendingMMR()); err != nil {
				t.Error(err)
			}
		}
	})
	wg.Wait()
	if p := srv.pendingMMR(); p == nil || !p.Ranked {
		t.Fatalf("prompt = %+v, want it marked ranked", p)
	}
}
