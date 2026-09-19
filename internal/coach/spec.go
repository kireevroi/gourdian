package coach

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"gourdian/internal/config"
	"gourdian/internal/gsi"
)

// RuleSpec is a rule the player builds on the dashboard: when something happens, if the
// conditions hold, show and say an alert.
type RuleSpec struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Enabled  bool      `json:"enabled"`
	Category string    `json:"category"`
	Roles    []string  `json:"roles,omitempty"`  // empty means every position
	Heroes   []int     `json:"heroes,omitempty"` // empty means every hero
	When     Trigger   `json:"when"`
	Match    string    `json:"match"` // "all" or "any" of the conditions
	If       []Cond    `json:"if"`
	Then     AlertSpec `json:"then"`
	// Cooldown is the least game time between two alerts; Once allows one per match.
	Cooldown int  `json:"cooldown,omitempty"`
	Once     bool `json:"once,omitempty"`
	// Max caps how often a rule may fire in one match, so a habit you keep isn't nagged at
	// all game. 0 means no cap.
	Max int `json:"max,omitempty"`
}

// Trigger types.
const (
	WhenState    = "state"    // while the conditions hold (for For seconds)
	WhenChange   = "change"   // the moment the conditions become true
	WhenEvent    = "event"    // when an event happens and the conditions hold
	WhenSchedule = "schedule" // a little before repeating game times
	WhenAfter    = "after"    // a set time after something happened
)

type Trigger struct {
	Type  string `json:"type"`
	For   int    `json:"for,omitempty"`   // state: seconds the conditions must hold
	Event string `json:"event,omitempty"` // event: one of Events
	Arg   string `json:"arg,omitempty"`   // event: item for item_bought
	// After: seconds to wait after Event before the rule fires.
	After int `json:"after,omitempty"`
	// Schedule: game times First, First+Every, … up to Until (0 = no end), announced Lead seconds early.
	First int `json:"first,omitempty"`
	Every int `json:"every,omitempty"`
	Until int `json:"until,omitempty"`
	Lead  int `json:"lead,omitempty"`
}

// Condition operators.
var (
	NumberOps = []string{"lt", "le", "eq", "ne", "ge", "gt", "between"}
	BoolOps   = []string{"true", "false"}
	TextOps   = []string{"is", "is_not", "contains"}
)

type Cond struct {
	Field string  `json:"field"`
	Arg   string  `json:"arg,omitempty"`
	Op    string  `json:"op"`
	Num   float64 `json:"num,omitempty"`
	Num2  float64 `json:"num2,omitempty"` // upper bound for between
	Text  string  `json:"text,omitempty"`
}

type AlertSpec struct {
	Text     string `json:"text"`
	Speech   string `json:"speech,omitempty"` // empty says the text
	Silent   bool   `json:"silent,omitempty"`
	Severity string `json:"severity"`
	// Mistake counts the alert toward the habits on the dashboard and in reviews.
	Mistake bool   `json:"mistake,omitempty"`
	Advice  string `json:"advice,omitempty"`
	// Habit names the mistake in your statistics; empty uses the rule's name.
	Habit string `json:"habit,omitempty"`
}

// RuleOverride changes a built-in rule without replacing it. Spec is set when the player
// edited the rule itself; the shipped one is then only used by "Reset to default".
type RuleOverride struct {
	Roles    []string  `json:"roles,omitempty"`
	Severity string    `json:"severity,omitempty"`
	Voice    string    `json:"voice,omitempty"` // "speak", "silent" or "" for the default
	Spec     *RuleSpec `json:"spec,omitempty"`
}

var severities = []string{string(Info), string(Warn), string(Urgent)}

var Categories = []string{"timing", "survival", "economy", "items", "skills", "map", "focus"}

