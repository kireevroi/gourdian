package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"
	"time"

	"gourdian/internal/data/opendota"
	"gourdian/internal/game/model"
	"gourdian/internal/sys/config"
)

func statsCurves(t *testing.T, h http.Handler) map[string][]int {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/stats", nil))
	var body struct {
		Curves map[string][]int `json:"last_hits_by_minute"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("stats: %v: %s", err, rec.Body)
	}
	return body.Curves
}

func TestAnImportedMatchIsDrawnFromItsOwnCurve(t *testing.T) {
	srv, h, _ := newTestServer(t, nil)
	ended := time.Now().Add(-time.Hour)
	srv.stats.AppendMatch(model.MatchSummary{MatchID: "imported", Source: model.SourceOpenDota, Parsed: true,
		EndedAt: ended, LastHitsByMinute: []int{0, 3, 8}})
	srv.stats.AppendMatch(model.MatchSummary{MatchID: "live", Source: model.SourceLive,
		EndedAt: ended, LastHitsByMinute: []int{0, 99, 99}})
	srv.stats.AppendSamples([]model.Sample{{MatchID: "live", Clock: 60, LastHits: 5}})

	curves := statsCurves(t, h)
	if got := curves["imported"]; !slices.Equal(got, []int{0, 3, 8}) {
		t.Errorf("imported curve = %v, want OpenDota's", got)
	}
	if got := curves["live"]; len(got) < 2 || got[1] != 5 {
		t.Errorf("live curve = %v; what the trainer sampled should win over OpenDota's", got)
	}
}

func TestImportedMatchesAreGivenTheirCurveAtStart(t *testing.T) {
	parsed, err := os.ReadFile("../../data/opendota/testdata/match_parsed.json")
	if err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/matches/9000000001" {
			w.Write(parsed)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(api.Close)
	srv, _, _ := newTestServer(t, func(s *config.Settings) { s.AccountID = "100000001" })
	data := opendota.New(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(data.Wait)
	data.SetBaseURL(api.URL)
	srv.data = data
	srv.stats.AppendMatch(model.MatchSummary{MatchID: "9000000001", HeroID: 44, Source: model.SourceOpenDota,
		Parsed: true, EndedAt: time.Now().Add(-time.Hour)})

	srv.learnLastHitCurves(t.Context())
	m, err := srv.stats.Match("9000000001")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.LastHitsByMinute) != 9 || m.LastHitsByMinute[5] != 11 {
		t.Fatalf("curve = %v, want the nine minutes OpenDota parsed", m.LastHitsByMinute)
	}
}
