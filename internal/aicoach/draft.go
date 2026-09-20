package aicoach

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gourdian/internal/ai"
	"gourdian/internal/config"
	"gourdian/internal/dota"
	"gourdian/internal/picks"
)

// draftSystemPrompt is deliberate about what the coach cannot see. Valve sends the draft to
// spectators only, so a player's tools never learn the enemy picks, and a coach that guessed
// at them would sound authoritative about nothing.
const draftSystemPrompt = `You are a Dota 2 coach helping one player choose a hero, in the seconds before they lock in. Their goal is to climb in MMR.

You are given their position, the heroes they have a record on there with their own win rates, how each of those heroes is doing in public games at their rank, and the heroes they keep losing on. You are NOT given the enemy team's picks or bans, or their own team's: Dota does not share the draft with a player's tools. Never mention, guess at or counter enemy heroes, and never talk about a lineup.

Answer with one sentence of under 25 words: name one hero from the lists you were given and the reason to take it. Prefer a hero the player already plays; suggest one that is new to them only when their own pool is thin or losing. No greeting, no hedging, no alternatives, no numbering.`

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
	return w.String()
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
func AskDraft(ctx context.Context, p ai.Provider, choice config.AIChoice, set config.AISettings, lang, prompt string) (string, error) {
	var out struct {
		Advice string `json:"advice"`
	}
	req := ai.Request{System: withInstructions(draftSystemPrompt, set, lang), Prompt: prompt, Schema: draftSchema,
		Model: choice.Model, Effort: choice.Effort}
	if err := complete(ctx, p, req, &out); err != nil {
		return "", err
	}
	if strings.TrimSpace(out.Advice) == "" {
		return "", errors.New("the coach had nothing to say about the pick")
	}
	return strings.TrimSpace(out.Advice), nil
}