// Validate reports the first problem with a rule, worded for the dashboard.
func (r RuleSpec) Validate() error {
	switch {
	case strings.TrimSpace(r.Name) == "":
		return errors.New("give the rule a name")
	case !slices.Contains(Categories, r.Category):
		return fmt.Errorf("unknown category %q", r.Category)
	case strings.TrimSpace(r.Then.Text) == "":
		return errors.New("write the alert text")
	case !slices.Contains(severities, r.Then.Severity):
		return fmt.Errorf("unknown severity %q", r.Then.Severity)
	case r.Match != "all" && r.Match != "any":
		return errors.New(`conditions must match "all" or "any"`)
	case r.Cooldown < 0 || r.When.For < 0 || r.Max < 0:
		return errors.New("times can't be negative")
	}
	for _, role := range r.Roles {
		if !slices.Contains(config.Roles, role) {
			return fmt.Errorf("unknown position %q", role)
		}
	}
	switch r.When.Type {
	case WhenState:
		if !r.Once && r.Cooldown < 5 {
			return errors.New("a rule that fires while something is true needs a cooldown of at least 5 seconds, or once per match")
		}
	case WhenChange:
	case WhenEvent:
		if !slices.ContainsFunc(Events, func(e Event) bool { return e.ID == r.When.Event }) {
			return fmt.Errorf("unknown event %q", r.When.Event)
		}
	case WhenAfter:
		if !slices.ContainsFunc(Events, func(e Event) bool { return e.ID == r.When.Event }) {
			return fmt.Errorf("unknown event %q", r.When.Event)
		}
		if r.When.After < 0 {
			return errors.New("the wait after an event can't be negative")
		}
	case WhenSchedule:
		if r.When.Every < 0 || r.When.Lead < 0 || r.When.Every > 0 && r.When.Every < 30 {
			return errors.New("a schedule repeats every 30 seconds or more")
		}
	default:
		return fmt.Errorf("unknown trigger %q", r.When.Type)
	}
	for i, c := range r.If {
		f, ok := fieldIndex[c.Field]
		if !ok {
			return fmt.Errorf("condition %d: unknown value %q", i+1, c.Field)
		}
		if f.Arg != "" && c.Arg == "" {
			return fmt.Errorf("condition %d: pick the %s for %q", i+1, f.Arg, f.Label)
		}
		ops := map[string][]string{"number": NumberOps, "bool": BoolOps, "text": TextOps}[f.Type]
		if !slices.Contains(ops, c.Op) {
			return fmt.Errorf("condition %d: %q can't be compared with %q", i+1, f.Label, c.Op)
		}
	}
	for _, m := range templateField.FindAllStringSubmatch(r.Then.Text+" "+r.Then.Speech, -1) {
		if _, ok := fieldIndex[m[1]]; !ok && !slices.Contains(templateExtras, m[1]) {
			return fmt.Errorf("{%s} in the alert text isn't a value the rule knows", m[1])
		}
	}
	return nil
}

// compileSpec turns a player's rule into an engine rule.
func compileSpec(spec RuleSpec) Rule {
	if spec.If == nil {
		// The dashboard edits the conditions as a list; a JSON null would break it.
		spec.If = []Cond{}
	}
	r := Rule{ID: spec.ID, Category: spec.Category, Label: spec.Name, Roles: spec.Roles, Custom: true, Spec: &spec}
	if spec.Then.Mistake {
		r.Habit, r.Advice = spec.Then.Habit, spec.Then.Advice
		if r.Habit == "" {
			r.Habit = spec.Name
		}
		if r.Advice == "" {
			r.Advice = spec.Name
		}
	}
	r.eval = func(c *Ctx) { c.runSpec(&spec) }
	return r
}

func (c *Ctx) runSpec(spec *RuleSpec) {
	if len(spec.Heroes) > 0 && !slices.Contains(spec.Heroes, c.S.Hero.ID) {
		return
	}
	switch w := spec.When; w.Type {
	case WhenState:
		if c.held("cond", c.conditions(spec), w.For) {
			c.fireSpec(spec, "state", nil)
		}
	case WhenChange:
		now := c.conditions(spec)
		was := c.m.specTrue[spec.ID]
		c.m.specTrue[spec.ID] = now
		if now && !was && c.Prev != nil {
			c.fireSpec(spec, "change", nil)
		}
	case WhenEvent:
		if c.happened(w.Event, w.Arg) && c.conditions(spec) {
			c.fireSpec(spec, fmt.Sprintf("event@%d", c.Clock), nil)
		}
	case WhenAfter:
		at, ok := c.m.eventAt[w.Event]
		if !ok {
			return
		}
		// GSI updates about once a second, so a 10-second window can't be missed.
		if fire := at + w.After; c.Clock >= fire && c.Clock < fire+10 && c.conditions(spec) {
			c.fireSpec(spec, fmt.Sprintf("after@%d", fire), map[string]string{"at": clockStr(fire), "since": strconv.Itoa(c.Clock - at)})
		}
	case WhenSchedule:
		at, ok := nextPeriodic(c.Clock+w.Lead, w.First, w.Every)
		if w.Every == 0 {
			at, ok = w.First, true
		}
		if ok && (w.Until == 0 || at <= w.Until) && c.Clock >= at-w.Lead && c.Clock < at-w.Lead+10 && c.conditions(spec) {
			c.fireSpec(spec, fmt.Sprintf("at@%d", at), map[string]string{"at": clockStr(at), "in": strconv.Itoa(at - c.Clock)})
		}
	}
}

func (c *Ctx) conditions(spec *RuleSpec) bool {
	if len(spec.If) == 0 {
		return true
	}
	for _, cond := range spec.If {
		ok := c.check(cond)
		if spec.Match == "any" && ok {
			return true
		}
		if spec.Match != "any" && !ok {
			return false
		}
	}
	return spec.Match != "any"
}

