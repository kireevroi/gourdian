package coach

import (
	"fmt"
	"strings"

	"gourdian/internal/buildinfo"
	"gourdian/internal/config"
	"gourdian/internal/dotadata"
)

// Source says where a piece of advice comes from, so the player can judge how far to trust it.
type Source struct {
	ID   string `json:"id"` // build, skills, last_hits, item_goals, timers
	What string `json:"what"`
	From string `json:"from"`
	// Short is the few words the HUD adds after a row.
	Short string `json:"short,omitempty"`
}

// proDays is how far back the pro games behind builds and skill orders go.
const proDays = 120

type sourceWords struct{ lang string }

// f formats the English or the Russian; Russian formats reorder arguments with %[n]d.
func (w sourceWords) f(en, ru string, args ...any) string {
	if w.lang == "ru" {
		return fmt.Sprintf(ru, args...)
	}
	return fmt.Sprintf(en, args...)
}

func (w sourceWords) position(pos int) string {
	return w.f("position %d (%s)", "позиция %d (%s)", pos, RoleName(positionRoles[pos], w.lang))
}

func buildSource(b *dotadata.Build, hero, role string, w sourceWords) Source {
	src := Source{ID: "build", What: w.f("Item build", "Сборка предметов")}
	pos := dotadata.Positions[role]
	where, short := w.f("in every position", " на всех позициях"), w.f("all positions", "все позиции")
	if b.Position > 0 {
		where, short = w.f("as %s", ", %s,", w.position(b.Position)), RoleName(positionRoles[b.Position], w.lang)
	}
	var notes []string
	switch {
	case b.Games == 0:
		src.From = w.f("OpenDota: what pros bought on %s in up to 100 recent pro games, in every position, won or lost.",
			"OpenDota: покупки про-игроков на %s в последних про-матчах (до 100), на всех позициях, победы и поражения.", hero)
		src.Short = w.f("all positions, recent pro games", "все позиции, последние про-матчи")
		if pos > 0 && !b.Loading {
			notes = append(notes, w.f("Pros won fewer than 12 games with it in the last %d days.",
				"За %d дней про-игроки выиграли на нём меньше 12 матчей.", proDays))
		}
	case b.Won:
		src.From = w.f("OpenDota: what pros bought on %s %s in the %d games they won over the last %d days.",
			"OpenDota: покупки про-игроков на %[1]s%[2]s в выигранных матчах за последние %[4]d дней, матчей: %[3]d.", hero, where, b.Games, proDays)
		src.Short = w.f("%s, %d won pro games", "%s, побед про: %d", short, b.Games)
	default:
		src.From = w.f("OpenDota: what pros bought on %s %s in all %d of their games there over the last %d days, won or lost.",
			"OpenDota: покупки про-игроков на %[1]s%[2]s во всех их матчах за последние %[4]d дней, победы и поражения, матчей: %[3]d.", hero, where, b.Games, proDays)
		src.Short = w.f("%s, %d pro games", "%s, про-матчей: %d", short, b.Games)
	}
	switch {
	case b.Position == 0 && b.Games > 0 && pos > 0 && !b.Loading:
		notes = append(notes, w.f("Pros played it as %s in fewer than 12 games.", "На позиции «%s» у про-игроков меньше 12 матчей.", RoleName(role, w.lang)))
	case b.Position > 0 && !b.Won:
		notes = append(notes, w.f("They won fewer than 12 of them, too few for a build from wins alone.",
			"Побед среди них меньше 12 — слишком мало для сборки только по победам."))
	}
	notes = append(notes, w.f("Items are grouped by when they were bought (before the horn, before 10:00, before 25:00, later) and listed by how many of those games had them.",
		"Предметы разбиты по времени покупки (до начала, до 10:00, до 25:00, позже) и отсортированы по тому, в скольких матчах их купили."))
	if b.Loading && pos > 0 {
		notes = append(notes, w.f("The build for %s is still loading.", "Сборка для позиции «%s» ещё загружается.", RoleName(role, w.lang)))
		src.Short += w.f(" · loading", " · загрузка")
	}
	src.From += " " + strings.Join(notes, " ")
	return src
}

