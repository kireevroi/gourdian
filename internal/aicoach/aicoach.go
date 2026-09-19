// Package aicoach writes the prompts for live suggestions and post-match reviews and reads
// the answers; package ai sends them to the provider the player chose.
package aicoach

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"gourdian/internal/ai"
	"gourdian/internal/coach"
	"gourdian/internal/config"
	"gourdian/internal/dota"
	"gourdian/internal/matchdata"
	"gourdian/internal/stats"
)

const liveSystemPrompt = `You are a Dota 2 coach speaking to one player during their match. Their goal is to climb in MMR. Your words appear on their screen and are read aloud, so each suggestion is one short, concrete instruction.

The request starts with the hero and position. Advise for that hero in that position: a core and a support on the same hero farm, buy and move differently.

Give zero, one or two suggestions for the next one to two minutes, most important first, each under 20 words. Your answer arrives a few seconds after the data was taken and stays up for about 20 seconds, so don't react to HP or to a fight happening right now.

Talk about this match. Each suggestion must rest on a fact in the data, such as a number, an item, an ability, a building, a clock time or an earlier death, and use it. Say what the trainer's automatic alerts don't: they already cover runes, stacks, TP scrolls, the stash, unspent gold, low HP, skill points, a low last-hit count, deaths, towers under attack and the Glyph, so never open with the death count or the last-hit gap. Look instead at priorities and trade-offs: which item to finish next and why, where to farm, when to join or avoid fights, when to push or defend, and how to use the hero's abilities given their cooldowns. Don't repeat your earlier advice from this match unless something changed. When there's nothing new worth saying, return no suggestions. Don't count or scold deaths.

Use only the facts you are given. The player sees only their own hero, so never invent enemy heroes, positions or items. Don't say the player has the Aegis unless the data says they carry it. When you name the parts of an item, use only the parts listed for it.

Items: you get the build professional players use on this hero, by game phase and in order of how often they buy it, from the games they won when there are enough of them, either in the player's position or, when professionals rarely play the hero there, in every position they play it in. Recommend only items from that build that suit the player's position: cores (positions 1 to 3) buy items that scale their farm and fights, supports (4 and 5) buy wards, detection and cheap items that help the team. When professionals rarely play this hero in the player's position, suggest what a professional of that position would buy instead. Never recommend an item a professional player of this hero wouldn't buy, such as a mobility item for a hero whose own abilities already give it mobility.

Skills: when you get the professional skill order, advice about which ability to level must follow it. Without it, don't say which ability to level.

If the player has a focus from their last match review, keep them on it when it's relevant. Use their match history to push on recurring weaknesses. No greetings, hedging or numbering.`

const liveSchema = `{"type":"object","properties":{"tips":{"type":"array","items":{"type":"string"},"minItems":0,"maxItems":2}},"required":["tips"],"additionalProperties":false}`

const reviewSystemPrompt = `You are a Dota 2 coach reviewing a match the player just finished. Their goal is to climb in MMR, so the review should change what they do in their next game.

You have the player's own stats, per-minute samples, the automatic warnings they triggered and their recent history, and the item build professional players use on this hero. Judge their items and play against how professionals play this hero in that position. The build says whether it comes from the player's position or from every position the hero is played in; only suggest items from it that suit the player's position. When the replay has been parsed you also get their lane, lane opponents and the heroes on both teams (names only), their percentiles against other players of the same hero, and their item timings. Don't guess how other players performed. Be direct and specific, and point at numbers from the data.

Write:
- followed_focus: when you are given the focus from the last review, say in one sentence whether they did it this game and what in the data shows it. Leave it empty when there was no focus.
- summary: two sentences on how the match went and the main reason for the result.
- strengths: up to two things they did well.
- improve: exactly three improvements, each an action with a measurable target for the next game (for example "Reach 55 last hits by 10:00; you had 38").
- next_game_focus: the single most important habit for the next game, under 12 words, phrased as an instruction. It must fit the position and hero this match was played on, because the trainer only shows it before another game in that position.
- goals: one to three goals for the rest of the week, each a metric from the list you're given, "at_least" or "at_most", a target, and a short label like "55 last hits at 10:00". A goal counts as done after five matches meeting it, so make each a realistic step up from this match and the player's history. Keep a goal from this week's list when it still matters.
Never use the "|" character.`

