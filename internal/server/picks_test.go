package server

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"gourdian/internal/coach"
	"gourdian/internal/config"
	"gourdian/internal/dota"
	"gourdian/internal/gsi"
	"gourdian/internal/model"
	"gourdian/internal/picks"
)

func TestPickBoardUsesYourOwnRecord(t *testing.T) {
	srv, _, _ := newTestServer(t, nil)
	add := func(hero string, id int, role, result string, n int) {
		for i := range n {
			srv.stats.AppendMatch(model.MatchSummary{
				MatchID: hero + role + result + string(rune('a'+i)), Hero: hero, HeroID: id, Role: role,
				Result: result, Source: model.SourceLive, EndedAt: time.Now().Add(-time.Duration(i+1) * time.Hour),
				LastHitsAt: map[string]int{"10:00": 50},
			})
		}
	}
	add("Storm Spirit", 17, dota.Mid, "win", 4)
	add("Storm Spirit", 17, dota.Mid, "loss", 1)
	add("Invoker", 74, dota.Mid, "loss", 4)
	add("Invoker", 74, dota.Mid, "win", 1)
	add("Lion", 26, dota.HardSupport, "win", 3)
	add("Puck", 13, dota.Mid, "win", 1) // too few games to say anything

	p := srv.pickBoard(roleSet(srv, dota.Mid))
	if p == nil || len(p.Best) != 1 || p.Best[0].Name != "Storm Spirit" || p.Best[0].WinPct != 80 {
		t.Fatalf("best = %+v", p)
	}
	if len(p.Avoid) != 1 || p.Avoid[0].Name != "Invoker" || p.Avoid[0].WinPct != 20 {
		t.Fatalf("avoid = %+v", p.Avoid)
	}
	if p.Best[0].AvgLH10 != 50 {
		t.Fatalf("last hits = %d", p.Best[0].AvgLH10)
	}
	if other := srv.pickBoard(roleSet(srv, dota.Carry)); other != nil {
		t.Fatalf("no carry games, so no help: %+v", other)
	}
}

// roleSet is the player's settings with the position swapped, for asking about another one.
func roleSet(srv *Server, role string) config.Settings {
	set := srv.cfg.Settings()
	set.Role = role
	return set
}

// Pick help is for the draft: it used to wait for a match in progress without a hero, which
// Dota never sends, so it never showed.
func TestPickBoardShowsDuringTheDraft(t *testing.T) {
	srv, h, _ := newTestServer(t, func(s *config.Settings) { s.Role = dota.HardSupport })
	seed(t, srv, 10)
	draft := payload(-60, func(s *gsi.State) {
		s.Hero = &gsi.Hero{} // Dota's hero block before the pick: id 0
		s.Map.GameState = gsi.StateHeroSelection
	})
	postState(t, h, draft)
	if snap := srv.snapshot(srv.cfg.Settings()); snap.Picks == nil || len(snap.Picks.Best)+len(snap.Picks.Avoid) == 0 {
		t.Fatalf("no pick help during the draft: %+v", snap.Picks)
	}
	postState(t, h, payload(10, func(s *gsi.State) { s.Hero.ID = 26 })) // picked and playing
	if snap := srv.snapshot(srv.cfg.Settings()); snap.Picks != nil {
		t.Fatal("pick help still shown once the hero is picked")
	}
}

