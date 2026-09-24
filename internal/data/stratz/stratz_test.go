package stratz

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fake(t *testing.T, calls *int, status int) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		if r.Header.Get("Authorization") != "Bearer tok" || r.Header.Get("User-Agent") != "STRATZ_API" {
			t.Errorf("sent headers %v", r.Header)
		}
		var body struct {
			Variables struct {
				Hero     int      `json:"hero"`
				Brackets []string `json:"brackets"`
			} `json:"variables"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.Variables.Hero != 1 || strings.Join(body.Variables.Brackets, ",") != "LEGEND_ANCIENT" {
			t.Errorf("asked for %+v", body.Variables)
		}
		w.WriteHeader(status)
		io.WriteString(w, `{"data":{"heroStats":{"heroVsHeroMatchup":{"advantage":[{"heroId":1,"vs":[
			{"heroId2":2,"matchCount":41234,"synergy":-6.4},{"heroId2":17,"matchCount":0,"synergy":9}]}]}}}}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAgainstReadsTheAdvantageAndCachesIt(t *testing.T) {
	calls, dir := 0, t.TempDir()
	srv := fake(t, &calls, http.StatusOK)
	c := New(dir, func() string { return "tok" }, slog.New(slog.DiscardHandler))
	c.SetURL(srv.URL)
	if got := c.Against(1, 54); got != nil {
		t.Fatalf("answered before loading: %v", got)
	}
	c.Wait()
	got := c.Against(1, 54)
	if e := got[2]; e.Games != 41234 || e.Pct != -6.4 {
		t.Errorf("Anti-Mage against Axe is %+v", e)
	}
	if _, ok := got[17]; ok {
		t.Error("kept a pair with no games")
	}

	again := New(dir, func() string { return "tok" }, slog.New(slog.DiscardHandler))
	again.SetURL(srv.URL)
	again.Against(1, 54)
	again.Wait()
	if again.Against(1, 54)[2].Games != 41234 || calls != 1 {
		t.Errorf("the week's copy on disk wasn't used: %d calls", calls)
	}
}

func TestNoTokenAsksNothing(t *testing.T) {
	calls := 0
	srv := fake(t, &calls, http.StatusOK)
	c := New(t.TempDir(), func() string { return "" }, slog.New(slog.DiscardHandler))
	c.SetURL(srv.URL)
	c.Against(1, 54)
	c.Wait()
	if calls != 0 {
		t.Errorf("called STRATZ without a token %d times", calls)
	}
}

func TestARefusedTokenIsRetriedOnlyAfterItChanges(t *testing.T) {
	calls := 0
	srv := fake(t, &calls, http.StatusForbidden)
	c := New(t.TempDir(), func() string { return "tok" }, slog.New(slog.DiscardHandler))
	c.SetURL(srv.URL)
	c.Against(1, 54)
	c.Wait()
	c.Against(1, 54)
	c.Wait()
	if calls != 1 {
		t.Errorf("a refused token was tried %d times in a row", calls)
	}
	c.Forget()
	c.Against(1, 54)
	c.Wait()
	if calls != 2 {
		t.Errorf("a new token wasn't tried: %d calls", calls)
	}
}

func TestBrackets(t *testing.T) {
	for tier, want := range map[int]string{0: "ALL", 15: "HERALD_GUARDIAN", 43: "CRUSADER_ARCHON", 54: "LEGEND_ANCIENT", 80: "DIVINE_IMMORTAL"} {
		if got := Bracket(tier); got != want {
			t.Errorf("rank tier %d is %s, want %s", tier, got, want)
		}
	}
}
