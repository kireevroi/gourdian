// Package coach turns a stream of game states into timed, deduplicated tips.
package coach

import (
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"gourdian/internal/config"
	"gourdian/internal/dotadata"
	"gourdian/internal/gsi"
	"gourdian/internal/stats"
)

type Severity string

const (
	Info   Severity = "info"
	Warn   Severity = "warn"
	Urgent Severity = "urgent"
)

type Tip struct {
	Rule     string   `json:"rule"`
	Category string   `json:"category"`
	Severity Severity `json:"severity"`
	Text     string   `json:"text"`
	Speech   string   `json:"speech"`
	// SpeechEN is the same line in English, for when the computer has no voice for the
	// player's language. Empty when the rule was written by the player.
	SpeechEN string    `json:"-"`
	Habit    bool      `json:"habit"`
	Quiet    bool      `json:"quiet,omitempty"` // shown but not spoken
	Clock    int       `json:"clock"`
	At       time.Time `json:"at"`
}

type Rule struct {
	ID       string   `json:"id"`
	Category string   `json:"category"`
	Label    string   `json:"label"`
	Advice   string   `json:"advice,omitempty"`
	Habit    string   `json:"habit,omitempty"`
	Roles    []string `json:"roles,omitempty"`
	Custom   bool     `json:"custom,omitempty"`
	// Spec is set for rules made of When/If/Then cards, which the dashboard can edit.
	Spec *RuleSpec `json:"spec,omitempty"`
	// english is the shipped English alert of a built-in rule, for spoken fallback.
	english *AlertSpec
	eval    func(*Ctx)
}

func (r *Rule) appliesTo(role string) bool {
	return len(r.Roles) == 0 || slices.Contains(r.Roles, role)
}

type Data interface {
	Items() map[string]dotadata.ItemInfo
	Hero(id int) (dotadata.HeroInfo, bool)
	BuildFor(heroID int, role string) *dotadata.Build
	SkillBuildFor(heroID int, role string) *dotadata.SkillBuild
	AbilityName(name string) string
}

const (
	// A fight is no time for a reminder about the stash: while the hero is losing health
	// this fast, only warnings and urgent alerts are spoken.
	hurtDrop  = 8
	hurtQuiet = 6

	// skillAccept is how long an unspent point is warned about before it counts as normal.
	skillAccept      = 100
	skillSettle      = 2
	maxTips          = 50
	minRecordedClock = 300
)

type Engine struct {
	data      Data
	rules     []Rule // built-in rules, then the player's custom rules
	overrides map[string]RuleOverride
	custom    []RuleSpec
	lang      string
	targets   TargetSource
	log       *slog.Logger
	now       func() time.Time

	mu   sync.Mutex
	last *gsi.State
	// prev is the earlier state the last update compared with, for the rule editor's check.
	prev        *gsi.State
	lastSeen    time.Time
	lastInMatch time.Time
	match       *match
	tips        []Tip
	focus       string
	roleNote    string
}

func New(data Data, log *slog.Logger) *Engine {
	return &Engine{data: data, lang: "en", rules: allRules("en"), log: log, now: time.Now}
}

func (e *Engine) Rules() []Rule {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.rules)
}

// BuiltinRules lists the rules that ship with the trainer, worded in lang.
func BuiltinRules(lang string) []Rule { return allRules(lang) }

// SetCustomRules replaces the player's rules; disabled and invalid ones are left out.
func (e *Engine) SetCustomRules(specs []RuleSpec) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.custom = specs
	e.rebuild()
}

// rebuild compiles the built-in rules with the player's edits, then their own rules.
func (e *Engine) rebuild() {
	rules := allRules(e.lang)
	for i, r := range rules {
		o, ok := e.overrides[r.ID]
		if !ok || o.Spec == nil || r.Spec == nil {
			continue
		}
		spec := *o.Spec
		spec.ID, spec.Enabled = r.ID, true
		if spec.Validate() != nil {
			continue
		}
		edited := compileSpec(spec)
		edited.Custom = false
		rules[i] = edited
	}
	for _, spec := range e.custom {
		if spec.Enabled && spec.Validate() == nil {
			rules = append(rules, compileSpec(spec))
		}
	}
	e.rules = rules
}

// SetLanguage words the built-in rules, and so the HUD and voice, in lang.
func (e *Engine) SetLanguage(lang string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if lang == e.lang {
		return
	}
	e.lang = lang
	e.rebuild()
}