// The board is read out once as the draft opens, not twice a second for the whole draft.
func TestPickBoardIsSpokenOnceADraft(t *testing.T) {
	srv, h, _ := newTestServer(t, func(s *config.Settings) { s.Role = dota.Mid })
	for i := range 5 {
		srv.stats.AppendMatch(model.MatchSummary{
			MatchID: "storm" + string(rune('a'+i)), Hero: "Storm Spirit", HeroID: 17, Role: dota.Mid,
			Result: "win", Source: model.SourceLive, EndedAt: time.Now().Add(-time.Duration(i+1) * time.Hour)})
	}
	draft := func(clock int) *gsi.State {
		return payload(clock, func(s *gsi.State) {
			s.Hero = &gsi.Hero{} // Dota's hero block before the pick: id 0
			s.Map.GameState = gsi.StateHeroSelection
		})
	}
	postState(t, h, draft(-90))
	postState(t, h, draft(-89))
	spoken := picksSpoken(srv)
	if len(spoken) != 1 {
		t.Fatalf("picks spoken %d times, want once: %+v", len(spoken), spoken)
	}
	if !strings.Contains(spoken[0].Text, "Storm Spirit") {
		t.Errorf("spoken picks = %q", spoken[0].Text)
	}

	// Picking a hero ends the draft, and starting the match clears the tip list.
	postState(t, h, payload(10, func(s *gsi.State) { s.Hero.ID = 17 }))
	if got := picksSpoken(srv); len(got) != 0 {
		t.Fatalf("starting a match left %d pick tips behind", len(got))
	}
	// So the next draft speaking again shows up as one fresh tip.
	postState(t, h, draft(-90))
	if got := picksSpoken(srv); len(got) != 1 {
		t.Fatalf("the next draft spoke %d times, want once", len(got))
	}
}

// picksSpoken is the pick tips the trainer has read out in this match.
func picksSpoken(srv *Server) []coach.Tip {
	return slices.DeleteFunc(srv.engine.RecentTips(), func(t coach.Tip) bool { return t.Rule != "picks" })
}

// The pick tuning is saved like any other setting, rejected when it makes no sense, and reset
// by sending a null.
func TestPickTuningIsSavedCheckedAndReset(t *testing.T) {
	srv, _, _ := newTestServer(t, nil)
	put := func(body string) int {
		req := httptest.NewRequest("PUT", "/api/settings", strings.NewReader(body))
		w := httptest.NewRecorder()
		srv.handlePutSettings(w, req)
		return w.Code
	}
	if code := put(`{"picks":{"half_life_days":14,"show":6}}`); code != http.StatusOK {
		t.Fatalf("saving a tuning returned %d", code)
	}
	if got := srv.cfg.Settings().Picks; got.HalfLifeDays != 14 || got.Show != 6 || got.Days != picks.DefaultTuning().Days {
		t.Fatalf("tuning = %+v; the fields sent should change and the rest should stay", got)
	}
	if code := put(`{"picks":{"half_life_days":0}}`); code == http.StatusOK {
		t.Error("a zero half-life was accepted")
	}
	if got := srv.cfg.Settings().Picks.HalfLifeDays; got != 14 {
		t.Errorf("a rejected value changed the saved tuning to %d", got)
	}
	if code := put(`{"picks":null}`); code != http.StatusOK {
		t.Fatalf("resetting returned %d", code)
	}
	if got := srv.cfg.Settings().Picks; got != picks.DefaultTuning() {
		t.Errorf("after a reset the tuning is %+v", got)
	}
}

// Changing a pick setting must change the board: the cache key has to cover everything the
// board is made of, or a new setting looks like it did nothing.
func TestChangingTheTuningRebuildsTheBoard(t *testing.T) {
	srv, _, _ := newTestServer(t, func(s *config.Settings) { s.Role = dota.Mid })
	add := func(hero string, id, n, wins int) {
		for i := range n {
			result := "loss"
			if i < wins {
				result = "win"
			}
			srv.stats.AppendMatch(model.MatchSummary{
				MatchID: hero + string(rune('a'+i)), Hero: hero, HeroID: id, Role: dota.Mid,
				Result: result, Source: model.SourceLive, EndedAt: time.Now().Add(-time.Duration(i+1) * time.Hour)})
		}
	}
	add("Steady", 1, 40, 24) // 60% of 40
	add("Lucky", 2, 8, 5)    // 62% of 8

	set := roleSet(srv, dota.Mid)
	if first := srv.pickBoard(set).Best[0].Name; first != "Steady" {
		t.Fatalf("by default %q ranks first, want Steady", first)
	}
	set.Picks.TrustAfter = 0
	if first := srv.pickBoard(set).Best[0].Name; first != "Lucky" {
		t.Errorf("taking records at face value still ranks %q first, want Lucky", first)
	}
}

