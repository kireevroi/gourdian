package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strconv"
	"testing"
	"time"

	"dotatrainer/internal/ai"
	"dotatrainer/internal/aicoach"
	"dotatrainer/internal/coach"
	"dotatrainer/internal/config"
	"dotatrainer/internal/dotadata"
	"dotatrainer/internal/gsi"
	"dotatrainer/internal/sim"
)

func TestReplayRecording(t *testing.T) {
	path := os.Getenv("REPLAY")
	if path == "" {
		t.Skip("set REPLAY to a recording")
	}
	promptAt, asking := strconv.Atoi(os.Getenv("PROMPT_AT"))
	var od *dotadata.Client
	var e *coach.Engine
	set := config.Default().Settings
	set.Role, set.Voice = config.RoleHardSupport, config.VoiceOff
	if r := os.Getenv("ROLE"); r != "" {
		set.Role = r
	}
	if asking == nil || os.Getenv("LIVE_OPENDOTA") != "" {
		od = dotadata.New(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
		od.Start(t.Context())
		warm(t, od, path, set.Role)
		e = coach.New(od, slog.New(slog.NewTextHandler(io.Discard, nil)))
	} else {
		e = coach.New(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}
	counts, total, spoken := map[string]int{}, 0, 0
	err := sim.ReadRecording(path, func(_ int64, payload map[string]json.RawMessage) error {
		data, _ := json.Marshal(payload)
		var st gsi.State
		if json.Unmarshal(data, &st) != nil {
			return nil
		}
		if asking == nil && st.Map != nil && st.Map.ClockTime >= promptAt {
			askAt(t, e, od, set, st.Hero)
			asking = io.EOF
		}
		for _, tip := range e.Update(&st, set).Tips {
			t.Logf("%3d:%02d %-16s %s", tip.Clock/60, tip.Clock%60, tip.Rule, tip.Text)
			counts[tip.Rule]++
			total++
			if !tip.Quiet {
				spoken++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d alerts in the match, %d of them spoken", total, spoken)
	for rule, n := range counts {
		t.Logf("%-20s %d×", rule, n)
	}
}

// warm loads the recorded hero's build and skill order before the replay, which runs faster
// than OpenDota answers.
func warm(t *testing.T, od *dotadata.Client, path, role string) {
	hero := 0
	_ = sim.ReadRecording(path, func(_ int64, payload map[string]json.RawMessage) error {
		var h gsi.Hero
		if raw, ok := payload["hero"]; ok && hero == 0 && json.Unmarshal(raw, &h) == nil {
			hero = h.ID
		}
		return nil
	})
	for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		b := od.BuildFor(hero, role)
		if sk := od.SkillBuildFor(hero, role); b != nil && !b.Loading && sk != nil {
			t.Logf("hero %d as %s: build from position %d, %d games (won only %v); skills from position %d, %d games (won only %v)",
				hero, role, b.Position, b.Games, b.Won, sk.Position, sk.Games, sk.Won)
			return
		}
	}
	t.Logf("hero %d: OpenDota data didn't load in a minute", hero)
}

// askAt prints the live prompt for this moment of the recording and, with LIVE_AI=1, what
// Claude answers to it.
func askAt(t *testing.T, e *coach.Engine, data *dotadata.Client, set config.Settings, hero *gsi.Hero) {
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if b := data.BuildFor(hero.ID, set.Role); b != nil && !b.Loading {
			break
		}
	}
	facts := &aicoach.HeroFacts{Build: map[string][]string{}}
	if info, ok := data.Hero(hero.ID); ok {
		facts.Name, facts.Roles = info.LocalizedName, info.Roles
	}
	if b := data.BuildFor(hero.ID, set.Role); b != nil {
		facts.BuildPosition, facts.BuildGames, facts.BuildWon = b.Position, b.Games, b.Won
		for _, it := range b.Items {
			facts.Build[it.Phase] = append(facts.Build[it.Phase], it.DName)
		}
	}
	snap := e.Snapshot(set)
	prompt := aicoach.Prompt(aicoach.Input{Context: aicoach.Context{Hero: facts}, Reason: "regular check-in", Role: set.Role,
		Snapshot: snap, Facts: e.Facts(set.Role), Tips: e.RecentTips()})
	t.Logf("prompt at %d:\n%s", snap.Clock, prompt)
	if os.Getenv("LIVE_AI") == "" {
		return
	}
	env := ai.Env{WorkDir: t.TempDir(), CLIPath: func(string) string { return "" }, Key: func(string) string { return "" }, CustomURL: func() string { return "" }}
	for _, p := range ai.NewProviders(env) {
		if p.Info().ID != "claude" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		tips, err := aicoach.Suggest(ctx, p, config.AIChoice{Provider: "claude", Model: "sonnet", Effort: "low"}, config.AISettings{}, os.Getenv("LANGUAGE_CODE"), prompt)
		t.Logf("coach: %q (err %v)", tips, err)
	}
}
