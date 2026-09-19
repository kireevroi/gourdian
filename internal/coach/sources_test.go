package coach

import (
	"strings"
	"testing"

	"gourdian/internal/config"
	"gourdian/internal/dotadata"
)

func TestSourcesSayWhereAdviceComesFrom(t *testing.T) {
	builds := []*dotadata.Build{
		{Position: 2, Games: 235, Won: true},
		{Position: 2, Games: 15},
		{Games: 97, Won: true},
		{},
		{Games: 97, Won: true, Loading: true},
	}
	skills := []*dotadata.SkillBuild{{Position: 2, Games: 235, Won: true, Order: []string{"a"}}, {Games: 19, Won: true, Order: []string{"a"}}, {Position: 2, Games: 15, Order: []string{"a"}}, {}}
	targets := []Targets{
		RoleTargets(config.RoleMid),
		{LastHits: []int{33, 66, 110, 160, 264}, Games: 8, Items: []ItemGoal{{Item: "bfury"}}, ItemGames: 6},
		{LastHits: []int{33}, Items: []ItemGoal{{Item: "bfury"}}},
	}
	for _, lang := range []string{"en", "ru"} {
		for i := range builds {
			list := sources(builds[i], skills[i%len(skills)], targets[i%len(targets)], config.DefaultTimings(), "Storm Spirit", config.RoleMid, lang)
			for _, src := range list {
				if src.What == "" || src.From == "" || strings.Contains(src.From, "%!") || strings.Contains(src.Short, "%!") ||
					!strings.Contains(src.From, "Storm Spirit") && src.ID != "timers" && src.ID != "item_goals" {
					t.Errorf("%s %s: %+v", lang, src.ID, src)
				}
			}
		}
	}
	src := buildSource(builds[0], "Storm Spirit", config.RoleMid, sourceWords{"en"})
	if !strings.HasPrefix(src.From, "OpenDota: what pros bought on Storm Spirit as position 2 (mid) in the 235 games they won over the last 120 days.") ||
		src.Short != "mid, 235 won pro games" {
		t.Fatalf("won build: %+v", src)
	}
	src = buildSource(builds[1], "Pudge", config.RoleMid, sourceWords{"ru"})
	if !strings.Contains(src.From, "во всех их матчах за последние 120 дней, победы и поражения, матчей: 15.") ||
		!strings.Contains(src.From, "Побед среди них меньше 12") || src.Short != "мид, про-матчей: 15" {
		t.Fatalf("position build from all games: %+v", src)
	}
	src = buildSource(builds[4], "Pudge", config.RoleMid, sourceWords{"en"})
	if !strings.Contains(src.From, "The build for mid is still loading.") || !strings.HasSuffix(src.Short, " · loading") {
		t.Fatalf("loading build: %+v", src)
	}
}

func TestRussianSourcesReadWell(t *testing.T) {
	w := sourceWords{"ru"}
	got := buildSource(&dotadata.Build{Position: 2, Games: 235, Won: true}, "Storm Spirit", config.RoleMid, w).From
	if !strings.HasPrefix(got, "OpenDota: покупки про-игроков на Storm Spirit, позиция 2 (мид), в выигранных матчах за последние 120 дней, матчей: 235.") {
		t.Fatalf("position build: %q", got)
	}
	got = buildSource(&dotadata.Build{Games: 97, Won: true}, "Pudge", config.RoleMid, w).From
	if !strings.HasPrefix(got, "OpenDota: покупки про-игроков на Pudge на всех позициях в выигранных матчах") {
		t.Fatalf("every position: %q", got)
	}
}
