package hud

import (
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"gourdian/internal/coach"
	"gourdian/internal/config"
	"gourdian/internal/dota"
	"gourdian/internal/gsi"
	"gourdian/internal/picks"
)

func widgets(mutate func([]config.HUDWidget)) []config.HUDWidget {
	w := config.DefaultWidgets()
	if mutate != nil {
		mutate(w)
	}
	return w
}

func find(w []config.HUDWidget, id string) *config.HUDWidget {
	for i := range w {
		if w[i].ID == id {
			return &w[i]
		}
	}
	return nil
}

func matchSnap() coach.Snapshot {
	return coach.Snapshot{
		InMatch: true, Clock: 300, Role: dota.Mid,
		Hero:   &coach.HeroView{ID: 1, Alive: true},
		Player: &gsi.Player{Gold: 900, Kills: 2, Deaths: 1, Assists: 3, GPM: 450, LastHits: 40, Denies: 6},
		Pace:   &coach.Pace{LastHits: 40, Expected: 45},
		Timers: []coach.Timer{
			{Label: "Stack pull", At: 353, Kind: "stack"},
			{Label: "Power rune", At: 360, Kind: "rune"},
			{Label: "Bounty runes", At: 540, Kind: "rune"},
			{Label: "Tormentor", At: 900, Kind: "objective"},
		},
	}
}

func TestBuildOrderAndOptions(t *testing.T) {
	now := time.Now()
	w := widgets(func(w []config.HUDWidget) {
		find(w, config.WidgetStats).On = true
		timers := find(w, config.WidgetTimers)
		timers.Count, timers.Kinds = 2, []string{"rune", "objective"}
	})
	v := Build(matchSnap(), nil, w, now)
	var got []string
	for _, r := range v.Rows {
		got = append(got, r.Text)
	}
	want := []string{"1:00  Power rune", "4:00  Bounty runes", "Last hits 40 · pace 45 (-5)", "2/1/3 · 450 GPM · 40/6 LH"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("rows:\n got %q\nwant %q", got, want)
	}
	if Build(coach.Snapshot{Clock: 300}, nil, w, now).Empty() == false {
		t.Fatal("nothing shows outside a match")
	}
}

func TestTimersWithinWindow(t *testing.T) {
	w := widgets(func(w []config.HUDWidget) { find(w, config.WidgetTimers).Within = 60 })
	v := Build(matchSnap(), nil, w, time.Now())
	if len(v.Rows) < 2 || v.Rows[0].Text != "0:53  Stack pull" || v.Rows[1].Text != "1:00  Power rune" || strings.Contains(v.Rows[2].Text, "Bounty") {
		t.Fatalf("rows = %+v", v.Rows)
	}
}

func TestAlertFiltersAndExpiry(t *testing.T) {
	now := time.Now()
	tips := []coach.Tip{
		{Text: "Buy a TP", Severity: coach.Warn, Category: "survival", At: now.Add(-2 * time.Second)},
		{Text: "Rune soon", Severity: coach.Info, Category: "timing", At: now.Add(-time.Second)},
	}
	if v := Build(matchSnap(), tips, widgets(nil), now); v.Alert == nil || v.Alert.Text != "Buy a TP" || v.More != 1 {
		t.Fatalf("the oldest alert goes first: %+v, more %d", v.Alert, v.More)
	}
	warnOnly := widgets(func(w []config.HUDWidget) { find(w, config.WidgetAlerts).MinSeverity = "warn" })
	if v := Build(matchSnap(), tips, warnOnly, now); v.Alert == nil || v.Alert.Text != "Buy a TP" {
		t.Fatalf("info tips should be skipped: %+v", v.Alert)
	}
	if v := Build(matchSnap(), tips, widgets(nil), now.Add(15*time.Second)); v.Alert != nil {
		t.Fatalf("tips expire: %+v", v.Alert)
	}
	ai := []coach.Tip{{Text: "Farm the jungle", Category: "ai", Severity: coach.Info, At: now}}
	noCoach := widgets(func(w []config.HUDWidget) { find(w, config.WidgetAlerts).Coach = false })
	if v := Build(matchSnap(), ai, noCoach, now); v.Alert != nil {
		t.Fatal("coach tips can be hidden")
	}
	if v := Build(matchSnap(), ai, widgets(nil), now); v.Alert == nil || v.Alert.Kind != KindCoach {
		t.Fatalf("coach tip = %+v", v.Alert)
	}
}

