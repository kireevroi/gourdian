package hud

import (
	"fmt"
	"strconv"
	"strings"
)

// words puts the HUD's own lines into the player's language, keyed by the English wording.
// Alerts arrive already worded by the coach.
type words map[string]string

func wordsFor(lang string) words {
	if lang == "ru" {
		return russian
	}
	return nil
}

func (w words) s(text string) string {
	if t, ok := w[text]; ok {
		return t
	}
	return text
}

func (w words) f(format string, args ...any) string { return fmt.Sprintf(w.s(format), args...) }

// timer names a timer from the coach, whose labels are English.
func (w words) timer(label string) string {
	if n, err := strconv.Atoi(strings.TrimPrefix(label, "Neutral tier ")); err == nil {
		return w.f("Neutral tier %d", n)
	}
	return w.s(label)
}

var russian = words{
	"carry": "керри", "mid": "мид", "offlane": "оффлейн", "soft support": "саппорт 4", "hard support": "хардсаппорт",

	"Position %d · %s":            "Позиция %d · %s",
	" · Ctrl+Shift+1–5 to change": " · Ctrl+Shift+1–5 чтобы сменить",
	"Drill: %s · %d this game":    "Тренировка: %s · %d за игру",
	"Focus: %s":                   "Фокус: %s",
	"Coach: %s":                   "Тренер: %s",
	"Your best %s heroes:":        "Ваши лучшие герои (%s):",
	"%s · %d%% of %d":             "%s · %d%% из %d",
	"Avoid %s · %d%% of %d":       "Избегайте %s · %d%% из %d",

	"first game on this hero and position": "первая игра на этом герое и позиции",
	"%d games, %d%% won":                   "%d игр, %d%% побед",
	" (usual %d)":                          " (обычно %d)",
	"Aim for %d last hits at 10:00%s":      "Цель — %d добиваний к 10:00%s",
	"%s by %s":                             "%s к %s",
	"Goal: %s · %d/%d this week":           "Цель: %s · %d/%d за неделю",

	"Dead · respawn in %ds · %d gold, shop now": "Мертвы · возрождение через %d с · %d золота, купите сейчас",
	"Last hits %d · pace %d (%+d)":              "Добивания %d · темп %d (%+d)",
	"Buy now: %s (%dg)":                         "Купите сейчас: %s (%d з.)",
	"Next item: %s · %dg to go":                 "Следующий предмет: %s · ещё %d з.",
	"Skill: %s · %s":                            "Навык: %s · %s",
	"%s, %d won pro games":                      "%s, побед про: %d",
	"your %d games +10%%":                       "ваши матчи: %d, +10%%",
	"%s is late (goal %s) · buy it now":         "%s опаздывает (цель %s) · купите сейчас",
	"%s by %s · buy it now":                     "%s к %s · купите сейчас",
	"%s is late (goal %s) · %dg to go":          "%s опаздывает (цель %s) · ещё %d з.",
	"%s by %s · %dg to go":                      "%s к %s · ещё %d з.",
	"%d/%d/%d · %d GPM · %d/%d LH":              "%d/%d/%d · %d GPM · %d/%d доб.",

	"Aegis": "Аегис", "Aegis expires": "Аегис истекает", "Your Aegis expires": "Ваш Аегис истекает",
	"Aegis on your team expires": "Аегис вашей команды истекает", "Aegis on the enemy team expires": "Аегис врага истекает",
	"Bounty runes": "Руны богатства", "Neutral tier %d": "Нейтральные предметы %d-го тира",
	"Power rune": "Руна силы", "Roshan surely up": "Рошан точно жив", "Roshan window opens": "Открывается окно Рошана",
	"Stack pull": "Стак", "Tormentor": "Торментор", "Water runes": "Водяные руны", "Shrines of Wisdom": "Святыни мудрости",

	"Stack the ancient camp at 0:53": "Стакните лагерь древних в 0:53",
	"No TP scroll":                   "Нет свитка телепорта",
	"Carry a TP scroll at all times": "Всегда носите свиток телепорта",
	"your pick":                      "ваш выбор",
}