// reviewSchema lists the allowed goal metrics, so the answer can only use ones the trainer measures.
func reviewSchema(metrics []string) string {
	enum, _ := json.Marshal(metrics)
	return `{"type":"object","properties":{"summary":{"type":"string"},"strengths":{"type":"array","items":{"type":"string"},"maxItems":2},` +
		`"improve":{"type":"array","items":{"type":"string"},"minItems":3,"maxItems":3},"next_game_focus":{"type":"string"},` +
		`"followed_focus":{"type":"string"},` +
		`"goals":{"type":"array","minItems":1,"maxItems":3,"items":{"type":"object","properties":{"metric":{"type":"string","enum":` + string(enum) + `},` +
		`"comparator":{"type":"string","enum":["at_least","at_most"]},"target":{"type":"number"},"label":{"type":"string"}},` +
		`"required":["metric","comparator","target","label"],"additionalProperties":false}}},` +
		`"required":["summary","strengths","improve","next_game_focus","followed_focus","goals"],"additionalProperties":false}`
}

type History struct {
	Matches   int
	WinRate   float64
	AvgDeaths float64
	AvgGPM    float64
	AvgLH10   float64
	Habits    []string
}

// Context is what both prompts know about the player beyond the current match.
type Context struct {
	Profile string
	History History
	MMR     []stats.MMREntry
	Focus   string
	// Hero is what is known about the hero being played, when the trainer has it.
	Hero *HeroFacts
}

// HeroFacts ground the coach's item and play advice in how the hero is really played.
type HeroFacts struct {
	Name  string
	Roles []string // OpenDota's roles, like Carry, Escape, Nuker
	// Build is the professional item build by phase: start, early, mid, late, each in order
	// of how often professional players buy it.
	Build map[string][]string
	// BuildPosition is the position the build comes from, 1 to 5, or 0 for every position.
	BuildPosition int
	BuildGames    int
	BuildWon      bool     // from won games only
	Owned         []string // what the player carries now
}

type Input struct {
	Context
	Reason   string
	Role     string
	Snapshot coach.Snapshot
	Facts    coach.MatchFacts
	Tips     []coach.Tip
	Timeline []stats.Sample
}

type ReviewInput struct {
	Context
	Match    stats.MatchSummary
	Targets  map[string]int
	Timeline []stats.Sample
	Warnings []string
	Detail   *matchdata.Detail
	// Metrics are the goal metrics the review may use, with what each measures.
	Metrics   map[string]string
	WeekGoals []stats.GoalProgress
	// LastFocus is what the previous review told them to do, so this one can say whether
	// it happened before asking for anything new.
	LastFocus string
}

type Review struct {
	Summary       string     `json:"summary"`
	FollowedFocus string     `json:"followed_focus"`
	Strengths     []string   `json:"strengths"`
	Improve       []string   `json:"improve"`
	NextGameFocus string     `json:"next_game_focus"`
	Goals         []GoalSpec `json:"goals"`
}

type GoalSpec struct {
	Metric     string  `json:"metric"`
	Comparator string  `json:"comparator"`
	Target     float64 `json:"target"`
	Label      string  `json:"label"`
}

type writer struct{ strings.Builder }

func (w *writer) line(format string, args ...any) { fmt.Fprintf(w, format+"\n", args...) }

// heroFacts writes what is known about the hero, so advice follows how it is really played.
func (w *writer) heroFacts(h *HeroFacts) {
	if h == nil {
		return
	}
	if len(h.Roles) > 0 {
		w.line("%s's roles: %s.", h.Name, strings.Join(h.Roles, ", "))
	}
	source := sourceFor(h.BuildPosition, h.BuildGames, h.BuildWon)
	for _, phase := range []string{"start", "early", "mid", "late"} {
		if items := h.Build[phase]; len(items) > 0 {
			w.line("Professional %s-game items for %s %s, most bought first: %s.", phase, h.Name, source, strings.Join(items, ", "))
		}
	}
	if len(h.Owned) > 0 {
		w.line("The player has: %s.", strings.Join(h.Owned, ", "))
	}
}

