// Package hud decides what the in-game HUD shows, from the player's widget settings. The
// overlay draws it and the dashboard previews it, so both always agree.
package hud

import (
	"slices"
	"strings"
	"time"

	"gourdian/internal/coach"
	"gourdian/internal/config"
	"gourdian/internal/dota"
	"gourdian/internal/model"
)

// Line kinds, which pick the colour.
const (
	KindText   = "text"
	KindMuted  = "muted"
	KindGood   = "good"
	KindInfo   = "info"
	KindWarn   = "warn"
	KindUrgent = "urgent"
	KindCoach  = "coach"
)

type Line struct {
	Text string `json:"text"`
	Kind string `json:"kind"`
}

// View is the HUD content: an alert in large type, then one row per widget line.
type View struct {
	Alert *Line `json:"alert,omitempty"`
	// More counts the other alerts still waiting behind the one shown.
	More int    `json:"more,omitempty"`
	Rows []Line `json:"rows,omitempty"`
}

// Queue remembers which alert is on screen, since when, and which have had their turn, so
// alerts take turns in the order they came. Urgent ones cut in. The zero value is ready.
type Queue struct {
	key   string
	since time.Time
	done  map[string]bool
}

func (q *Queue) show(t coach.Tip, now time.Time) { q.key, q.since = alertKey(t), now }

// turn is how long an alert stays while others wait; alone, it stays until it expires.
func turn(t coach.Tip) time.Duration {
	if t.Category == "ai" {
		return 8 * time.Second
	}
	return 5 * time.Second
}

func (v View) Empty() bool { return v.Alert == nil && len(v.Rows) == 0 }

// Payload is published to the overlay and the dashboard: the live view, and a sample with
// every enabled widget for editing the layout outside a match.
type Payload struct {
	Live   View `json:"live"`
	Sample View `json:"sample"`
}

// PositionUntil is when the HUD stops offering the position hotkeys; lane detection decides then.
const PositionUntil = 150

var severityRank = map[string]int{"info": 0, "warn": 1, "urgent": 2}

// Build returns what the HUD shows now. Outside a match it's empty.
func Build(snap coach.Snapshot, tips []coach.Tip, widgets []config.HUDWidget, now time.Time) View {
	return BuildHeld(snap, tips, widgets, now, nil, "en")
}

// BuildHeld is Build for a HUD that is redrawn continuously; the queue carries which alert is
// shown from one frame to the next. The HUD's own lines are in lang; alerts come worded.
func BuildHeld(snap coach.Snapshot, tips []coach.Tip, widgets []config.HUDWidget, now time.Time, queue *Queue, lang string) View {
	var v View
	l := wordsFor(lang)
	if !snap.InMatch {
		return v
	}
	for _, w := range widgets {
		if !w.On {
			continue
		}
		switch w.ID {
		case config.WidgetAlerts:
			v.Alert, v.More = alert(tips, w, now, queue, l)
		case config.WidgetPosition:
			if snap.Clock < PositionUntil && snap.Role != "" {
				line := l.f("Position %d · %s", dota.Position(snap.Role), l.role(snap.Role))
				if snap.RoleNote != "" {
					line += " (" + snap.RoleNote + ")"
				}
				v.Rows = append(v.Rows, Line{line + l.s(" · Ctrl+Shift+1–5 to change"), KindCoach})
			}
		case config.WidgetDrill:
			if d := snap.Drill; d != nil && snap.Clock > 0 {
				kind := KindGood
				if d.Count > 0 {
					kind = KindText
				}
				v.Rows = append(v.Rows, Line{l.f("Drill: %s · %d this game", d.Label, d.Count), kind})
			}
		case config.WidgetPicks:
			v.Rows = append(v.Rows, pickLines(snap.Picks, l)...)
		case config.WidgetBriefing:
			if snap.Clock < 0 {
				v.Rows = append(v.Rows, briefingLines(snap.Briefing, l)...)
			}
		case config.WidgetFocus:
			if snap.Focus != "" && (snap.Clock < 120 || dead(snap)) {
				v.Rows = append(v.Rows, Line{l.f("Focus: %s", snap.Focus), KindCoach})
			}
		case config.WidgetDeath:
			if dead(snap) && snap.Player != nil {
				v.Rows = append(v.Rows, deathLine(snap.Hero.RespawnSeconds, snap.Player.Gold, l))
			}
		case config.WidgetTimers:
			v.Rows = append(v.Rows, timerLines(snap.Timers, snap.Clock, w, l)...)
		case config.WidgetPace:
			if p := snap.Pace; p != nil && snap.Clock > 0 {
				line := paceLine(p.LastHits, p.Expected, l)
				if src := sourceShort(snap.Sources, "last_hits"); src != "" {
					line.Text += " · " + src
				}
				v.Rows = append(v.Rows, line)
			}
		case config.WidgetNextItem:
			for _, b := range snap.Build {
				if b.Next {
					gold := 0
					if snap.Player != nil {
						gold = snap.Player.Gold
					}
					line := itemLine(b.DName, b.Remaining, gold, l)
					if src := sourceShort(snap.Sources, "build"); src != "" {
						line.Text += " · " + src
					}
					v.Rows = append(v.Rows, line)
				}
			}
		case config.WidgetSkill:
			if sk := snap.Skill; sk != nil && sk.Points > 0 && sk.Next != "" {
				v.Rows = append(v.Rows, skillLine(sk.Next, sk.Source, l))
			}
		case config.WidgetItemGoal:
			gold := 0
			if snap.Player != nil {
				gold = snap.Player.Gold
			}
			if line, ok := itemGoalLine(snap.ItemGoals, snap.Clock, gold, l); ok {
				v.Rows = append(v.Rows, line)
			}
		case config.WidgetStats:
			if p := snap.Player; p != nil {
				v.Rows = append(v.Rows, statsLine(p.Kills, p.Deaths, p.Assists, p.GPM, p.LastHits, p.Denies, l))
			}
		}
	}
	return v
}

