package coach

import (
	"slices"
	"testing"

	"gourdian/internal/sys/config"
)

// Every rule that ships with the trainer needs Russian wording, or a Russian player gets
// English alerts in the middle of a match.
func TestEveryDefaultRuleHasRussianWording(t *testing.T) {
	for _, spec := range DefaultSpecs("en") {
		w, ok := russian[spec.ID]
		switch {
		case !ok:
			t.Errorf("%s: no Russian wording", spec.ID)
		case w.Name == "" || w.Text == "":
			t.Errorf("%s: Russian name or text missing", spec.ID)
		case spec.Then.Speech != "" && w.Speech == "":
			t.Errorf("%s: Russian speech missing", spec.ID)
		case spec.Then.Advice != "" && w.Advice == "":
			t.Errorf("%s: Russian advice missing", spec.ID)
		}
	}
	for _, spec := range DefaultSpecs("ru") {
		if err := spec.Validate(); err != nil {
			t.Errorf("%s in Russian: %v", spec.ID, err)
		}
	}
}

// The Russian text must show the same values as the English, or it quietly reports another number.
func TestRussianWordingUsesTheSameValues(t *testing.T) {
	fields := func(text string) map[string]bool {
		out := map[string]bool{}
		for _, m := range templateField.FindAllStringSubmatch(text, -1) {
			out[m[0]] = true
		}
		return out
	}
	for _, spec := range DefaultSpecs("en") {
		w, ok := russian[spec.ID]
		if !ok {
			continue
		}
		en, ru := fields(spec.Then.Text), fields(w.Text)
		for f := range en {
			if !ru[f] {
				t.Errorf("%s: the Russian text leaves out %s", spec.ID, f)
			}
		}
		for f := range ru {
			if !en[f] {
				t.Errorf("%s: the Russian text shows %s, which the English doesn't", spec.ID, f)
			}
		}
		for f := range fields(w.Speech) {
			if !en[f] && !fields(spec.Then.Speech)[f] {
				t.Errorf("%s: the Russian speech says %s, which the English doesn't", spec.ID, f)
			}
		}
	}
}

// config names the rules that ship switched off by id, and it can't import this package to
// check them. A typo there would silently ship the rule on.
func TestRulesThatShipOffAreRealRules(t *testing.T) {
	specs := DefaultSpecs("en")
	for _, id := range config.RulesShipOff {
		if !slices.ContainsFunc(specs, func(s RuleSpec) bool { return s.ID == id }) {
			t.Errorf("config.RulesShipOff names %q, which is not a built-in rule", id)
		}
	}
}