func sourceFor(position, games int, won bool) string {
	kind := "professional games"
	if won {
		kind = "professional games they won"
	}
	if role := dota.RoleAt(position); role != "" {
		return fmt.Sprintf("as %s, from %d %s", positionName(role), games, kind)
	}
	if games > 0 {
		return fmt.Sprintf("from %d %s in every position", games, kind)
	}
	return "from up to 100 recent professional games in every position, won or lost"
}

// who opens every request, since the hero and position decide what good advice is.
func who(verb, hero, role string) string {
	switch {
	case hero != "" && role != "":
		return fmt.Sprintf("The player %s %s as %s.", verb, hero, positionName(role))
	case hero != "":
		return fmt.Sprintf("The player %s %s.", verb, hero)
	case role != "":
		return fmt.Sprintf("The player %s %s.", verb, positionName(role))
	}
	return ""
}

func Prompt(in Input) string {
	var w writer
	s := in.Snapshot
	hero := ""
	if in.Hero != nil {
		hero = in.Hero.Name
	} else if s.Hero != nil {
		hero = s.Hero.Name
	}
	if line := who("is playing", hero, in.Role); line != "" {
		w.line("%s", line)
	}
	w.line("Why you're being asked: %s", in.Reason)
	w.heroFacts(in.Hero)
	f := in.Facts
	w.line("Role: %s. Team: %s. Clock %s (%s), %s.", dota.RoleName(in.Role, "en"), s.Team, dota.Clock(s.Clock), dayNight(s.Daytime), stage(s.Clock))
	w.line("Kills: the player's team %d, the enemy %d.", f.TeamKills, f.EnemyKills)
	if len(f.LostBuildings) > 0 {
		w.line("The player's team has lost: %s.", strings.Join(f.LostBuildings, ", "))
	}
	if f.UnderAttack != "" {
		w.line("Being hit now: %s.", f.UnderAttack)
	}
	if h := s.Hero; h != nil {
		state := "alive"
		if !h.Alive {
			state = fmt.Sprintf("dead, respawning in %ds", h.RespawnSeconds)
		}
		w.line("Hero: %s, level %d, %s, HP %d%%, mana %d%%. Buyback %dg (cooldown %ds).",
			h.Name, h.Level, state, h.HealthPercent, h.ManaPercent, h.BuybackCost, h.BuybackCooldown)
	}
	if len(f.Abilities) > 0 {
		var abilities []string
		for _, a := range f.Abilities {
			abilities = append(abilities, abilityText(a))
		}
		w.line("Abilities: %s.", strings.Join(abilities, "; "))
	}
	if f.SkillPoints > 0 {
		w.line("Unspent skill points: %d.", f.SkillPoints)
	}
	if sk := s.Skill; sk != nil && len(sk.Order) > 0 {
		w.line("Professional skill order for %s %s, one ability per point: %s.", hero, sourceFor(sk.Position, sk.Games, sk.Won), strings.Join(sk.Order, ", "))
		if sk.Next != "" {
			w.line("By the player's current ability levels, the next ability in that order is %s.", sk.Next)
		}
	}
	if p := s.Player; p != nil {
		w.line("Score: %d/%d/%d. Last hits %d, denies %d. Gold %d (%d reliable), GPM %d, XPM %d.",
			p.Kills, p.Deaths, p.Assists, p.LastHits, p.Denies, p.Gold, p.GoldReliable, p.GPM, p.XPM)
	}
	if len(f.Deaths) > 0 {
		var at []string
		for _, d := range f.Deaths {
			at = append(at, dota.Clock(d))
		}
		w.line("Died at: %s.", strings.Join(at, ", "))
	}
	if p := s.Pace; p != nil {
		w.line("Last-hit pace: expected %d by now; target %d by %s.", p.Expected, p.Target, p.Checkpoint)
	}
	var inv, stash []string
	for _, it := range s.Items {
		if strings.HasPrefix(it.Slot, "stash") {
			stash = append(stash, it.DName)
		} else {
			inv = append(inv, it.DName)
		}
	}
	w.line("Items: %s.", listOr(inv, "none"))
	w.line("Stash: %s.", listOr(stash, "empty"))
	if f.Neutral != "" {
		w.line("Neutral item: %s.", f.Neutral)
	}
	if f.HasAegis {
		w.line("The player carries the Aegis.")
	} else {
		w.line("The player doesn't carry the Aegis.")
	}
	for _, p := range f.Parts {
		w.line("Toward %s the player has %s; missing %s; %dg to finish.", p.Item, strings.Join(p.Have, ", "), listOr(p.Missing, "nothing"), p.Left)
	}
	var next []string
	for _, it := range s.Build {
		if !it.Owned && !it.Skipped {
			next = append(next, fmt.Sprintf("%s (%dg to finish)", it.DName, it.Remaining))
		}
	}
	if len(next) > 0 {
		w.line("Build still to get, in order: %s.", strings.Join(next[:min(4, len(next))], ", "))
	}
	var timers []string
	for _, t := range s.Timers {
		timers = append(timers, fmt.Sprintf("%s in %s", t.Label, dota.Clock(t.At-s.Clock)))
	}
	if len(timers) > 0 {
		w.line("Upcoming: %s.", strings.Join(timers, "; "))
	}
	var alerts, advice []string
	for _, t := range in.Tips {
		line := fmt.Sprintf("[%s] %s", dota.Clock(t.Clock), t.Text)
		if t.Rule == "ai" {
			advice = append(advice, line)
		} else if s.Clock-t.Clock <= 300 {
			alerts = append(alerts, line)
		}
	}
	if len(alerts) > 0 {
		w.line("The trainer's alerts in the last 5 minutes, don't repeat them: %s", strings.Join(alerts[max(0, len(alerts)-10):], " | "))
	}
	if len(advice) > 0 {
		w.line("Your earlier advice this match, don't repeat it unless something changed: %s", strings.Join(advice[max(0, len(advice)-6):], " | "))
	}
	if n := len(in.Timeline); n > 1 {
		var parts []string
		for _, x := range in.Timeline[max(0, n-6):] {
			parts = append(parts, fmt.Sprintf("%s: %d LH, %d GPM, %d deaths", dota.Clock(x.Clock), x.LastHits, x.GPM, x.Deaths))
		}
		w.line("Recent minutes: %s.", strings.Join(parts, "; "))
	}
	writeContext(&w, in.Context)
	return w.String()
}

