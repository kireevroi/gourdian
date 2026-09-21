package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gourdian/internal/data/mmr"
	"gourdian/internal/game/model"
	"gourdian/internal/sys/config"
)

func TestMMRPromptAfterARealMatch(t *testing.T) {
	srv, _, _ := newTestServer(t, nil)
	if err := srv.stats.AppendMMR(model.MMREntry{Date: time.Now().Add(-time.Hour), MMR: 3000}); err != nil {
		t.Fatal(err)
	}
	srv.askForMMR(&model.MatchSummary{MatchID: "123", Hero: "Lion", Result: "win", Source: model.SourceLive})
	p := srv.mmr.Pending()
	if p == nil || p.Last != 3000 || p.MatchID != "123" {
		t.Fatalf("prompt = %+v", p)
	}

	srv.askForMMR(&model.MatchSummary{MatchID: "local-1", Hero: "Lion", Source: model.SourcePractice})
	if srv.mmr.Pending().MatchID != "123" {
		t.Fatal("a practice game replaced the prompt")
	}

	srv.confirmRanked("123", false)
	if srv.mmr.Pending() != nil {
		t.Fatal("an unranked match should take the prompt away")
	}
}

func TestMMRChangeCountsEachMatchOnce(t *testing.T) {
	srv, _, _ := newTestServer(t, nil)
	if err := srv.stats.AppendMMR(model.MMREntry{Date: time.Now().Add(-time.Hour), MMR: 3000}); err != nil {
		t.Fatal(err)
	}
	if err := srv.stats.AppendMatch(model.MatchSummary{MatchID: "123", Hero: "Lion", Result: "win", Source: model.SourceLive, EndedAt: time.Now()}); err != nil {
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
	srv.askForMMR(&model.MatchSummary{MatchID: "123", Hero: "Lion", Result: "win", Source: model.SourceLive})
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
	srv.askForMMR(&model.MatchSummary{MatchID: "123", Hero: "Lion", Result: "win", Source: model.SourceLive})
	if p := srv.mmr.Pending(); p.Last != 3000 {
		t.Fatalf("the prompt should step from the MMR before this match, not %d", p.Last)
	}
}

func TestAMatchMarkedRankedStaysRanked(t *testing.T) {
	srv, _, _ := newTestServer(t, nil)
	if err := srv.stats.AppendMatch(model.MatchSummary{MatchID: "123", Hero: "Lion", Source: model.SourceLive, EndedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/matches/123/ranked", strings.NewReader(`{"ranked":true}`)))
	if rec.Code != http.StatusOK || srv.mmr.Pending() == nil {
		t.Fatalf("status %d, prompt %+v", rec.Code, srv.mmr.Pending())
	}
	srv.saveMatchKind("123", 0, 0)
	if m, _ := srv.stats.Match("123"); !m.Ranked {
		t.Fatal("OpenDota's lobby type undid the player's own mark")
	}
	if p := srv.mmr.Pending(); p == nil || !p.Ranked {
		t.Fatalf("the prompt for a match marked ranked went away: %+v", p)
	}
}

func TestMMRGoalForecastsTheChosenRank(t *testing.T) {
	srv, h, _ := newTestServer(t, func(s *config.Settings) { s.Language = "ru" })
	for _, e := range []model.MMREntry{{Date: time.Now().AddDate(0, 0, -10), MMR: 3000}, {Date: time.Now(), MMR: 3040}} {
		if err := srv.stats.AppendMMR(e); err != nil {
			t.Fatal(err)
		}
	}
	put := func(body string) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(body)))
		return rec.Code
	}
	if code := put(`{"mmr_goal":56}`); code != http.StatusBadRequest {
		t.Fatalf("Legend 6 isn't a rank, but saving it gave %d", code)
	}
	if code := put(`{"mmr_goal":51}`); code != http.StatusOK {
		t.Fatalf("saving Legend 1 as the goal: status %d", code)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/mmr/goal", nil))
	var got struct {
		Goal     int          `json:"goal"`
		Ranks    []rankChoice `json:"ranks"`
		Forecast *mmr.Forecast
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status %d, %v: %s", rec.Code, err, rec.Body)
	}
	if got.Goal != 51 || len(got.Ranks) != 36 || got.Ranks[20].Name != "Легенда 1" || got.Ranks[20].MMR != 3080 {
		t.Fatalf("goal %d, ranks %+v", got.Goal, got.Ranks)
	}
	if f := got.Forecast; f == nil || f.Goal != 3080 || f.Status != mmr.Closing || f.DaysLeft != 10 {
		t.Fatalf("forecast %+v, want 40 to go at +40 in 10 days", f)
	}
}