// SetTargetSource provides personal targets; without one the engine uses position defaults.
func (e *Engine) SetTargetSource(src TargetSource) {
	e.mu.Lock()
	e.targets = src
	e.mu.Unlock()
}

// targetsFor asks the target source, which may read the data file, so it's called without
// e.mu: the dashboard and HUD wait on that lock.
func (e *Engine) targetsFor(heroID int, role string) Targets {
	e.mu.Lock()
	src := e.targets
	e.mu.Unlock()
	if src == nil || heroID == 0 {
		return RoleTargets(role)
	}
	return src.TargetsFor(heroID, role)
}

// SetOverrides applies the player's changes to built-in rules.
func (e *Engine) SetOverrides(o map[string]RuleOverride) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.overrides = o
	e.rebuild()
}

// newCtx is what rules see for state s. The rule editor's live check builds it the same way,
// so it shows the values the rule really gets. The caller holds e.mu, and asked for the
// targets before taking it.
func (e *Engine) newCtx(s, prev *gsi.State, set config.Settings, targets Targets, now time.Time) *Ctx {
	return &Ctx{S: s, Prev: prev, Clock: s.Map.ClockTime, Settings: set, T: set.Timings, Focus: e.focus, RoleNote: e.roleNote,
		Targets: targets, data: e.data, m: e.match, now: now}
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type Result struct {
	// MatchID is the trainer's id for the current match: Dota's id, or local-<time> for
	// lobby and bot games, which Dota reports as match 0.
	MatchID string
	Tips    []Tip
	// DetectedRole is set once per match, at 2:30, when the hero clearly laned in a lane that
	// doesn't fit the current role; DetectedLane says which lane.
	DetectedRole string
	DetectedLane string
	Samples      []stats.Sample
	Finished     *stats.MatchSummary
	NewMatch     bool
}

func (e *Engine) Update(s *gsi.State, set config.Settings) Result {
	var targets Targets
	if s.InMatch() {
		targets = e.targetsFor(s.Hero.ID, set.Role)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	prev := e.last
	e.last, e.lastSeen = s, now

	var res Result
	if s.Map == nil {
		return res
	}
	if e.match != nil && e.match.gsiID != s.Map.MatchID {
		if !e.match.finished {
			res.Finished = e.finish("unknown")
		}
		e.match = nil
	}
	if s.Map.GameState == gsi.StatePostGame {
		if e.match != nil && !e.match.finished {
			res.Finished = e.finish(resultFor(e.match.team, s.Map.WinTeam))
		}
		return res
	}
	if !s.InMatch() {
		return res
	}
	if e.match == nil {
		e.match = newMatch(s.Map.MatchID, now)
		e.tips = nil
		res.NewMatch = true
	}
	e.lastInMatch = now
	m := e.match
	res.MatchID = m.id
	if !prev.InMatch() || prev.Map.MatchID != m.gsiID {
		prev = nil
	}
	e.prev = prev
	m.role = set.Role
	m.observe(s, prev, set.Timings)
	m.seeItems(heldNames(s), s.Map.ClockTime, !m.observed)
	m.observed = true
	m.lane.observe(s)
	if lane, ok := m.lane.decide(s.Map.ClockTime); ok {
		if role := roleForLane(lane, set.Role, m.lane.wards); role != "" && role != set.Role {
			res.DetectedRole, res.DetectedLane = role, lane
		}
	}
	if sample, ok := m.sample(s); ok {
		res.Samples = append(res.Samples, sample)
	}
	if s.Map.Paused {
		return res
	}

	c := e.newCtx(s, prev, set, targets, now)
	for i := range e.rules {
		r := &e.rules[i]
		o, hasOverride := e.overrides[r.ID]
		roles := r.Roles
		if hasOverride && o.Roles != nil {
			roles = o.Roles
		}
		if (len(roles) > 0 && !slices.Contains(roles, set.Role)) || !set.RuleEnabled(r.ID) {
			continue
		}
		c.override = nil
		if hasOverride {
			c.override = &o
		}
		before := len(c.out)
		e.run(c, r)
		if hasOverride {
			for j := before; j < len(c.out); j++ {
				if o.Severity != "" {
					c.out[j].Severity = Severity(o.Severity)
				}
				switch o.Voice {
				case "silent":
					c.out[j].Quiet = true
				case "speak":
					c.out[j].Quiet = false
				}
			}
		}
	}
	if set.QuietInFights && m.hurtAt > 0 && c.Clock-m.hurtAt <= hurtQuiet {
		for i := range c.out {
			if c.out[i].Severity == Info {
				c.out[i].Quiet = true
			}
		}
	}
	e.tips = append(e.tips, c.out...)
	if n := len(e.tips); n > maxTips {
		e.tips = slices.Clone(e.tips[n-maxTips:])
	}
	res.Tips = c.out
	return res
}

// SetRoleNote says where the current role came from, for the pre-game role line.
func (e *Engine) SetRoleNote(note string) {
	e.mu.Lock()
	e.roleNote = note
	e.mu.Unlock()
}

// HeroID is the hero of the match in progress, 0 when there isn't one.
func (e *Engine) HeroID() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.last == nil || e.last.Hero == nil {
		return 0
	}
	return e.last.Hero.ID
}

// SetFocus sets the habit from the last match review that the player is working on.
func (e *Engine) SetFocus(focus string) {
	e.mu.Lock()
	e.focus = focus
	e.mu.Unlock()
}

// Expire closes a match that stopped sending updates without reaching the post-game
// screen, such as when the player leaves early, so its stats still get recorded.
func (e *Engine) Expire(idle time.Duration) *stats.MatchSummary {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.match == nil || e.match.finished || e.now().Sub(e.lastInMatch) < idle {
		return nil
	}
	return e.finish("unknown")
}

func (e *Engine) run(c *Ctx, r *Rule) {
	defer func() {
		if p := recover(); p != nil {
			e.log.Error("rule panicked", "rule", r.ID, "panic", p)
		}
	}()
	c.rule = r
	r.eval(c)
}

// AddTips records tips produced outside the rules, such as AI suggestions.
func (e *Engine) AddTips(tips []Tip) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tips = append(e.tips, tips...)
	if n := len(e.tips); n > maxTips {
		e.tips = slices.Clone(e.tips[n-maxTips:])
	}
}