// A player picks early and then watches the rest of the draft. Which hero to take is settled
// by then, but who the other side is taking is not, and it is what tells them what to buy.
func TestTheDraftStaysOnScreenAfterYouPick(t *testing.T) {
	srv, h, _ := newTestServer(t, func(s *config.Settings) {
		s.Role = dota.Mid
		s.Screen.Draft = true
	})
	for i := range 5 {
		srv.stats.AppendMatch(model.MatchSummary{
			MatchID: "storm" + string(rune('a'+i)), Hero: "Storm Spirit", HeroID: 17, Role: dota.Mid,
			Result: "win", Source: model.SourceLive, EndedAt: time.Now().Add(-time.Duration(i+1) * time.Hour)})
	}
	choosing := func(s *gsi.State) {
		s.Hero = &gsi.Hero{} // Dota's hero block before the pick: id 0
		s.Map.GameState = gsi.StateHeroSelection
		s.Map.MatchID = "m1"
	}
	postState(t, h, payload(-90, choosing))
	postDraft(t, srv, `{"ours":[17],"theirs":[35,26]}`)

	before := srv.snapshot(srv.cfg.Settings()).Picks
	if before == nil || len(before.Best) == 0 {
		t.Fatalf("no pick advice while choosing: %+v", before)
	}

	// Now they have taken a hero, and the draft goes on around them.
	postState(t, h, payload(-60, func(s *gsi.State) {
		choosing(s)
		s.Hero.ID = 17
	}))
	after := srv.snapshot(srv.cfg.Settings()).Picks
	if after == nil {
		t.Fatal("the draft vanished the moment a hero was taken")
	}
	if len(after.Best)+len(after.Fresh)+len(after.Avoid) != 0 {
		t.Errorf("still advising which hero to take after one was taken: %+v", after)
	}
	if len(after.Enemies) != 2 {
		t.Errorf("the other side's heroes went with it: %+v", after.Enemies)
	}

	// Once the game is under way it goes entirely.
	postState(t, h, payload(30, func(s *gsi.State) { s.Hero.ID = 17; s.Map.MatchID = "m1" }))
	if snap := srv.snapshot(srv.cfg.Settings()); snap.Picks != nil {
		t.Errorf("the draft is still shown in the match: %+v", snap.Picks)
	}
}

// The overlay reads the screen only while the trainer asks it to, so that has to last the
// whole draft. It used to stop the moment the player picked, and the reading it had already
// taken was then dropped for going stale, so the other side vanished a few seconds later.
func TestTheScreenIsReadForTheWholeDraft(t *testing.T) {
	srv, h, _ := newTestServer(t, func(s *config.Settings) { s.Screen.Draft = true })
	choosing := func(s *gsi.State) {
		s.Hero = &gsi.Hero{}
		s.Map.GameState = gsi.StateHeroSelection
		s.Map.MatchID = "m1"
	}
	postState(t, h, payload(-90, choosing))
	if !srv.hudPayload().Draft {
		t.Fatal("not reading the screen while choosing a hero")
	}
	postState(t, h, payload(-60, func(s *gsi.State) { choosing(s); s.Hero.ID = 17 }))
	if !srv.hudPayload().Draft {
		t.Error("stopped reading the screen as soon as a hero was taken")
	}
	// Strategy time is still the draft; the rest of them are still picking.
	postState(t, h, payload(-30, func(s *gsi.State) {
		choosing(s)
		s.Hero.ID = 17
		s.Map.GameState = gsi.StateStrategyTime
	}))
	if !srv.hudPayload().Draft {
		t.Error("stopped reading the screen during strategy time")
	}
	// Once the game is under way there is nothing left to read.
	postState(t, h, payload(30, func(s *gsi.State) { s.Hero.ID = 17; s.Map.MatchID = "m1" }))
	if srv.hudPayload().Draft {
		t.Error("still reading the screen once the match had started")
	}
}

// And with the setting off, the screen is never read whatever the game is doing.
func TestTheScreenIsNotReadWhenTurnedOff(t *testing.T) {
	srv, h, _ := newTestServer(t, nil)
	postState(t, h, payload(-90, func(s *gsi.State) {
		s.Hero = &gsi.Hero{}
		s.Map.GameState = gsi.StateHeroSelection
	}))
	if srv.hudPayload().Draft {
		t.Error("read the screen with the setting off")
	}
}

