package coach

import (
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"gourdian/internal/config"
	"gourdian/internal/dota"
	"gourdian/internal/dotadata"
	"gourdian/internal/gsi"
	"gourdian/internal/model"
)

type fakeData struct {
	items  map[string]dotadata.ItemInfo
	build  *dotadata.Build
	skills *dotadata.SkillBuild
}

func (f fakeData) Items() map[string]dotadata.ItemInfo { return f.items }
func (f fakeData) Hero(id int) (dotadata.HeroInfo, bool) {
	return dotadata.HeroInfo{ID: id, LocalizedName: "Anti-Mage"}, true
}
func (f fakeData) BuildFor(int, string) *dotadata.Build           { return f.build }
func (f fakeData) SkillBuildFor(int, string) *dotadata.SkillBuild { return f.skills }
func (f fakeData) AbilityName(name string) string                 { return name }

func newEngine(data Data) *Engine {
	return New(data, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func settings(role string) config.Settings {
	s := config.Default().Settings
	s.Role = role
	return s
}

func state(clock int) *gsi.State {
	s := &gsi.State{
		Map:    &gsi.Map{MatchID: "123", ClockTime: clock, GameTime: clock + 90, GameState: gsi.StateInProgress},
		Player: &gsi.Player{TeamName: "radiant", Gold: 50},
		Hero: &gsi.Hero{ID: 1, Name: "npc_dota_hero_antimage", Level: 1, Alive: true, XPos: 4000, YPos: -6000,
			Health: 600, MaxHealth: 600, HealthPercent: 100, Mana: 300, MaxMana: 300, ManaPercent: 100},
		Abilities: map[string]gsi.Ability{"ability0": {Name: "antimage_mana_break", Level: 1}},
		Items:     map[string]gsi.Item{},
	}
	for _, slot := range []string{"slot0", "slot1", "slot2", "slot3", "slot4", "slot5", "slot6", "slot7", "slot8",
		"stash0", "stash1", "stash2", "stash3", "stash4", "stash5", "neutral0"} {
		s.Items[slot] = gsi.Item{Name: "empty"}
	}
	s.Items["teleport0"] = gsi.Item{Name: "item_tpscroll", CanCast: true, Charges: 1}
	return s
}

// play feeds one update per game second and returns the tips produced.
func play(e *Engine, set config.Settings, from, to int, mutate func(*gsi.State)) []Tip {
	var tips []Tip
	for clock := from; clock <= to; clock++ {
		s := state(clock)
		if mutate != nil {
			mutate(s)
		}
		tips = append(tips, e.Update(s, set).Tips...)
	}
	return tips
}

func byRule(tips []Tip, rule string) []Tip {
	var out []Tip
	for _, t := range tips {
		if t.Rule == rule {
			out = append(out, t)
		}
	}
	return out
}

func TestNoTPWaitsThenRespectsCooldown(t *testing.T) {
	e := newEngine(nil)
	tips := play(e, settings(dota.Carry), 100, 205, func(s *gsi.State) {
		s.Items["teleport0"] = gsi.Item{Name: "empty"}
		s.Player.Gold = 500
	})
	got := byRule(tips, "no_tp")
	if len(got) != 2 || got[0].Clock != 110 || got[1].Clock != 200 {
		t.Fatalf("want no_tp at 110 and 200, got %+v", got)
	}
}

func TestNoTPSilentWithoutGoldOrWhenInStash(t *testing.T) {
	e := newEngine(nil)
	tips := play(e, settings(dota.Carry), 100, 200, func(s *gsi.State) {
		s.Items["teleport0"] = gsi.Item{Name: "empty"}
		s.Player.Gold = 500
		s.Items["stash0"] = gsi.Item{Name: "item_tpscroll"}
	})
	tips = append(tips, play(newEngine(nil), settings(dota.Carry), 100, 200, func(s *gsi.State) {
		s.Items["teleport0"] = gsi.Item{Name: "empty"}
		s.Player.Gold = 40
	})...)
	if got := byRule(tips, "no_tp"); len(got) != 0 {
		t.Fatalf("unexpected no_tp tips: %+v", got)
	}
}

func TestRunesOnlyForRolesThatTakeThem(t *testing.T) {
	if got := byRule(play(newEngine(nil), settings(dota.Carry), 220, 250, nil), "runes"); len(got) != 0 {
		t.Fatalf("carry should not get rune reminders: %+v", got)
	}
	// Patch 7.41: bounty runes at 0:00 and every 4:00 after, power runes from 6:00 every 2:00.
	bounty := byRule(play(newEngine(nil), settings(dota.Mid), 220, 250, nil), "runes")
	if len(bounty) != 1 || bounty[0].Clock != 225 || !strings.Contains(bounty[0].Text, "Bounty runes spawn in 15s (4:00)") {
		t.Fatalf("want a bounty reminder at 3:45, got %+v", bounty)
	}
	tips := play(newEngine(nil), settings(dota.Mid), 340, 360, nil)
	if got := byRule(tips, "runes"); len(got) != 0 {
		t.Fatalf("no bounty runes at 6:00 any more: %+v", got)
	}
	power := byRule(tips, "power_runes")
	if len(power) != 1 || power[0].Clock != 345 || !strings.Contains(power[0].Text, "Power runes spawn in 15s (6:00)") {
		t.Fatalf("want a power rune reminder at 345, got %+v", power)
	}
}

func TestDisabledRuleDoesNotFire(t *testing.T) {
	set := settings(dota.Mid)
	set.DisabledRules = []string{"runes"}
	if got := byRule(play(newEngine(nil), set, 340, 360, nil), "runes"); len(got) != 0 {
		t.Fatalf("disabled rule fired: %+v", got)
	}
}

func TestPausedGameProducesNoTips(t *testing.T) {
	tips := play(newEngine(nil), settings(dota.Carry), 100, 200, func(s *gsi.State) {
		s.Map.Paused = true
		s.Items["teleport0"] = gsi.Item{Name: "empty"}
		s.Player.Gold = 500
	})
	if len(tips) != 0 {
		t.Fatalf("paused game produced tips: %+v", tips)
	}
}

func TestSkillPointIgnoresInnateOffset(t *testing.T) {
	levelUp := func(delay int) func(*gsi.State) {
		return func(s *gsi.State) {
			s.Abilities["innate"] = gsi.Ability{Name: "antimage_persectur", Level: 1, Passive: true}
			s.Hero.Level = 1
			s.Abilities["ability0"] = gsi.Ability{Name: "antimage_mana_break", Level: 1}
			if s.Map.ClockTime >= 100 {
				s.Hero.Level = 2
				if s.Map.ClockTime >= 100+delay {
					s.Abilities["ability1"] = gsi.Ability{Name: "antimage_blink", Level: 1}
				}
			}
		}
	}
	if got := byRule(play(newEngine(nil), settings(dota.Carry), 60, 160, levelUp(3)), "skill_points"); len(got) != 0 {
		t.Fatalf("prompt level-up should not warn: %+v", got)
	}
	got := byRule(play(newEngine(nil), settings(dota.Carry), 60, 160, levelUp(40)), "skill_points")
	if len(got) != 1 || got[0].Clock != 115 {
		t.Fatalf("want one skill point warning at 115, got %+v", got)
	}
}

func TestSkillPointIgnoresAnAbilityLevelAheadOfTheHeroLevel(t *testing.T) {
	// Recorded in a Chen game: one update had the ability levelled while the hero was still a
	// level behind, and every later level-up looked like an unspent point.
	got := byRule(play(newEngine(nil), settings(dota.Carry), 60, 300, func(s *gsi.State) {
		s.Hero.Level = 2
		s.Abilities["ability1"] = gsi.Ability{Name: "antimage_blink", Level: 1}
		if s.Map.ClockTime == 100 {
			s.Abilities["ability2"] = gsi.Ability{Name: "antimage_counterspell", Level: 1}
		}
		if s.Map.ClockTime > 100 {
			s.Hero.Level = 3
			s.Abilities["ability2"] = gsi.Ability{Name: "antimage_counterspell", Level: 1}
		}
		if s.Map.ClockTime > 200 {
			s.Hero.Level = 4
			s.Abilities["ability1"] = gsi.Ability{Name: "antimage_blink", Level: 2}
		}
	}), "skill_points")
	if len(got) != 0 {
		t.Fatalf("every point was spent: %+v", got)
	}
}

func TestSkillPointStopsNagging(t *testing.T) {
	got := byRule(play(newEngine(nil), settings(dota.Carry), 60, 400, func(s *gsi.State) {
		if s.Map.ClockTime >= 100 {
			s.Hero.Level = 2
		}
	}), "skill_points")
	if len(got) == 0 || len(got) > 3 || got[len(got)-1].Clock > 260 {
		t.Fatalf("want a couple of reminders and then quiet, got %d: %+v", len(got), got)
	}
}

func TestLowHPSuggestsWand(t *testing.T) {
	got := byRule(play(newEngine(nil), settings(dota.Carry), 300, 301, func(s *gsi.State) {
		s.Hero.HealthPercent, s.Hero.Health = 20, 120
		s.Items["slot2"] = gsi.Item{Name: "item_magic_wand", CanCast: true, Charges: 9}
	}), "low_hp")
	if len(got) != 1 || !strings.Contains(got[0].Text, "Use magic wand and back off") || got[0].Severity != Urgent {
		t.Fatalf("want one urgent wand suggestion, got %+v", got)
	}
}

func TestLowHPWithoutAnItemJustSaysBackOff(t *testing.T) {
	got := byRule(play(newEngine(nil), settings(dota.Carry), 300, 301, func(s *gsi.State) {
		s.Hero.HealthPercent, s.Hero.Health = 20, 120
	}), "low_hp")
	if len(got) != 1 || !strings.HasSuffix(got[0].Text, "). Back off") {
		t.Fatalf("want one back-off warning, got %+v", got)
	}
}

func TestLowHPIsSaidOnceWhenYouUseTheItem(t *testing.T) {
	tips := play(newEngine(nil), settings(dota.Carry), 300, 305, func(s *gsi.State) {
		s.Hero.HealthPercent, s.Hero.Health = 18, 108
		if s.Map.ClockTime < 302 {
			s.Items["slot2"] = gsi.Item{Name: "item_faerie_fire", CanCast: true}
		} else {
			s.Hero.HealthPercent, s.Hero.Health = 22, 132 // used it, still low
		}
	})
	var low []Tip
	for _, tip := range tips {
		if strings.HasPrefix(tip.Rule, "low_hp") {
			low = append(low, tip)
		}
	}
	if len(low) != 1 {
		t.Fatalf("want one low HP warning, got %+v", low)
	}
}

func TestNoLowHPWarningAtZeroHealth(t *testing.T) {
	got := byRule(play(newEngine(nil), settings(dota.Carry), 300, 301, func(s *gsi.State) {
		s.Hero.HealthPercent, s.Hero.Health = 0, 2 // burst down; Dota says dead a moment later
	}), "low_hp")
	if len(got) != 0 {
		t.Fatalf("too late to back off: %+v", got)
	}
}

func TestEmptyReasonLeavesNoBrackets(t *testing.T) {
	got := byRule(play(newEngine(nil), settings(dota.Mid), -60, -58, nil), "role_check")
	if len(got) != 1 || strings.Contains(got[0].Text, "()") || !strings.HasPrefix(got[0].Text, "Coaching you as mid. ") {
		t.Fatalf("role check = %+v", got)
	}
}

func TestRoshanEventTimers(t *testing.T) {
	tips := play(newEngine(nil), settings(dota.Carry), 1200, 1700, func(s *gsi.State) {
		if s.Map.ClockTime >= 1205 {
			s.Events = []gsi.Event{{GameTime: 1205 + 90, EventType: "roshan_killed", KilledByTeam: "dire"}}
		}
	})
	killed := byRule(tips, "roshan")
	if len(killed) != 1 || !strings.Contains(killed[0].Text, "Roshan killed by the enemy team at 20:05. Respawns between 28:05 and 31:05") {
		t.Fatalf("kill tip = %+v", killed)
	}
	window := byRule(tips, "roshan_window")
	if len(window) != 1 || window[0].Clock != 1685 {
		t.Fatalf("window tip = %+v", window)
	}
}

func TestIdleNeedsNoMovementAndNoFarm(t *testing.T) {
	farming := play(newEngine(nil), settings(dota.Carry), 700, 800, func(s *gsi.State) {
		s.Player.LastHits = s.Map.ClockTime / 10
	})
	if got := byRule(farming, "idle"); len(got) != 0 {
		t.Fatalf("farming in place should not count as idle: %+v", got)
	}
	got := byRule(play(newEngine(nil), settings(dota.Carry), 700, 800, nil), "idle")
	if len(got) != 2 || got[0].Clock != 730 || got[1].Clock != 790 {
		t.Fatalf("want idle at 730 and 790, got %+v", got)
	}
}

func TestMatchSummaryOnPostGame(t *testing.T) {
	e := newEngine(fakeData{})
	set := settings(dota.Carry)
	play(e, set, 590, 700, func(s *gsi.State) {
		s.Player.LastHits = 40
		if s.Map.ClockTime >= 650 {
			s.Hero.Alive = false
			s.Player.Deaths = 1
		}
	})
	end := state(701)
	end.Map.GameState, end.Map.WinTeam = gsi.StatePostGame, "radiant"
	res := e.Update(end, set)
	sum := res.Finished
	if sum == nil {
		t.Fatal("expected a match summary")
	}
	if sum.Result != "win" || sum.Hero != "Anti-Mage" || sum.LastHitsAt["10:00"] != 40 ||
		len(sum.DeathClocks) != 1 || sum.DeathClocks[0] != 650 || sum.TipCounts["death"] != 1 || sum.TipCounts["farm_pace"] != 1 {
		t.Fatalf("unexpected summary: %+v", sum)
	}
	if again := e.Update(end, set); again.Finished != nil {
		t.Fatal("summary emitted twice")
	}
}

func TestRoleCheckBeforeHorn(t *testing.T) {
	got := byRule(play(newEngine(nil), settings(dota.SoftSupport), -40, 30, nil), "role_check")
	if len(got) != 1 || got[0].Clock != -40 || !strings.Contains(got[0].Text, "soft support") {
		t.Fatalf("role check = %+v", got)
	}
	if got := byRule(play(newEngine(nil), settings(dota.Carry), 10, 30, nil), "role_check"); len(got) != 0 {
		t.Fatalf("no role check after the horn, got %+v", got)
	}
}

func TestFocusAtHorn(t *testing.T) {
	e := newEngine(nil)
	e.SetFocus("Carry a TP scroll")
	got := byRule(play(e, settings(dota.Carry), -5, 60, nil), "focus")
	if len(got) != 1 || got[0].Clock != 0 || got[0].Text != "Focus this game: Carry a TP scroll" {
		t.Fatalf("focus tips = %+v", got)
	}
}

func TestExpireRecordsAbandonedMatch(t *testing.T) {
	e := newEngine(fakeData{})
	now := time.Now()
	e.now = func() time.Time { return now }
	play(e, settings(dota.Carry), 590, 700, nil)
	if e.Expire(3*time.Minute) != nil {
		t.Fatal("expired while updates are fresh")
	}
	now = now.Add(4 * time.Minute)
	sum := e.Expire(3 * time.Minute)
	if sum == nil || sum.Result != "unknown" || sum.DurationSec != 700 {
		t.Fatalf("summary = %+v", sum)
	}
	if e.Expire(3*time.Minute) != nil {
		t.Fatal("expired twice")
	}
}

func TestNextPeriodic(t *testing.T) {
	cases := []struct{ clock, first, period, want int }{
		{-15, 0, 180, 0}, {1, 0, 180, 180}, {180, 0, 180, 180}, {100, 360, 120, 360}, {361, 360, 120, 480},
	}
	for _, c := range cases {
		if got, ok := nextPeriodic(c.clock, c.first, c.period); !ok || got != c.want {
			t.Errorf("nextPeriodic(%d, %d, %d) = %d, want %d", c.clock, c.first, c.period, got, c.want)
		}
	}
	if _, ok := nextPeriodic(500, 360, 0); ok {
		t.Error("period 0 past first spawn should report no next spawn")
	}
}

func TestExpectedLastHitsInterpolates(t *testing.T) {
	if got, _ := expectedLastHits(RoleTargets(dota.Carry).LastHits, 450); got != 47 {
		t.Fatalf("expected 47 at 7:30 for carry, got %d", got)
	}
	if _, ok := expectedLastHits(RoleTargets(dota.HardSupport).LastHits, 450); ok {
		t.Fatal("supports have no last-hit pace")
	}
}

func TestLobbyGamesAreRecordedAsPractice(t *testing.T) {
	e := newEngine(fakeData{})
	set := settings(dota.Carry)
	var res Result
	for clock := 290; clock <= 320; clock++ {
		s := state(clock)
		s.Map.MatchID = "0"
		res = e.Update(s, set)
	}
	if !strings.HasPrefix(res.MatchID, LocalMatchPrefix) {
		t.Fatalf("match id = %q", res.MatchID)
	}
	if snap := e.Snapshot(set); snap.MatchID != res.MatchID {
		t.Fatalf("snapshot match id %q, want %q", snap.MatchID, res.MatchID)
	}
	end := state(321)
	end.Map.MatchID, end.Map.GameState, end.Map.WinTeam = "0", gsi.StatePostGame, "dire"
	sum := e.Update(end, set).Finished
	if sum == nil || sum.MatchID != res.MatchID || sum.Source != model.SourcePractice || sum.Real() || sum.Result != "loss" {
		t.Fatalf("summary = %+v", sum)
	}
}

func TestRoleCheckSaysWhereTheRoleCameFrom(t *testing.T) {
	e := newEngine(nil)
	e.SetRoleNote("your usual role on Lion")
	got := byRule(play(e, settings(dota.SoftSupport), -40, -39, nil), "role_check")
	if len(got) != 1 || !strings.Contains(got[0].Text, "soft support (your usual role on Lion)") {
		t.Fatalf("role check = %+v", got)
	}
}