func dead(snap coach.Snapshot) bool { return snap.Hero != nil && !snap.Hero.Alive }

func alertTTL(t coach.Tip) time.Duration {
	switch {
	case t.Category == "ai":
		return 20 * time.Second
	case t.Severity == coach.Info:
		return 9 * time.Second
	default:
		return 14 * time.Second
	}
}

func urgent(t coach.Tip) bool { return t.Category != "ai" && t.Severity == coach.Urgent }

func alertKey(t coach.Tip) string { return t.Rule + "@" + t.At.Format(time.RFC3339Nano) }

// alert is the alert whose turn it is, of those still fresh that the widget's options allow,
// and how many are waiting after it. Without a queue it's the first in line.
func alert(tips []coach.Tip, w config.HUDWidget, now time.Time, q *Queue, l words) (*Line, int) {
	var live []coach.Tip
	for _, t := range tips {
		switch {
		case now.Sub(t.At) >= alertTTL(t):
		case t.Category == "ai" && !w.Coach:
		case t.Category != "ai" && t.Category != "system" && severityRank[string(t.Severity)] < severityRank[w.MinSeverity]:
		default:
			live = append(live, t)
		}
	}
	if q == nil {
		q = &Queue{}
	}
	slices.SortStableFunc(live, func(a, b coach.Tip) int { return a.At.Compare(b.At) })
	done := map[string]bool{}
	var cur *coach.Tip
	for i, t := range live {
		if k := alertKey(t); q.done[k] {
			done[k] = true
		} else if k == q.key {
			cur = &live[i]
		}
	}
	q.done = done
	var waiting []coach.Tip
	for _, t := range live {
		if k := alertKey(t); !done[k] && (cur == nil || k != q.key) {
			waiting = append(waiting, t)
		}
	}
	if len(waiting) > 0 {
		next := waiting[0]
		if i := slices.IndexFunc(waiting, urgent); i >= 0 {
			next = waiting[i]
		}
		switch {
		case cur == nil:
			q.show(next, now)
		case urgent(next) && !urgent(*cur):
			q.show(next, now)
		case now.Sub(q.since) >= turn(*cur):
			done[q.key] = true
			q.show(next, now)
		}
	}
	i := slices.IndexFunc(live, func(t coach.Tip) bool { return alertKey(t) == q.key && !done[q.key] })
	if i < 0 {
		*q = Queue{done: done}
		return nil, 0
	}
	shown := live[i]
	line := &Line{shown.Text, string(shown.Severity)}
	if shown.Category == "ai" {
		line = &Line{l.f("Coach: %s", shown.Text), KindCoach}
	}
	return line, len(live) - len(done) - 1
}