// Pick advice is worked out for a position, so the position keys have to work while the
// player is still choosing a hero. They used to come alive only once a match had started,
// which is after the one moment they matter most.
func TestThePositionCanBeSetWhileChoosing(t *testing.T) {
	srv, h, _ := newTestServer(t, func(s *config.Settings) { s.Role = dota.Carry })
	choosing := func(s *gsi.State) {
		s.Hero = &gsi.Hero{} // Dota's hero block before the pick: id 0
		s.Map.GameState = gsi.StateHeroSelection
		s.Map.MatchID = "m1"
	}
	postState(t, h, payload(-90, choosing))
	if !srv.hudPayload().Choosing {
		t.Fatal("the position keys are dead while the player is choosing a hero")
	}

	setRole(t, srv, dota.Offlane)
	if got := srv.cfg.Settings().Role; got != dota.Offlane {
		t.Fatalf("role is %q after choosing offlane", got)
	}

	// The match starting on the hero they took must not quietly undo it. Hero 17 is one this
	// player has played as mid, which is what would otherwise be restored over their choice.
	srv.rememberHeroRole(17, dota.Mid)
	setRole(t, srv, dota.Offlane)
	postState(t, h, payload(-60, func(s *gsi.State) {
		s.Map.MatchID = "m1"
		s.Map.GameState = gsi.StatePreGame
		s.Hero.ID = 17
	}))
	if got := srv.cfg.Settings().Role; got != dota.Offlane {
		t.Errorf("picking a hero put the role back to %q over the player's own choice", got)
	}
	// And it is remembered against the hero, so next time starts from what they meant.
	if got := srv.cfg.Settings().HeroRoles["17"]; got != dota.Offlane {
		t.Errorf("the hero remembers %q, not what the player chose", got)
	}
}

// Changing position mid-draft changes the advice entirely, so it is read out again.
func TestTheAdviceIsReadAgainWhenThePositionChanges(t *testing.T) {
	srv, h, _ := newTestServer(t, func(s *config.Settings) { s.Role = dota.Mid })
	for _, hero := range []struct {
		name string
		id   int
		role string
	}{{"Storm Spirit", 17, dota.Mid}, {"Undying", 85, dota.Offlane}} {
		for i := range 5 {
			srv.stats.AppendMatch(model.MatchSummary{
				MatchID: hero.name + string(rune('a'+i)), Hero: hero.name, HeroID: hero.id, Role: hero.role,
				Result: "win", Source: model.SourceLive, EndedAt: time.Now().Add(-time.Duration(i+1) * time.Hour)})
		}
	}
	draft := func(clock int) *gsi.State {
		return payload(clock, func(s *gsi.State) {
			s.Hero = &gsi.Hero{}
			s.Map.GameState = gsi.StateHeroSelection
		})
	}
	postState(t, h, draft(-90))
	first := picksSpoken(srv)
	if len(first) != 1 || !strings.Contains(first[0].Text, "Storm Spirit") {
		t.Fatalf("the mid advice wasn't read out: %+v", first)
	}

	setRole(t, srv, dota.Offlane)
	postState(t, h, draft(-80))
	again := picksSpoken(srv)
	if len(again) != 2 {
		t.Fatalf("changing position read out %d lots of advice, want a second: %+v", len(again), again)
	}
	if !strings.Contains(again[1].Text, "Undying") {
		t.Errorf("the second reading isn't the offlane advice: %q", again[1].Text)
	}

	// Saying the same position again is not news.
	setRole(t, srv, dota.Offlane)
	postState(t, h, draft(-70))
	if got := picksSpoken(srv); len(got) != 2 {
		t.Errorf("the same position was read out again: %d lots", len(got))
	}
}

// setRole is the position hotkey, as the overlay sends it.
func setRole(t *testing.T, srv *Server, role string) {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/role", strings.NewReader(`{"role":"`+role+`"}`))
	w := httptest.NewRecorder()
	srv.handleRole(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("setting the position returned %d: %s", w.Code, w.Body)
	}
}