func skillsSource(b *dotadata.SkillBuild, hero, role string, w sourceWords) Source {
	src := Source{ID: "skills", What: w.f("Skill order", "Порядок прокачки"), Short: skillSource(b, w.lang)}
	if len(b.Order) == 0 {
		src.From = w.f("No pro skill order: pros played %s in fewer than 12 games over the last %d days, so the skill point reminder just says to level up.",
			"Порядка прокачки нет: за %[2]d дней у про-игроков меньше 12 матчей на %[1]s, поэтому напоминание просто просит прокачаться.", hero, proDays)
		return src
	}
	games := w.f("pro games, won or lost", "матчей про-игроков, победы и поражения")
	if b.Won {
		games = w.f("won pro games", "выигранных матчей про-игроков")
	}
	if b.Position > 0 {
		src.From = w.f("OpenDota: skill orders from the %d most recent %s on %s as %s, over the last %d days.",
			"OpenDota: порядок прокачки из последних %[2]s на %[3]s, %[4]s, за %[5]d дней, матчей: %[1]d.",
			b.Games, games, hero, w.position(b.Position), proDays)
	} else {
		src.From = w.f("OpenDota: skill orders from the %d most recent %s on %s in every position, over the last %d days, because too few were as %s.",
			"OpenDota: порядок прокачки из последних %[2]s на %[3]s на всех позициях за %[4]d дней, матчей: %[1]d — на позиции «%[5]s» их слишком мало.",
			b.Games, games, hero, proDays, RoleName(role, w.lang))
	}
	src.From += " " + w.f("Each point is the ability most of them took there, and the next one is worked out from your own ability levels.",
		"На каждом уровне — способность, которую взяло большинство, а следующая считается от ваших текущих уровней.")
	return src
}

func lastHitSource(t Targets, hero, role string, w sourceWords) Source {
	src := Source{ID: "last_hits", What: w.f("Last-hit targets", "Цели по добиваниям")}
	if t.Games > 0 {
		src.From = w.f("Your own games: 10%% above your median at each checkpoint over your last %d games on %s as %s. Checkpoints with fewer than 3 of your games use the built-in targets.",
			"Ваши матчи: на 10%% выше вашей медианы на каждой отметке за последние матчи на %[2]s (%[3]s), матчей: %[1]d. Где ваших матчей меньше 3, цели встроенные.",
			t.Games, hero, RoleName(role, w.lang))
		src.Short = w.f("your %d games +10%%", "ваши матчи: %d, +10%%", t.Games)
		return src
	}
	var marks []string
	for i, cp := range paceCheckpoints {
		if i < len(t.LastHits) {
			marks = append(marks, w.f("%d by %s", "%d к %s", t.LastHits[i], clockStr(cp)))
		}
	}
	src.From = w.f("Built into the trainer for %s: %s. After 3 of your games on %s in this position, the targets come from your own games.",
		"Встроенные цели для позиции «%s»: %s. После 3 ваших матчей на %s на этой позиции цели считаются по вашим матчам.",
		RoleName(role, w.lang), strings.Join(marks, ", "), hero)
	src.Short = w.f("built-in", "встроенные")
	return src
}

func itemGoalSource(t Targets, hero string, w sourceWords) Source {
	src := Source{ID: "item_goals", What: w.f("Item goals", "Цели по предметам")}
	if t.ItemGames > 0 {
		src.From = w.f("Items: the core items you finished first in at least 2 of your last %d games on %s.",
			"Предметы: ядро, которое вы собирали первым хотя бы в 2 из последних матчей на %[2]s, матчей: %[1]d.", t.ItemGames, hero)
	} else {
		src.From = w.f("Items: the core items (2000 gold or more) of the pro build.", "Предметы: ключевые предметы (от 2000 золота) из про-сборки.")
	}
	src.From += " " + w.f("Times: the earliest time that wins at least as often as the item does overall, from OpenDota's item timings (games it parsed in the last 4 weeks, every position). When your usual time is later than that, the goal is a minute before your usual.",
		"Время: самое раннее, при котором предмет выигрывает не реже, чем в среднем, по таймингам OpenDota (матчи, разобранные за последние 4 недели, все позиции). Если вы обычно собираете его позже — цель на минуту раньше вашего обычного времени.")
	return src
}

func timerSource(t config.Timings, w sourceWords) Source {
	return Source{ID: "timers", What: w.f("Timers", "Таймеры"),
		From: w.f("Built into Gourdian %s: bounty runes every %s, power runes from %s every %s, Shrines of Wisdom every %s. An app update brings new patch timings; the rules that use them show them.",
			"Встроены в Gourdian %s: руны богатства каждые %s, руны силы с %s каждые %s, святыни мудрости каждые %s. Новые тайминги патча приходят с обновлением приложения; правила, которые их используют, их показывают.",
			buildinfo.Version, clockStr(t.BountyRuneEvery), clockStr(t.PowerRuneFirst), clockStr(t.PowerRuneEvery), clockStr(t.WisdomRuneEvery))}
}

// sources lists where each part of the snapshot comes from.
func sources(build *dotadata.Build, skills *dotadata.SkillBuild, t Targets, timings config.Timings, hero, role, lang string) []Source {
	w := sourceWords{lang}
	var out []Source
	if build != nil {
		out = append(out, buildSource(build, hero, role, w))
	}
	if skills != nil {
		out = append(out, skillsSource(skills, hero, role, w))
	}
	if len(t.LastHits) > 0 {
		out = append(out, lastHitSource(t, hero, role, w))
	}
	if len(t.Items) > 0 {
		out = append(out, itemGoalSource(t, hero, w))
	}
	return append(out, timerSource(timings, w))
}
