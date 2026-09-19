package hud

import (
	"gourdian/internal/i18n"
	"strconv"
	"strings"
)

// words puts the HUD's own lines into the player's language (i18n has the phrases). Alerts
// arrive already worded by the coach.
type words string

func wordsFor(lang string) words { return words(lang) }

func (w words) s(text string) string { return i18n.Word(string(w), text) }

func (w words) f(format string, args ...any) string { return i18n.Say(string(w), format, args...) }

// timer names a timer from the coach, whose labels are English.
func (w words) timer(label string) string {
	if n, err := strconv.Atoi(strings.TrimPrefix(label, "Neutral tier ")); err == nil {
		return w.f("Neutral tier %d", n)
	}
	return w.s(label)
}