func (e *Engine) RecentTips() []Tip {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.tips)
}

func resultFor(team, winTeam string) string {
	switch {
	case team == "" || winTeam == "" || winTeam == "none":
		return "unknown"
	case team == winTeam:
		return "win"
	default:
		return "loss"
	}
}

func (e *Engine) finish(result string) *stats.MatchSummary {
	m := e.match
	m.finished = true
	s := m.last
	if s == nil || m.gsiID == "" || s.Map.ClockTime < minRecordedClock {
		return nil
	}
	simulated := strings.HasPrefix(m.id, "sim-")
	source := stats.SourceLive
	switch {
	case simulated:
		source = stats.SourceSim
	case strings.HasPrefix(m.id, LocalMatchPrefix):
		source = stats.SourcePractice
	}
	return &stats.MatchSummary{
		MatchID:     m.id,
		Source:      source,
		HeroID:      s.Hero.ID,
		Hero:        e.heroName(s.Hero),
		Role:        m.role,
		Team:        m.team,
		Result:      result,
		EndedAt:     e.now(),
		DurationSec: s.Map.ClockTime,
		Kills:       s.Player.Kills,
		Deaths:      s.Player.Deaths,
		Assists:     s.Player.Assists,
		LastHits:    s.Player.LastHits,
		Denies:      s.Player.Denies,
		GPM:         s.Player.GPM,
		XPM:         s.Player.XPM,
		LastHitsAt:  m.lhAt,
		DeathClocks: m.deaths,
		TipCounts:   m.counts,
		Simulated:   simulated,
		Items:       e.matchItems(m, s.Hero),
	}
}

// matchItems lists the core items bought during the match with when they first appeared.
func (e *Engine) matchItems(m *match, h *gsi.Hero) []stats.ItemTiming {
	if e.data == nil || e.data.Items() == nil {
		return nil
	}
	times := map[string]int{}
	for name, at := range m.itemSeen {
		if at >= 0 {
			times[name] = at
		}
	}
	var out []stats.ItemTiming
	for name, at := range dotadata.CoreItemTimes(times, e.data.Items(), 1500) {
		out = append(out, stats.ItemTiming{MatchID: m.id, Hero: e.heroName(h), Item: name, Time: at, Source: stats.SourceGSI})
	}
	slices.SortFunc(out, func(a, b stats.ItemTiming) int { return a.Time - b.Time })
	return out
}

func (e *Engine) heroName(h *gsi.Hero) string {
	if e.data != nil {
		if info, ok := e.data.Hero(h.ID); ok {
			return info.LocalizedName
		}
	}
	return strings.TrimPrefix(h.Name, "npc_dota_hero_")
}

type anchor struct {
	set      bool
	x, y     int
	clock    int
	progress int
}

