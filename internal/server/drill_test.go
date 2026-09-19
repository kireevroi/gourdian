package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gourdian/internal/stats"
)

func TestDrillScoresRecentMatches(t *testing.T) {
	srv, _, _ := newTestServer(t, nil)
	for i, count := range []int{4, 2, 0} {
		id := string(rune('a' + i))
		srv.stats.AppendMatch(stats.MatchSummary{MatchID: id, Hero: "Lion", Source: stats.SourceLive,
			EndedAt: time.Now().Add(-time.Duration(3-i) * time.Hour)})
		tips := make([]stats.TipRecord, count)
		for j := range tips {
			tips[j] = stats.TipRecord{At: time.Now(), MatchID: id, Rule: "no_tp", Habit: true, Text: "No TP scroll"}
		}
		srv.stats.AppendTips(tips)
	}
	// Imported history has no tips, since the trainer wasn't watching; it mustn't count as clean.
	srv.stats.AppendMatch(stats.MatchSummary{MatchID: "imported", Hero: "Lion", Source: stats.SourceOpenDota, EndedAt: time.Now()})
	set := srv.cfg.Settings()
	set.Drill = "no_tp"
	if err := srv.cfg.UpdateSettings(set); err != nil {
		t.Fatal(err)
	}
	v := srv.drill()
	if v.Rule != "no_tp" || v.Average != 2 || v.Best != 0 || len(v.Recent) != 3 {
		t.Fatalf("drill = %+v", v)
	}
	if v.Recent[0].MatchID != "c" {
		t.Fatalf("recent should be newest first: %+v", v.Recent)
	}
	if len(v.Choices) == 0 || v.Choices[0].Rule != "no_tp" {
		t.Fatalf("the worst habit should lead the choices: %+v", v.Choices)
	}
}

func TestSetDrillRejectsRulesWithoutHabits(t *testing.T) {
	srv, h, _ := newTestServer(t, nil)
	if code := putJSON(t, h, "/api/drill", `{"rule":"runes"}`); code != 400 {
		t.Fatalf("a rule with no habit was accepted: %d", code)
	}
	if code := putJSON(t, h, "/api/drill", `{"rule":"no_tp"}`); code != 200 {
		t.Fatalf("setting a drill: %d", code)
	}
	if srv.cfg.Settings().Drill != "no_tp" {
		t.Fatal("the drill was not saved")
	}
	if code := putJSON(t, h, "/api/drill", `{"rule":""}`); code != 200 || srv.cfg.Settings().Drill != "" {
		t.Fatal("clearing the drill")
	}
}

func putJSON(t *testing.T, h http.Handler, path, body string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, path, strings.NewReader(body)))
	return rec.Code
}