func ReviewPrompt(in ReviewInput) string {
	var w writer
	m := in.Match
	if line := who("played", m.Hero, m.Role); line != "" {
		w.line("%s", line)
	}
	w.heroFacts(in.Hero)
	w.line("Match: %s as %s (%s), %s after %s.", m.Hero, dota.RoleName(m.Role, "en"), m.Team, m.Result, dota.Clock(m.DurationSec))
	w.line("Final: %d/%d/%d, %d last hits, %d denies, %d GPM, %d XPM.", m.Kills, m.Deaths, m.Assists, m.LastHits, m.Denies, m.GPM, m.XPM)
	var checkpoints []string
	for _, key := range []string{"5:00", "10:00", "15:00", "20:00", "30:00"} {
		lh, ok := m.LastHitsAt[key]
		if !ok {
			continue
		}
		if target, ok := in.Targets[key]; ok {
			checkpoints = append(checkpoints, fmt.Sprintf("%s %d (target %d)", key, lh, target))
		} else {
			checkpoints = append(checkpoints, fmt.Sprintf("%s %d", key, lh))
		}
	}
	if len(checkpoints) > 0 {
		w.line("Last hits at checkpoints: %s.", strings.Join(checkpoints, ", "))
	}
	if len(m.DeathClocks) > 0 {
		var deaths []string
		for _, d := range m.DeathClocks {
			deaths = append(deaths, dota.Clock(d))
		}
		w.line("Died at: %s.", strings.Join(deaths, ", "))
	}
	if len(in.Warnings) > 0 {
		w.line("Automatic warnings triggered: %s.", strings.Join(in.Warnings, "; "))
	}
	if n := len(in.Timeline); n > 0 {
		var parts []string
		for i, x := range in.Timeline {
			if i%5 == 0 || i == n-1 {
				parts = append(parts, fmt.Sprintf("%s: %d LH, %d gold held, %d GPM, K/D/A %d/%d/%d, level %d",
					dota.Clock(x.Clock), x.LastHits, x.Gold, x.GPM, x.Kills, x.Deaths, x.Assists, x.Level))
			}
		}
		w.line("Timeline: %s.", strings.Join(parts, "; "))
	}
	writeDetail(&w, in.Detail)
	writeContext(&w, in.Context)
	writeGoals(&w, in)
	return w.String()
}