// pickLines are your own record for this position, shown while you are choosing a hero.
func pickLines(p *coach.PickHelp, l words) []Line {
	if p == nil {
		return nil
	}
	lines := []Line{{l.f("Your best %s heroes:", l.role(p.Role)), KindCoach}}
	for _, h := range p.Best {
		lines = append(lines, Line{l.f("%s · %d%% of %d", h.Hero, h.WinPct, h.Games), KindText})
	}
	for _, h := range p.Avoid {
		lines = append(lines, Line{l.f("Avoid %s · %d%% of %d", h.Hero, h.WinPct, h.Games), KindWarn})
	}
	return lines
}

// briefingLines sum up the plan before the horn: the record on this hero, the last-hit target,
// core item goals and the week's unfinished goals.
func briefingLines(b *coach.Briefing, l words) []Line {
	if b == nil {
		return nil
	}
	record := l.s("first game on this hero and position")
	if b.Games > 0 {
		record = l.f("%d games, %d%% won", b.Games, b.Wins*100/b.Games)
	}
	lines := []Line{{b.Hero + " · " + record, KindCoach}}
	if b.Target10 > 0 {
		usual := ""
		if b.Usual10 > 0 {
			usual = l.f(" (usual %d)", b.Usual10)
		}
		lines = append(lines, Line{l.f("Aim for %d last hits at 10:00%s", b.Target10, usual), KindText})
	}
	var items []string
	for _, it := range b.Items {
		items = append(items, l.f("%s by %s", it.Name, dota.Clock(it.By)))
	}
	if len(items) > 0 {
		lines = append(lines, Line{strings.Join(items, " · "), KindText})
	}
	for _, g := range b.Goals[:min(len(b.Goals), 2)] {
		lines = append(lines, Line{l.f("Goal: %s · %d/%d this week", g.Label, g.Met, model.GoalsDone), KindCoach})
	}
	return lines
}

func deathLine(respawn, gold int, l words) Line {
	return Line{l.f("Dead · respawn in %ds · %d gold, shop now", respawn, gold), KindUrgent}
}

func timerLines(timers []coach.Timer, clock int, w config.HUDWidget, l words) []Line {
	var out []Line
	for _, t := range timers {
		in := t.At - clock
		if len(out) == w.Count || w.Within > 0 && in > w.Within {
			break
		}
		if len(w.Kinds) > 0 && !slices.Contains(w.Kinds, t.Kind) {
			continue
		}
		kind := KindText
		if in <= 20 {
			kind = KindWarn
		}
		out = append(out, Line{dota.Clock(in) + "  " + l.timer(t.Label), kind})
	}
	return out
}

func paceLine(lastHits, expected int, l words) Line {
	diff := lastHits - expected
	kind := KindGood
	if diff < 0 {
		kind = KindWarn
	}
	return Line{l.f("Last hits %d · pace %d (%+d)", lastHits, expected, diff), kind}
}

func itemLine(name string, remaining, gold int, l words) Line {
	if gold >= remaining {
		return Line{l.f("Buy now: %s (%dg)", name, remaining), KindGood}
	}
	return Line{l.f("Next item: %s · %dg to go", name, remaining-gold), KindText}
}

// itemGoalShowFor is how long before a core item's goal the HUD starts showing it.
const itemGoalShowFor = 300

// itemGoalLine shows the next core item goal once it's within five minutes, or late, with
// the gold still needed after what the player holds.
func itemGoalLine(goals []coach.ItemGoalView, clock, gold int, l words) (Line, bool) {
	for _, g := range goals {
		if g.Owned {
			continue
		}
		need := g.Remaining - gold
		switch left := g.By - clock; {
		case left < 0 && need <= 0:
			return Line{l.f("%s is late (goal %s) · buy it now", g.Name, dota.Clock(g.By)), KindWarn}, true
		case left < 0:
			return Line{l.f("%s is late (goal %s) · %dg to go", g.Name, dota.Clock(g.By), need), KindWarn}, true
		case left <= itemGoalShowFor && need <= 0:
			return Line{l.f("%s by %s · buy it now", g.Name, dota.Clock(g.By)), KindGood}, true
		case left <= itemGoalShowFor:
			return Line{l.f("%s by %s · %dg to go", g.Name, dota.Clock(g.By), need), KindText}, true
		}
		return Line{}, false
	}
	return Line{}, false
}

