package aicoach

import (
	"context"
	"os"
	"testing"
	"time"

	"gourdian/internal/ai"
	"gourdian/internal/coach"
	"gourdian/internal/config"
	"gourdian/internal/dota"
)

// TestLiveCoachOnStormSpirit asks the real coach one question; it only runs when asked to.
func TestLiveCoachOnStormSpirit(t *testing.T) {
	if os.Getenv("LIVE_AI") == "" {
		t.Skip("set LIVE_AI=1 to spend one real request")
	}
	facts := &HeroFacts{Name: "Storm Spirit", Roles: []string{"Carry", "Escape", "Nuker", "Initiator", "Disabler"},
		Build: map[string][]string{
			"start": {"Iron Branch", "Tango", "Faerie Fire", "Observer Ward", "Circlet"},
			"early": {"Boots of Speed", "Bottle", "Soul Ring", "Power Treads", "Null Talisman"},
			"mid":   {"Ogre Axe", "Kaya", "Staff of Wizardry", "Oblivion Staff", "Blitz Knuckles", "Witch Blade"},
			"late":  {"Mystic Staff", "Parasma", "Black King Bar", "Kaya and Sange", "Aghanim's Scepter"},
		},
		Owned: []string{"Bottle", "Power Treads", "Soul Ring", "Null Talisman"}}
	snap := coach.Snapshot{InMatch: true, Clock: 720, Team: "radiant", Role: dota.Mid,
		Hero: &coach.HeroView{Name: "Storm Spirit", Level: 11, Alive: true, HealthPercent: 85, ManaPercent: 60}}
	prompt := Prompt(Input{Context: Context{Hero: facts}, Reason: "a regular check-in; the player has 2400 gold", Role: dota.Mid, Snapshot: snap})
	env := ai.Env{WorkDir: t.TempDir(), CLIPath: func(string) string { return "" }, Key: func(string) string { return "" }, CustomURL: func() string { return "" }}
	var claude ai.Provider
	for _, p := range ai.NewProviders(env) {
		if p.Info().ID == "claude" {
			claude = p
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	tips, err := Suggest(ctx, claude, config.AIChoice{Provider: "claude", Model: "sonnet", Effort: "low"}, config.AISettings{}, "en", prompt)
	if err != nil {
		t.Fatal(err)
	}
	for _, tip := range tips {
		t.Log("coach:", tip)
	}
}
