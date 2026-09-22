package server

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"gourdian/internal/game/dota"
	"gourdian/internal/game/gsi"
	"gourdian/internal/game/model"
	"gourdian/internal/sys/config"
	"time"
)

func postDraft(t *testing.T, srv *Server, body string) int {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/draft", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleDraftSeen(w, req)
	return w.Code
}

// seeDraft posts the same reading twice, as two screen reads in a row of a settled draft do:
// a hero only reaches the board once two readings agree on it.
func seeDraft(t *testing.T, srv *Server, body string) int {
	t.Helper()
	postDraft(t, srv, body)
	return postDraft(t, srv, body)
}

// A reading off the screen is refused until the player has asked for it, because it means the
// trainer looks at their screen.
func TestTheScreenIsNotReadUntilItIsTurnedOn(t *testing.T) {
	srv, _, _ := newTestServer(t, nil)
	if code := postDraft(t, srv, `{"theirs":[35,26]}`); code != http.StatusConflict {
		t.Fatalf("a reading was taken with the setting off: %d", code)
	}
	if got := func() []int { _, t := srv.sides(""); return t }(); len(got) != 0 {
		t.Errorf("enemies %v were remembered anyway", got)
	}
}

func TestAReadingReachesThePickBoard(t *testing.T) {
	srv, h, _ := newTestServer(t, func(s *config.Settings) {
		s.Role = dota.Mid
		s.Screen.Draft = true
	})
	postState(t, h, payload(-90, func(s *gsi.State) {
		s.Hero = &gsi.Hero{}
		s.Map.GameState = gsi.StateHeroSelection
		s.Map.MatchID = "m1"
	}))
	if code := seeDraft(t, srv, `{"ours":[17],"theirs":[35,26,14]}`); code != http.StatusOK {
		t.Fatalf("the reading was refused: %d", code)
	}
	got := func() []int { _, t := srv.sides("m1"); return t }()
	if len(got) != 3 || got[0] != 35 {
		t.Fatalf("enemies = %v, want the three that were read", got)
	}
	// A reading belongs to the match it was taken in, not to the next one.
	if other := func() []int { _, t := srv.sides("m2"); return t }(); len(other) != 0 {
		t.Errorf("a later match inherited %v", other)
	}
}

// A reading with repeats or nonsense in it must not reach the advice: Dota lets nobody take a
// hero someone else has.
func TestAReadingIsTidiedUp(t *testing.T) {
	srv, h, _ := newTestServer(t, func(s *config.Settings) { s.Screen.Draft = true })
	postState(t, h, payload(-90, func(s *gsi.State) {
		s.Hero = &gsi.Hero{}
		s.Map.GameState = gsi.StateHeroSelection
		s.Map.MatchID = "m1"
	}))
	if code := seeDraft(t, srv, `{"theirs":[35,35,0,-4,26]}`); code != http.StatusOK {
		t.Fatalf("refused: %d", code)
	}
	if got := func() []int { _, t := srv.sides("m1"); return t }(); len(got) != 2 || got[0] != 35 || got[1] != 26 {
		t.Errorf("enemies = %v, want [35 26]", got)
	}
}

func TestWhereThePortraitsWereFoundIsRemembered(t *testing.T) {
	srv, _, _ := newTestServer(t, func(s *config.Settings) { s.Screen.Draft = true })
	body := `{"theirs":[35],"screen":"2560x1440","bar":{"left":{"x":277,"y":8,"w":825,"h":88},"right":{"x":1459,"y":8,"w":825,"h":88}}}`
	if code := postDraft(t, srv, body); code != http.StatusOK {
		t.Fatalf("refused: %d", code)
	}
	bars := srv.cfg.Settings().Screen.Bars
	if got, ok := bars["2560x1440"]; !ok || got.Left.X != 277 || got.Right.W != 825 {
		t.Errorf("the bar wasn't remembered: %+v", bars)
	}
}