func sourceShort(sources []coach.Source, id string) string {
	for _, src := range sources {
		if src.ID == id {
			return src.Short
		}
	}
	return ""
}

func skillLine(next, source string, l words) Line {
	return Line{l.f("Skill: %s · %s", next, source), KindInfo}
}

func statsLine(k, d, a, gpm, lh, dn int, l words) Line {
	return Line{l.f("%d/%d/%d · %d GPM · %d/%d LH", k, d, a, gpm, lh, dn), KindMuted}
}

// Sample shows every enabled widget with made-up values, for placing and styling the HUD.
func Sample(widgets []config.HUDWidget) View { return SampleIn(widgets, "en") }

// SampleIn is Sample with the HUD's own lines in lang.
func SampleIn(widgets []config.HUDWidget, lang string) View {
	var v View
	l := wordsFor(lang)
	timers := []coach.Timer{
		{Label: "Night falls", At: 12, Kind: "daynight"},
		{Label: "Power rune", At: 18, Kind: "rune"},
		{Label: "Stack pull", At: 53, Kind: "stack"},
		{Label: "Neutral tier 2", At: 165, Kind: "neutral"},
		{Label: "Bounty runes", At: 180, Kind: "rune"},
		{Label: "Roshan window opens", At: 240, Kind: "objective"},
		{Label: "Shrines of Wisdom", At: 420, Kind: "rune"},
		{Label: "Tormentor", At: 900, Kind: "objective"},
	}
	for _, w := range widgets {
		if !w.On {
			continue
		}
		switch w.ID {
		case config.WidgetAlerts:
			v.Alert = &Line{l.s("Stack the ancient camp at 0:53"), KindInfo}
		case config.WidgetPosition:
			line := l.f("Position %d · %s", dota.Position(dota.Mid), l.role(dota.Mid)) + " (" + l.s("your pick") + ")" + l.s(" · Ctrl+Shift+1–5 to change")
			v.Rows = append(v.Rows, Line{line, KindCoach})
		case config.WidgetDrill:
			v.Rows = append(v.Rows, Line{l.f("Drill: %s · %d this game", l.s("No TP scroll"), 1), KindText})
		case config.WidgetPicks:
			v.Rows = append(v.Rows, pickLines(&coach.PickHelp{Role: dota.Mid,
				Best:  []coach.HeroRecord{{Hero: "Storm Spirit", Games: 11, Wins: 7, WinPct: 63}, {Hero: "Puck", Games: 8, Wins: 5, WinPct: 62}},
				Avoid: []coach.HeroRecord{{Hero: "Invoker", Games: 6, Wins: 2, WinPct: 33}}}, l)...)
		case config.WidgetBriefing:
			v.Rows = append(v.Rows, briefingLines(&coach.Briefing{Hero: "Shadow Fiend", Games: 12, Wins: 7, Target10: 66, Usual10: 60,
				Items: []coach.ItemGoal{{Name: "Black King Bar", By: 1170}}}, l)...)
		case config.WidgetFocus:
			v.Rows = append(v.Rows, Line{l.f("Focus: %s", l.s("Carry a TP scroll at all times")), KindCoach})
		case config.WidgetDeath:
			v.Rows = append(v.Rows, deathLine(24, 1250, l))
		case config.WidgetTimers:
			v.Rows = append(v.Rows, timerLines(timers, 0, w, l)...)
		case config.WidgetPace:
			line := paceLine(58, 60, l)
			line.Text += " · " + l.f("your %d games +10%%", 8)
			v.Rows = append(v.Rows, line)
		case config.WidgetNextItem:
			line := itemLine("Battle Fury", 1450, 800, l)
			line.Text += " · " + l.f("%s, %d won pro games", l.role(dota.Carry), 180)
			v.Rows = append(v.Rows, line)
		case config.WidgetSkill:
			v.Rows = append(v.Rows, skillLine("Ball Lightning", l.f("%s, %d won pro games", l.role(dota.Mid), 235), l))
		case config.WidgetItemGoal:
			v.Rows = append(v.Rows, Line{l.f("%s by %s · %dg to go", "Battle Fury", "15:00", 650), KindText})
		case config.WidgetStats:
			v.Rows = append(v.Rows, statsLine(3, 1, 5, 412, 58, 4, l))
		}
	}
	return v
}