func (c *Ctx) check(cond Cond) bool {
	f, ok := fieldIndex[cond.Field]
	if !ok {
		return false
	}
	switch f.Type {
	case "number":
		v := f.num(c, cond.Arg)
		switch cond.Op {
		case "lt":
			return v < cond.Num
		case "le":
			return v <= cond.Num
		case "eq":
			return v == cond.Num
		case "ne":
			return v != cond.Num
		case "ge":
			return v >= cond.Num
		case "gt":
			return v > cond.Num
		case "between":
			return v >= cond.Num && v <= cond.Num2
		}
	case "bool":
		return f.flag(c, cond.Arg) == (cond.Op == "true")
	case "text":
		v := strings.ToLower(f.text(c, cond.Arg))
		want := strings.ToLower(cond.Text)
		switch cond.Op {
		case "is":
			return v == want
		case "is_not":
			return v != want
		case "contains":
			return strings.Contains(v, want)
		}
	}
	return false
}

// fireSpec emits the rule's alert. Schedules fire once per time; other triggers follow the
// rule's cooldown or once-per-match setting.
func (c *Ctx) fireSpec(spec *RuleSpec, key string, extras map[string]string) {
	if spec.Max > 0 && c.m.ruleFires[spec.ID] >= spec.Max {
		return
	}
	cooldown := spec.Cooldown
	if spec.Once {
		cooldown, key = once, "once"
	}
	if spec.When.Type == WhenSchedule || spec.When.Type == WhenEvent || spec.When.Type == WhenAfter {
		cooldown = once
		if spec.Once {
			key = "once"
		}
	}
	text := c.render(spec.Then.Text, extras)
	speech := text
	if spec.Then.Speech != "" {
		speech = c.render(spec.Then.Speech, extras)
	}
	sev := Severity(spec.Then.Severity)
	if !c.emit(spec.Then.Mistake, key, cooldown, sev, text, speech) {
		return
	}
	if en := c.rule.english; en != nil {
		line := en.Speech
		if line == "" {
			line = en.Text
		}
		lang := c.Settings.Language
		c.Settings.Language = "en" // the English voice needs building and item words in English too
		c.out[len(c.out)-1].SpeechEN = c.render(line, extras)
		c.Settings.Language = lang
	}
	if spec.Then.Silent {
		c.out[len(c.out)-1].Quiet = true
	}
}

var (
	templateField  = regexp.MustCompile(`\{([a-z_]+)(?::([a-z0-9_]+))?\}`)
	templateExtras = []string{"item_name", "hero", "at", "in", "since"}
)

// emptyBrackets is what's left of "({field})" when the field has nothing to say.
var emptyBrackets = regexp.MustCompile(`\s*\(\s*\)`)

// render fills {field} and {field:arg} placeholders with current values.
func (c *Ctx) render(text string, extras map[string]string) string {
	return emptyBrackets.ReplaceAllString(c.fill(text, extras), "")
}

func (c *Ctx) fill(text string, extras map[string]string) string {
	return templateField.ReplaceAllStringFunc(text, func(m string) string {
		parts := templateField.FindStringSubmatch(m)
		name, arg := parts[1], parts[2]
		if v, ok := extras[name]; ok {
			return v
		}
		switch name {
		case "item_name":
			return c.itemName(arg)
		case "hero":
			if h := c.S.Hero; h != nil && c.data != nil {
				if info, ok := c.data.Hero(h.ID); ok {
					return info.LocalizedName
				}
			}
			return "your hero"
		}
		f, ok := fieldIndex[name]
		if !ok {
			return m
		}
		return formatField(c, f, arg)
	})
}

func formatField(c *Ctx, f *Field, arg string) string {
	switch f.Type {
	case "number":
		v := f.num(c, arg)
		if f.Unit == "clock" {
			return clockStr(int(v))
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case "bool":
		if f.flag(c, arg) {
			return "yes"
		}
		return "no"
	}
	return f.text(c, arg)
}

// CondResult is one condition checked against the current game, for the rule editor.
type CondResult struct {
	Value string `json:"value"`
	OK    bool   `json:"ok"`
}

// CheckSpec evaluates a rule's conditions against the latest game state.
func (e *Engine) CheckSpec(spec RuleSpec, set config.Settings) ([]CondResult, bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.last
	if !s.InMatch() || e.match == nil {
		return nil, false, errors.New("no match running: start a match or Demo Hero to check the conditions live")
	}
	c := e.newCtx(s, e.prev, set, e.now())
	out := make([]CondResult, len(spec.If))
	for i, cond := range spec.If {
		if f, ok := fieldIndex[cond.Field]; ok {
			out[i] = CondResult{Value: formatField(c, f, cond.Arg), OK: c.check(cond)}
		}
	}
	return out, c.conditions(&spec), nil
}

// TestSpec runs one rule over recorded game states and returns every alert it would have given.
func TestSpec(data Data, spec RuleSpec, set config.Settings, states func(yield func(*gsi.State) bool)) []Tip {
	e := New(data, discardLogger())
	e.rules = []Rule{compileSpec(spec)}
	var tips []Tip
	for s := range states {
		tips = append(tips, e.Update(s, set).Tips...)
		if len(tips) >= 200 {
			break
		}
	}
	return tips
}
