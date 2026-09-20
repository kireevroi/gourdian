// Package i18n holds the trainer's Russian that isn't rule wording (coach/defaults_ru.go) or
// dashboard text (web/i18n/ru.json): the phrases the server and HUD build, keyed by their
// English, and the glossary that keeps game terms worded the same everywhere.
package i18n

import "fmt"

// Word is text in lang: its Russian when lang is "ru" and there is one, else the text itself.
func Word(lang, text string) string {
	if t, ok := phrases[text]; ok && lang == "ru" {
		return t
	}
	return text
}

// Say words format in lang, then fills it in.
func Say(lang, format string, args ...any) string { return fmt.Sprintf(Word(lang, format), args...) }

// phrases are Russian for English phrases; a Russian phrase takes the same values in the same
// order as its English.
var phrases = map[string]string{
	// The HUD's own lines; alerts arrive already worded by the coach, and roles by dota.
	// The tips the trainer builds itself, before the horn and after the match.
	"aim for %d last hits at 10:00":        "цель — %d добиваний к 10:00",
	"Aim for %d last hits at ten minutes.": "Цель — %d добиваний к десятой минуте.",
	"%s by %d minutes.":                    "%s к %d-й минуте.",
	"goal: %s (%d/%d)":                     "цель: %s (%d/%d)",
	"This week's goal: %s.":                "Цель недели: %s.",
	"Three losses in a row. Take a proper break before you queue again":           "Три поражения подряд. Сделайте настоящий перерыв перед следующей игрой",
	"Two losses in a row. Take a 10-minute break before you queue again":          "Два поражения подряд. Сделайте перерыв на 10 минут перед следующей игрой",
	"Three of your last four games were losses. Take a break before the next one": "Три из последних четырёх игр — поражения. Сделайте перерыв перед следующей",
	"your 11 games 63%":  "ваших матчей: 11, 63%",
	"your 8 games 62%":   "ваших матчей: 8, 62%",
	"meta 54% at Archon": "в мете 54% (Archon)",
	"You're down %d MMR this session. Take a break before you queue again":                                         "За сессию вы потеряли %d MMR. Сделайте перерыв перед следующей игрой",
	"Straight back in after a losing run: play this one calm, mute anyone tilting you, and focus on your own farm": "Сразу после серии поражений: играйте спокойно, мьютите тех, кто вас заводит, и следите за своим фармом",
	"Play this one calm. Mute anyone tilting you.":                                                                 "Играйте спокойно. Мьютите тех, кто вас заводит.",
	"Weekly goal done: %s, met in %d matches":                                                                      "Цель недели выполнена: %s, засчитано матчей: %d",
	"Weekly goal done. %s":              "Цель недели выполнена. %s",
	"Goal met: %s (%d of %d this week)": "Цель достигнута: %s (%d из %d за неделю)",
	"Goal met. %d of %d this week.":     "Цель достигнута. %d из %d за неделю.",
	"Goal missed: %s (you had %g)":      "Цель не достигнута: %s (у вас %g)",

	"Position %d · %s":            "Позиция %d · %s",
	" · Ctrl+Shift+1–5 to change": " · Ctrl+Shift+1–5 чтобы сменить",
	"Drill: %s · %d this game":    "Тренировка: %s · %d за игру",
	"Focus: %s":                   "Фокус: %s",
	"Coach: %s":                   "Тренер: %s",
	"Your best %s heroes:":        "Ваши лучшие герои (%s):",
	"New to you: %s":              "Новый для вас: %s",
	"Against: %s":                 "Против: %s",
	"Ctrl+Shift+1–5 for your position, then hero advice": "Ctrl+Shift+1–5 — ваша позиция, потом советы по героям",
	"Nobody on your side can stun or hold":               "На вашей стороне некому дать контроль",
	"Best %s picks: %s":                                  "Лучшие пики (%s): %s",
	"Avoid %s":                                           "Избегайте %s",
	"Avoid %s · %d%% of %d":                              "Избегайте %s · %d%% из %d",

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
	"Stack pull": "Стак", "Tormentor": "Торментор", "Water runes": "Водяные руны", "Shrines of Wisdom": "Святилища мудрости",
	"Night falls": "Наступает ночь", "Day breaks": "Наступает день",

	"Stack the ancient camp at 0:53": "Стакните лагерь древних в 0:53",
	"No TP scroll":                   "Нет свитка телепорта",
	"Carry a TP scroll at all times": "Всегда носите свиток телепорта",
	"your pick":                      "ваш выбор",

	// The server's lines about the position the player is coached for.
	"what you played on %s last time": "так вы играли на %s в прошлый раз",
	"your usual role on %s":           "ваша обычная позиция на %s",
	"a guess for %s":                  "предположение для %s",
	"you laned %s":                    "вы стояли %s",
	"Coaching you as %s":              "Тренирую вас как %s",
	"Coaching you as %s.":             "Тренирую вас как %s.",
	"You're laning %s, so the coach switched you to %s. Another position? Press Ctrl+Shift+1 to 5": "Вы стоите %s, поэтому тренер переключил вас на позицию: %s. Другая позиция? Нажмите Ctrl+Shift+1–5",
	"You're %s. Coaching you as %s.": "Вы %s. Тренирую вас как %s.",
	"%s (from your %s game as %s)":   "%s (из вашей игры на %s, позиция: %s)",
	"%s (from your %s game)":         "%s (из вашей игры на %s)",
	// the line after a drilled match
	"Drill: %s %d times this game":              "Тренировка «%s»: за игру — %d",
	"Drill: %s %d times, under your usual %.1f": "Тренировка «%s»: за игру — %d, меньше обычного (%.1f)",
	"Drill: %s %d times, above your usual %.1f": "Тренировка «%s»: за игру — %d, больше обычного (%.1f)",
	"in the safe lane":                          "на лёгкой линии",
	"in the mid lane":                           "на миде",
	"in the offlane":                            "на сложной линии",
}