func writeGoals(w *writer, in ReviewInput) {
	if in.LastFocus != "" {
		w.line("The focus you gave them after their last game in this position: %q. Check it against this match.", in.LastFocus)
	}
	if len(in.WeekGoals) > 0 {
		var parts []string
		for _, g := range in.WeekGoals {
			parts = append(parts, fmt.Sprintf("%s: met in %d of %d matches since it was set", g.Label, g.Met, g.Tried))
		}
		w.line("This week's goals so far: %s.", strings.Join(parts, "; "))
	}
	ids := make([]string, 0, len(in.Metrics))
	for id := range in.Metrics {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var parts []string
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf("%s (%s)", id, in.Metrics[id]))
	}
	w.line("Goal metrics you can use: %s.", strings.Join(parts, ", "))
}

var percentileNames = []struct{ key, label string }{
	{"gold_per_min", "GPM"}, {"xp_per_min", "XPM"}, {"last_hits_per_min", "last hits"},
	{"hero_damage_per_min", "hero damage"}, {"tower_damage", "tower damage"}, {"kills_per_min", "kills"},
	{"deaths_per_min", "deaths (higher means dying more)"},
}

func writeDetail(w *writer, d *matchdata.Detail) {
	switch {
	case d == nil:
		return
	case !d.Parsed:
		w.line("OpenDota hadn't parsed the replay in time, so lane, percentile and item timing data are missing.")
		return
	}
	lane := d.LaneRoleName
	if len(d.LaneOpponents) > 0 {
		lane += " against " + strings.Join(d.LaneOpponents, " and ")
	}
	w.line("From the parsed replay: played %s. Allies: %s. Enemies: %s.", lane, strings.Join(d.Allies, ", "), strings.Join(d.Enemies, ", "))
	w.line("Net worth %d (#%d on the team), hero damage %d, tower damage %d, stuns %.0fs, teamfight participation %.0f%%.",
		d.NetWorth, d.NetWorthRank, d.HeroDamage, d.TowerDamage, d.Stuns, d.TeamfightParticipation*100)
	w.line("Observer wards placed %d, sentries %d, camps stacked %d.", d.ObsPlaced, d.SenPlaced, d.CampsStacked)
	var pct []string
	for _, p := range percentileNames {
		if v, ok := d.Percentiles[p.key]; ok {
			pct = append(pct, fmt.Sprintf("%s %.0f", p.label, v*100))
		}
	}
	if len(pct) > 0 {
		w.line("Percentiles against other players of this hero (0 lowest, 100 highest): %s.", strings.Join(pct, ", "))
	}
	if len(d.CoreItems) > 0 {
		var items []string
		for _, it := range d.CoreItems {
			items = append(items, fmt.Sprintf("%s %s", it.Name, dota.Clock(it.Time)))
		}
		w.line("Item timings: %s.", strings.Join(items, ", "))
	}
}