func TestPositionDeathAndItemRows(t *testing.T) {
	snap := matchSnap()
	snap.Clock, snap.RoleNote = 40, "a guess for Lion"
	snap.Hero.Alive, snap.Hero.RespawnSeconds = false, 12
	snap.Focus = "Stack every minute"
	snap.Build = []coach.BuildView{{Next: true, Remaining: 850}}
	snap.Build[0].DName = "Force Staff"
	snap.Timers = nil
	snap.Pace = nil
	v := Build(snap, nil, widgets(nil), time.Now())
	want := []string{
		"Position 2 · mid (a guess for Lion) · Ctrl+Shift+1–5 to change",
		"Focus: Stack every minute",
		"Dead · respawn in 12s · 900 gold, shop now",
		"Buy now: Force Staff (850g)",
	}
	if len(v.Rows) != len(want) {
		t.Fatalf("rows = %+v", v.Rows)
	}
	for i := range want {
		if v.Rows[i].Text != want[i] {
			t.Errorf("row %d = %q, want %q", i, v.Rows[i].Text, want[i])
		}
	}
}

func TestSampleShowsEnabledWidgets(t *testing.T) {
	w := widgets(func(w []config.HUDWidget) { find(w, config.WidgetDeath).On = false })
	v := Sample(w)
	if v.Alert == nil || len(v.Rows) != 18 {
		t.Fatalf("sample = %+v", v)
	}
	for _, r := range v.Rows {
		if strings.HasPrefix(r.Text, "Dead") {
			t.Fatal("disabled widgets don't show in the sample")
		}
	}
}

func TestItemGoalRow(t *testing.T) {
	goals := []coach.ItemGoalView{
		{ItemGoal: coach.ItemGoal{Item: "bfury", Name: "Battle Fury", By: 900}, Owned: true},
		{ItemGoal: coach.ItemGoal{Item: "manta", Name: "Manta Style", By: 1500}, Remaining: 2100},
	}
	if _, ok := itemGoalLine(goals, 1000, 0, wordsFor("en")); ok {
		t.Fatal("goals more than five minutes away stay hidden")
	}
	if l, ok := itemGoalLine(goals, 1300, 0, wordsFor("en")); !ok || l.Text != "Manta Style by 25:00 · 2100g to go" {
		t.Fatalf("line = %+v", l)
	}
	if l, _ := itemGoalLine(goals, 1560, 0, wordsFor("en")); l.Kind != KindWarn || l.Text != "Manta Style is late (goal 25:00) · 2100g to go" {
		t.Fatalf("late line = %+v", l)
	}
}

func TestAlertsTakeTurnsAndUrgentCutsIn(t *testing.T) {
	now := time.Now()
	at := func(sec int) time.Time { return now.Add(time.Duration(sec) * time.Second) }
	w := widgets(nil)
	var q Queue
	show := func(tips []coach.Tip, sec int, want string, more int) {
		t.Helper()
		v := BuildHeld(matchSnap(), tips, w, at(sec), &q, "en")
		got := ""
		if v.Alert != nil {
			got = v.Alert.Text
		}
		if got != want || v.More != more {
			t.Fatalf("at +%ds shown %q (+%d), want %q (+%d)", sec, got, v.More, want, more)
		}
	}
	runes := coach.Tip{Rule: "runes", Text: "Bounty runes in 15s", Severity: coach.Info, Category: "timing", At: at(0)}
	tp := coach.Tip{Rule: "no_tp", Text: "No TP scroll", Severity: coach.Warn, Category: "survival", At: at(1)}
	stack := coach.Tip{Rule: "stack", Text: "Stack a camp", Severity: coach.Info, Category: "timing", At: at(2)}
	show([]coach.Tip{runes}, 0, runes.Text, 0)
	show([]coach.Tip{runes}, 3, runes.Text, 0)
	// Newer alerts queue behind it, a warning included.
	tips := []coach.Tip{runes, tp, stack}
	show(tips, 3, runes.Text, 2)
	show(tips, 5, tp.Text, 1)
	// Something urgent cuts in, and the warning it interrupted gets its turn back.
	hp := coach.Tip{Rule: "low_hp", Text: "Low HP. Back off", Severity: coach.Urgent, Category: "survival", At: at(6)}
	tips = append(tips, hp)
	show(tips, 6, hp.Text, 2)
	show(tips, 10, hp.Text, 2)
	// The reminder went stale while waiting, so it's dropped instead of shown late.
	show(tips, 11, tp.Text, 0)
	show(tips, 14, tp.Text, 0)
	show(tips, 15, "", 0)
}