// LocalMatchPrefix marks lobby and bot games, which have no Dota match id.
const LocalMatchPrefix = "local-"

type match struct {
	id       string
	gsiID    string
	role     string
	team     string
	last     *gsi.State
	finished bool

	fired  map[string]int
	counts map[string]int
	since  map[string]int
	events map[string]bool

	lhAt   map[string]int
	deaths []int

	specTrue  map[string]bool // custom "becomes true" rules: whether conditions held last update
	newEvents []string        // GSI event types first seen in this update
	eventAt   map[string]int  // clock at which each rule event last happened
	ruleFires map[string]int  // how often each rule has fired this match
	hurtAt    int             // clock when the hero last took a real chunk of damage
	buildings []buildingSample
	itemSeen  map[string]int // first game clock each item was held; -1 if already held when first seen
	observed  bool

	roshanKnown  bool
	roshanDeadAt int
	roshanTeam   string
	aegisKnown   bool
	aegisExpires int
	aegisTeam    string
	aegisMine    bool
	aegisUsed    bool // the player's own Aegis brought them back; a teammate's use can't be seen

	sampledMinute int

	skillGapSet  bool
	skillGap     int
	skillSeen    int
	skillSpareAt int
	skillLow     int
	skillLowSet  bool
	skillLowAt   int

	idle anchor
	lane laneTracker
}

func newMatch(gsiID string, now time.Time) *match {
	id := gsiID
	if gsiID == "0" {
		id = LocalMatchPrefix + now.Format("2006-01-02-150405")
	}
	return &match{
		id:     id,
		gsiID:  gsiID,
		fired:  map[string]int{},
		counts: map[string]int{},
		since:  map[string]int{},
		events: map[string]bool{}, eventAt: map[string]int{}, ruleFires: map[string]int{},
		lhAt: map[string]int{},

		specTrue: map[string]bool{},

		sampledMinute: -1,
	}
}

func (m *match) sample(s *gsi.State) (stats.Sample, bool) {
	clock := s.Map.ClockTime
	// Only near the top of a minute, so a trainer started mid-minute doesn't skew per-minute curves.
	if clock < 0 || clock/60 <= m.sampledMinute || clock%60 > 10 {
		return stats.Sample{}, false
	}
	m.sampledMinute = clock / 60
	p, h := s.Player, s.Hero
	return stats.Sample{
		MatchID: m.id, Clock: clock, Gold: p.Gold, GPM: p.GPM, XPM: p.XPM, LastHits: p.LastHits, Denies: p.Denies,
		Kills: p.Kills, Deaths: p.Deaths, Assists: p.Assists, Level: h.Level, Alive: h.Alive,
	}, true
}

// seeSkillPoints tracks unspent skill points against the fewest the hero has had this match.
// Innate and auto-levelled abilities make the raw count look wrong, so only a rise counts, and
// a rise the player never spends becomes the new normal instead of a warning every minute.
func (m *match) seeSkillPoints(s *gsi.State) {
	if s.Hero.Level == 0 || len(s.Abilities) == 0 {
		return
	}
	// Talents are not counted: they spend their own talent points, not skill points.
	spent := s.Hero.AttributesLevel
	for _, a := range s.Abilities {
		spent += a.Level
	}
	gap := SkillPointsAtLevel(s.Hero.Level) - spent
	clock := s.Map.ClockTime
	if gap >= m.skillGap || gap != m.skillLow {
		m.skillLowSet = false
	}
	switch {
	case !m.skillGapSet:
		m.skillGapSet, m.skillGap, m.skillSpareAt = true, gap, 0
	case gap < m.skillGap:
		// A level-up can show the new ability level one update before the new hero level.
		m.skillSpareAt = 0
		if !m.skillLowSet {
			m.skillLow, m.skillLowSet, m.skillLowAt = gap, true, clock
		} else if clock-m.skillLowAt >= skillSettle {
			m.skillGap, m.skillLowSet = gap, false
		}
	case gap > m.skillGap:
		if m.skillSpareAt == 0 {
			m.skillSpareAt = clock
		} else if clock-m.skillSpareAt >= skillAccept {
			m.skillGap, m.skillSpareAt = gap, 0
		}
	default:
		m.skillSpareAt = 0
	}
	m.skillSeen = gap
}

// skillSpare is how many points are unspent beyond the hero's usual gap.
func (m *match) skillSpare() int {
	if !m.skillGapSet {
		return 0
	}
	return max(m.skillSeen-m.skillGap, 0)
}