func writeContext(w *writer, c Context) {
	if c.Focus != "" {
		w.line("Focus from the player's last match review: %s", c.Focus)
	}
	if h := c.History; h.Matches > 0 {
		w.line("Player history over the last %d matches: win rate %.0f%%, %.1f deaths, %.0f GPM, %.0f last hits at 10:00.",
			h.Matches, h.WinRate*100, h.AvgDeaths, h.AvgGPM, h.AvgLH10)
		if len(h.Habits) > 0 {
			w.line("Recurring mistakes (per match): %s.", strings.Join(h.Habits, "; "))
		}
	}
	if n := len(c.MMR); n > 0 {
		var parts []string
		for _, e := range c.MMR[max(0, n-4):] {
			parts = append(parts, fmt.Sprintf("%d on %s", e.MMR, e.Date.Format("Jan 2")))
		}
		w.line("MMR log: %s.", strings.Join(parts, ", "))
	}
	if p := strings.TrimSpace(c.Profile); p != "" {
		w.line("What the player says about themselves: %s", p)
	}
}

// Suggest asks for one or two live tips.
func Suggest(ctx context.Context, p ai.Provider, choice config.AIChoice, set config.AISettings, lang, prompt string) ([]string, error) {
	var out struct {
		Tips []string `json:"tips"`
	}
	req := ai.Request{System: withInstructions(liveSystemPrompt, set, lang), Prompt: prompt, Schema: liveSchema, Model: choice.Model, Effort: choice.Effort}
	if err := complete(ctx, p, req, &out); err != nil {
		return nil, err
	}
	tips := slices.DeleteFunc(out.Tips, func(t string) bool { return strings.TrimSpace(t) == "" })
	return tips[:min(len(tips), 2)], nil
}

// RequestReview asks for a match review whose goals use only the given metrics.
func RequestReview(ctx context.Context, p ai.Provider, choice config.AIChoice, set config.AISettings, lang, prompt string, metrics []string) (Review, error) {
	var r Review
	req := ai.Request{System: withInstructions(reviewSystemPrompt, set, lang), Prompt: prompt, Schema: reviewSchema(metrics), Model: choice.Model, Effort: choice.Effort}
	if err := complete(ctx, p, req, &r); err != nil {
		return Review{}, err
	}
	if strings.TrimSpace(r.NextGameFocus) == "" || len(r.Improve) == 0 {
		return Review{}, errors.New("the coach returned an incomplete review")
	}
	r.Goals = slices.DeleteFunc(r.Goals, func(g GoalSpec) bool {
		return !slices.Contains(metrics, g.Metric) || g.Comparator != "at_least" && g.Comparator != "at_most" || strings.TrimSpace(g.Label) == ""
	})
	return r, nil
}

func complete(ctx context.Context, p ai.Provider, req ai.Request, out any) error {
	raw, err := p.Complete(ctx, req)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s answered in an unexpected shape: %w", p.Info().Name, err)
	}
	return nil
}

func withInstructions(system string, set config.AISettings, lang string) string {
	if lang == "ru" {
		// Hero, item and ability names stay English, the way the game shows them.
		system += "\n\nAnswer in Russian. Keep hero, item and ability names in English."
	}
	if extra := strings.TrimSpace(set.Instructions); extra != "" {
		return system + "\n\nThe player's instructions for you: " + extra
	}
	return system
}

func stage(sec int) string {
	switch {
	case sec < 0:
		return "before the horn"
	case sec < 600:
		return "laning stage"
	case sec < 1500:
		return "mid game"
	}
	return "late game"
}

func abilityText(a coach.AbilityFact) string {
	text := fmt.Sprintf("%s level %d", a.Name, a.Level)
	switch {
	case a.Level == 0:
		text += ", not learned"
	case a.Passive:
		text += ", passive"
	case a.Cooldown > 0:
		text += fmt.Sprintf(", on cooldown %ds", a.Cooldown)
	case a.Ready:
		text += ", ready"
	}
	if a.Ultimate {
		text += ", ultimate"
	}
	return text
}

func positionName(role string) string {
	if n := dota.Position(role); n > 0 {
		return fmt.Sprintf("%s (position %d)", dota.RoleName(role, "en"), n)
	}
	return dota.RoleName(role, "en")
}

func dayNight(day bool) string {
	if day {
		return "day"
	}
	return "night"
}

func listOr(items []string, empty string) string {
	if len(items) == 0 {
		return empty
	}
	return strings.Join(items, ", ")
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