func TestHUDSpeaksRussian(t *testing.T) {
	cyrillic := regexp.MustCompile(`\p{Cyrillic}`)
	v := SampleIn(widgets(nil), "ru")
	for _, line := range append([]Line{*v.Alert}, v.Rows...) {
		if !cyrillic.MatchString(line.Text) && !strings.Contains(line.Text, "GPM") && !strings.Contains(line.Text, "%") {
			t.Errorf("an English line on the Russian HUD: %q", line.Text)
		}
	}
	snap := matchSnap()
	snap.Hero.Alive, snap.Hero.RespawnSeconds = false, 12
	live := BuildHeld(snap, nil, widgets(nil), time.Now(), nil, "ru")
	for _, line := range live.Rows {
		if strings.HasPrefix(line.Text, "Dead") || strings.Contains(line.Text, "Buy now") {
			t.Errorf("an English line on the Russian HUD: %q", line.Text)
		}
	}
}

func TestSkillRowOnlyWithAPointToSpend(t *testing.T) {
	w := widgets(nil)
	snap := matchSnap()
	snap.Skill = &coach.SkillView{Next: "Ball Lightning", Source: "mid, 300 pro games", Points: 0}
	for _, r := range Build(snap, nil, w, time.Now()).Rows {
		if strings.HasPrefix(r.Text, "Skill:") {
			t.Fatal("no skill row without a point to spend")
		}
	}
	snap.Skill.Points = 1
	var found bool
	for _, r := range BuildHeld(snap, nil, w, time.Now(), nil, "ru").Rows {
		found = found || r.Text == "Навык: Ball Lightning · mid, 300 pro games"
	}
	if !found {
		t.Fatal("the skill row names the ability and where the order comes from")
	}
}

// draftSnap is what the trainer knows while the player is still choosing a hero: no hero, no
// pace, no timers, but a board to pick from. Dota sends a player block here too, all zeroes.
func draftSnap() coach.Snapshot {
	return coach.Snapshot{
		InMatch: false, Clock: -75, Role: dota.Mid,
		Player: &gsi.Player{},
		Picks: &picks.Board{Role: dota.Mid,
			Best:  []picks.Hero{{Name: "Storm Spirit", Games: 11, Wins: 7, WinPct: 63, Why: []string{"your 11 games 63%", "meta 52% at Archon"}}},
			Fresh: []picks.Hero{{Name: "Death Prophet", Why: []string{"meta 54% at Archon"}}},
			Avoid: []picks.Hero{{Name: "Invoker", Games: 6, Wins: 2, WinPct: 33}}},
	}
}

// The HUD used to draw nothing outside a match, so the pick widget it carried could never
// appear: the draft is the one time it is worth anything.
func TestPickHelpShowsDuringTheDraft(t *testing.T) {
	v := BuildHeld(draftSnap(), nil, widgets(nil), time.Now(), nil, "en")
	want := []string{
		"Your best mid heroes:",
		"Storm Spirit · your 11 games 63% · meta 52% at Archon",
		"New to you: Death Prophet · meta 54% at Archon",
		"Avoid Invoker · 33% of 6",
	}
	var got []string
	for _, r := range v.Rows {
		got = append(got, r.Text)
	}
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("no row %q in %q", w, got)
		}
	}
}

// Nothing that needs a hero may show during the draft, least of all an all-zero score line.
// The stats widget is off by default, so it is turned on here to be checked at all.
func TestTheDraftHUDShowsNothingThatNeedsAHero(t *testing.T) {
	w := widgets(func(w []config.HUDWidget) { find(w, config.WidgetStats).On = true })
	v := BuildHeld(draftSnap(), nil, w, time.Now(), nil, "en")
	for _, r := range v.Rows {
		for _, bad := range []string{"0/0/0", "Last hits", "Next item", "Skill:", "Dead ·"} {
			if strings.Contains(r.Text, bad) {
				t.Errorf("row %q belongs to a match, not a draft", r.Text)
			}
		}
	}
}

// With no board and no match there is still nothing to draw.
func TestTheHUDIsEmptyOutsideAMatchAndADraft(t *testing.T) {
	snap := draftSnap()
	snap.Picks = nil
	if v := BuildHeld(snap, nil, widgets(nil), time.Now(), nil, "en"); v.Alert != nil || len(v.Rows) != 0 {
		t.Errorf("HUD = %+v", v)
	}
}