func (m *match) observe(s, prev *gsi.State, t config.Timings) {
	clock := s.Map.ClockTime
	m.last = s
	m.team = s.Player.TeamName
	for _, cp := range paceCheckpoints {
		key := clockStr(cp)
		if _, ok := m.lhAt[key]; !ok && clock >= cp && clock < cp+60 {
			m.lhAt[key] = s.Player.LastHits
		}
	}
	if prev != nil && reincarnated(prev, s) {
		m.aegisUsed = true
	}
	if prev != nil && died(prev, s) {
		m.deaths = append(m.deaths, clock)
	}
	if prev != nil && prev.Hero != nil && s.Hero.Alive && prev.Hero.HealthPercent-s.Hero.HealthPercent >= hurtDrop {
		m.hurtAt = clock
	}
	m.seeSkillPoints(s)
	m.seeBuildings(s, clock)
	m.seeIdle(s, clock)
	m.seeRuleEvents(s, prev, clock)
	// Event game_time is on the game_time axis, which is offset from the displayed clock.
	offset := s.Map.GameTime - clock
	m.newEvents = m.newEvents[:0]
	for _, ev := range s.Events {
		if m.events[ev.Key()] {
			continue
		}
		m.events[ev.Key()] = true
		m.newEvents = append(m.newEvents, ev.EventType)
		// Events carry their own game time, which is what rules wait from.
		m.eventAt[ev.EventType] = ev.GameTime - offset
		switch ev.EventType {
		case "roshan_killed":
			m.roshanKnown, m.roshanDeadAt, m.roshanTeam = true, ev.GameTime-offset, ev.KilledByTeam
		case "aegis_picked_up":
			m.aegisKnown, m.aegisExpires, m.aegisUsed = true, ev.GameTime-offset+t.AegisDuration, false
			m.aegisTeam = "radiant"
			if ev.PlayerID >= 5 {
				m.aegisTeam = "dire"
			}
			me, ok := s.Player.PlayerID()
			m.aegisMine = ok && me == ev.PlayerID
		}
	}
}

const once = -1

type Ctx struct {
	S        *gsi.State
	Prev     *gsi.State
	Clock    int
	Settings config.Settings
	T        config.Timings
	Focus    string
	RoleNote string
	Targets  Targets

	data     Data
	m        *match
	rule     *Rule
	override *RuleOverride
	now      time.Time
	out      []Tip
}

func (c *Ctx) remind(key string, cooldown int, sev Severity, text, speech string) bool {
	return c.emit(false, key, cooldown, sev, text, speech)
}

func (c *Ctx) mistake(key string, cooldown int, sev Severity, text, speech string) bool {
	return c.emit(true, key, cooldown, sev, text, speech)
}

func (c *Ctx) emit(habit bool, key string, cooldown int, sev Severity, text, speech string) bool {
	k := c.rule.ID + "/" + key
	if last, ok := c.m.fired[k]; ok && (cooldown == once || c.Clock-last < cooldown) {
		return false
	}
	c.m.fired[k] = c.Clock
	c.m.ruleFires[c.rule.ID]++
	if habit {
		c.m.counts[c.rule.ID]++
	}
	if speech == "" {
		speech = text
	}
	c.out = append(c.out, Tip{
		Rule: c.rule.ID, Category: c.rule.Category, Severity: sev,
		Text: text, Speech: speech, Habit: habit, Clock: c.Clock, At: c.now,
	})
	return true
}

// held uses the game clock, not wall time, so pauses don't count toward secs.
func (c *Ctx) held(key string, cond bool, secs int) bool {
	k := c.rule.ID + "/" + key
	if !cond {
		delete(c.m.since, k)
		return false
	}
	start, ok := c.m.since[k]
	if !ok {
		c.m.since[k], start = c.Clock, c.Clock
	}
	return c.Clock-start >= secs
}

func (c *Ctx) items() map[string]dotadata.ItemInfo {
	if c.data == nil {
		return nil
	}
	return c.data.Items()
}

func (c *Ctx) build() *dotadata.Build {
	if c.data == nil {
		return nil
	}
	return c.data.BuildFor(c.S.Hero.ID, c.Settings.Role)
}

func (c *Ctx) itemName(short string) string {
	if info, ok := c.items()[short]; ok && info.DName != "" {
		return info.DName
	}
	return strings.ReplaceAll(short, "_", " ")
}

func clockStr(sec int) string {
	sign := ""
	if sec < 0 {
		sign, sec = "-", -sec
	}
	return fmt.Sprintf("%s%d:%02d", sign, sec/60, sec%60)
}
