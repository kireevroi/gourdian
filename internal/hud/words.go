package hud

import (
	"strconv"
	"strings"

	"gourdian/internal/dota"
	"gourdian/internal/i18n"
)

// words puts the HUD's own lines into the player's language (i18n has the phrases). Alerts
// arrive already worded by the coach.
type words string

func wordsFor(lang string) words { return words(lang) }

func (w words) s(text string) string { return i18n.Word(string(w), text) }

func (w words) f(format string, args ...any) string { return i18n.Say(string(w), format, args...) }

// role names a position; dota holds the wording, so the HUD and the coach agree.
func (w words) role(role string) string { return dota.RoleName(role, string(w)) }

// timer names a timer from the coach, whose labels are English.
func (w words) timer(label string) string {
	if n, err := strconv.Atoi(strings.TrimPrefix(label, "Neutral tier ")); err == nil {
		return w.f("Neutral tier %d", n)
	}
	return w.s(label)
}
