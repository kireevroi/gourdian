package prompts

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gourdian/internal/ai"
	"gourdian/internal/coaching/picks"
	"gourdian/internal/game/dota"
	"gourdian/internal/sys/config"
)

// draftSystemPrompt is deliberate about what the coach cannot see. Valve sends the draft to
// spectators only, so a player's tools never learn the enemy picks, and a coach that guessed
// at them would sound authoritative about nothing.
// The coach is told whether the enemy picks are known, because that changes what it may say.
// Dota tells a player's tools nothing about the draft, so most of the time they aren't; when
// the player has turned on reading them off their own screen, they are.
const draftSystemPrompt = `You are a Dota 2 coach helping one player choose a hero, in the seconds before they lock in. Their goal is to climb in MMR.

You are given their position, the heroes they have a record on there with their own win rates, how each of those heroes is doing in public games at their rank, and the heroes they keep losing on.

Answer with one sentence of under 25 words: name one hero from the lists you were given and the reason to take it. Prefer a hero the player already plays; suggest one that is new to them only when their own pool is thin or losing. No greeting, no hedging, no alternatives, no numbering.`

// blindToTheDraft is added when the enemy picks aren't known, which is the usual case.
const blindToTheDraft = `

You are NOT given the enemy team's picks or bans, or their own team's. Never mention, guess at or counter enemy heroes, and never talk about a lineup.`

// seesTheDraft is added when they are.
const seesTheDraft = `

You are also given the heroes the other team has taken, read off the player's own screen. You may counter them, but only with a hero from the lists you were given, and say which enemy hero you are answering. Say nothing about heroes that are not on those lists, and nothing about their own team, which you cannot see.`

const draftSchema = `{"type":"object","properties":{"advice":{"type":"string"}},"required":["advice"],"additionalProperties":false}`

// DraftInput is what the coach is told before the pick.
type DraftInput struct {
	Context
	Role  string
	Board *picks.Board
}

func DraftPrompt(in DraftInput) string {
	var w writer
	w.line("The player is choosing a hero for %s.", dota.RoleName(in.Role, "en"))
	if in.Profile != "" {
		w.line("About them: %s", in.Profile)
	}
	if h := in.History; h.Matches > 0 {
		w.line("Over their last %d matches: %.0f%% won, %.1f deaths a game.", h.Matches, h.WinRate*100, h.AvgDeaths)
	}
	if in.Focus != "" {
		w.line("Their focus from the last review: %s", in.Focus)
	}
	w.heroList("Heroes they play in this position, best first", in.Board.Best)
	w.heroList("Heroes doing well at their rank that they don't play", in.Board.Fresh)
	w.heroList("Heroes they keep losing on", in.Board.Avoid)
	if names := enemyNames(in.Board); names != "" {
		w.line("The other team has taken: %s.", names)
	}
	return w.String()
}

func enemyNames(b *picks.Board) string {
	if b == nil || len(b.Enemies) == 0 {
		return ""
	}
	var names []string
	for _, h := range b.Enemies {
		if h.Name != "" {
			names = append(names, h.Name)
		}
	}
	return strings.Join(names, ", ")
}

// heroList writes one of the board's lists, each hero with the reasons it is on it.
func (w *writer) heroList(title string, heroes []picks.Hero) {
	if len(heroes) == 0 {
		return
	}
	var lines []string
	for _, h := range heroes {
		line := h.Name
		if h.Games > 0 {
			line += fmt.Sprintf(" (%d%% of %d games", h.WinPct, h.Games)
			if len(h.Why) > 0 {
				line += "; " + strings.Join(h.Why, ", ")
			}
			line += ")"
		} else if len(h.Why) > 0 {
			line += " (" + strings.Join(h.Why, ", ") + ")"
		}
		lines = append(lines, line)
	}
	w.line("%s: %s.", title, strings.Join(lines, "; "))
}

// AskDraft asks which hero to take and returns the one sentence, or an error.
func AskDraft(ctx context.Context, p ai.Provider, choice config.AIChoice, set config.AISettings, lang, prompt string, sawDraft bool) (string, error) {
	var out struct {
		Advice string `json:"advice"`
	}
	system := draftSystemPrompt + blindToTheDraft
	if sawDraft {
		system = draftSystemPrompt + seesTheDraft
	}
	req := ai.Request{System: withInstructions(system, set, lang), Prompt: prompt, Schema: draftSchema,
		Model: choice.Model, Effort: choice.Effort}
	if err := complete(ctx, p, req, &out); err != nil {
		return "", err
	}
	if strings.TrimSpace(out.Advice) == "" {
		return "", errors.New("the coach had nothing to say about the pick")
	}
	return strings.TrimSpace(out.Advice), nil
}
