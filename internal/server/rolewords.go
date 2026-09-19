package server

import (
	"fmt"

	"gourdian/internal/coach"
)

// roleWordsRU words the position messages in Russian, keyed by the English.
var roleWordsRU = map[string]string{
	"what you played on %s last time": "так вы играли на %s в прошлый раз",
	"your usual role on %s":           "ваша обычная позиция на %s",
	"a guess for %s":                  "предположение для %s",
	"your pick":                       "ваш выбор",
	"you laned %s":                    "вы стояли %s",
	"Coaching you as %s":              "Тренирую вас как %s",
	"Coaching you as %s.":             "Тренирую вас как %s.",
	"You're laning %s, so the coach switched you to %s. Another position? Press Ctrl+Shift+1 to 5": "Вы стоите %s, поэтому тренер переключил вас на позицию: %s. Другая позиция? Нажмите Ctrl+Shift+1–5",
	"You're %s. Coaching you as %s.": "Вы %s. Тренирую вас как %s.",
	"%s (from your %s game as %s)":   "%s (из вашей игры на %s, позиция: %s)",
	"%s (from your %s game)":         "%s (из вашей игры на %s)",
}

var lanesRU = map[string]string{coach.LaneSafe: "на лёгкой линии", coach.LaneMid: "на миде", coach.LaneOff: "на сложной линии"}

func roleSay(lang, format string, args ...any) string {
	if t, ok := roleWordsRU[format]; ok && lang == "ru" {
		format = t
	}
	return fmt.Sprintf(format, args...)
}

func laneIn(lang, lane string) string {
	if lang == "ru" {
		return lanesRU[lane]
	}
	return "in the " + lane
}