// The draft fills in a hero at a time, and the advice has to follow it the whole way rather
// than settling on whatever the first reading happened to see.
func TestTheBoardFollowsTheDraftAsItFillsIn(t *testing.T) {
	srv, h, _ := newTestServer(t, func(s *config.Settings) {
		s.Role = dota.Mid
		s.Screen.Draft = true
	})
	for i := range 6 {
		srv.stats.AppendMatch(model.MatchSummary{
			MatchID: "storm" + string(rune('a'+i)), Hero: "Storm Spirit", HeroID: 17, Role: dota.Mid,
			Result: "win", Source: model.SourceLive, EndedAt: time.Now().Add(-time.Duration(i+1) * time.Hour)})
	}
	postState(t, h, payload(-90, func(s *gsi.State) {
		s.Hero = &gsi.Hero{}
		s.Map.GameState = gsi.StateHeroSelection
		s.Map.MatchID = "m1"
	}))

	seen := func() int {
		b := srv.snapshot(srv.cfg.Settings()).Picks
		if b == nil {
			return -1
		}
		return len(b.Enemies)
	}
	if got := seen(); got != 0 {
		t.Fatalf("enemies before anything was read: %d", got)
	}
	for _, step := range []struct {
		body string
		want int
	}{
		{`{"theirs":[35]}`, 1},
		{`{"theirs":[35,26]}`, 2},
		{`{"theirs":[35,26,14]}`, 3},
		{`{"theirs":[35,26,14,2,25]}`, 5},
	} {
		if code := seeDraft(t, srv, step.body); code != http.StatusOK {
			t.Fatalf("reading %q refused: %d", step.body, code)
		}
		if got := seen(); got != step.want {
			t.Errorf("after %s the board shows %d enemies, want %d", step.body, got, step.want)
		}
	}
	if got := seen(); got != 5 {
		t.Errorf("the board ended with %d enemies, want the whole side", got)
	}
}

func TestAHeroJoinsOnceTwoReadingsAgreeAndThenStays(t *testing.T) {
	var s sightings
	s.see([]int{35})
	if len(s.kept) != 0 {
		t.Fatalf("one reading put %v on the board", s.kept)
	}
	s.see([]int{35, 26})
	if !slices.Equal(s.kept, []int{35}) {
		t.Fatalf("kept %v after two readings of 35, want [35]", s.kept)
	}
	s.see([]int{26})
	s.see(nil)
	if !slices.Equal(s.kept, []int{35, 26}) {
		t.Fatalf("kept %v, want a hero missing from later readings to stay", s.kept)
	}
}

func TestAOneOffMisreadNeverJoins(t *testing.T) {
	var s sightings
	for _, reading := range [][]int{{35}, {35, 99}, {35}, {35, 99}} {
		s.see(reading)
	}
	if slices.Contains(s.kept, 99) {
		t.Fatalf("kept %v; 99 was never read twice in a row", s.kept)
	}
}

func TestASideHoldsNoMoreThanFive(t *testing.T) {
	var s sightings
	all := []int{1, 2, 3, 4, 5, 6, 7}
	s.see(all)
	s.see(all)
	if len(s.kept) != 5 {
		t.Fatalf("kept %d heroes, want the five a side can have", len(s.kept))
	}
}

// With one portrait flickering between read and missed, each reading used to change the other
// side, which restarted the wait for the picks to settle, so the advice was never read again.
func TestTheAdviceComesAgainWhenAPortraitFlickers(t *testing.T) {
	srv, h, _ := newTestServer(t, func(s *config.Settings) {
		s.Role = dota.Mid
		s.Screen.Draft = true
	})
	postState(t, h, payload(-90, func(s *gsi.State) {
		s.Hero = &gsi.Hero{}
		s.Map.GameState = gsi.StateHeroSelection
		s.Map.MatchID = "m1"
	}))
	var c pickCache
	at := time.Now()
	c.sayNow(dota.Mid, nil, at)
	spoken := 0
	for _, body := range []string{`{"theirs":[35,26]}`, `{"theirs":[35,26]}`, `{"theirs":[35]}`,
		`{"theirs":[35,26]}`, `{"theirs":[35]}`, `{"theirs":[35,26]}`, `{"theirs":[35]}`} {
		postDraft(t, srv, body)
		_, theirs := srv.sides("m1")
		at = at.Add(2 * time.Second)
		if c.sayNow(dota.Mid, theirs, at) {
			spoken++
		}
	}
	if spoken != 1 {
		t.Fatalf("the advice was read out %d times as the portrait flickered, want once", spoken)
	}
}
