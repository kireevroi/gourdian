package drill

import (
	"testing"
	"time"

	"gourdian/internal/coaching/coach"
	"gourdian/internal/game/model"
)

var habits = []coach.Rule{
	{ID: "no_tp", Label: "No TP scroll", Advice: "Buy one every time you go back", Habit: "tp"},
	{ID: "unspent", Label: "Unspent gold", Habit: "gold"},
}

func played(ids ...string) []model.MatchSummary {
	var out []model.MatchSummary
	for i, id := range ids {
		out = append(out, model.MatchSummary{MatchID: id, Hero: "Anti-Mage", Source: model.SourceLive,
			EndedAt: time.Date(2026, 9, 17, 18, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Hour)})
	}
	return out
}

func TestHabitsAreTheRulesThatCountMistakes(t *testing.T) {
	rules := append([]coach.Rule{{ID: "runes", Label: "Runes"}}, habits...)
	got := Habits(rules)
	if len(got) != 2 || got[0].ID != "no_tp" || got[1].ID != "unspent" {
		t.Fatalf("Habits = %+v, want only the two with a Habit set", got)
	}
}

func TestCountableKeepsOnlyWatchedRealGames(t *testing.T) {
	recent := played("a", "b", "c", "d")
	recent[1].Source = model.SourcePractice
	recent[2].Source = model.SourceOpenDota // imported, so never coached
	recent[3].Simulated = true
	got := Countable(recent)
	if len(got) != 1 || got[0].MatchID != "a" {
		t.Fatalf("Countable = %+v, want only the live game", got)
	}
}

func TestCountableStopsAtTenAndLeavesTheInputAlone(t *testing.T) {
	var ids []string
	for i := range 15 {
		ids = append(ids, string(rune('a'+i)))
	}
	recent := played(ids...)
	if got := Countable(recent); len(got) != Matches {
		t.Fatalf("Countable kept %d matches, want %d", len(got), Matches)
	}
	if len(recent) != 15 {
		t.Fatalf("Countable modified its input, now %d long", len(recent))
	}
}

func TestNothingToScoreGivesAnEmptyViewThatStillCarriesTheChoice(t *testing.T) {
	v := Score(nil, nil, nil, "no_tp")
	if v.Rule != "no_tp" {
		t.Fatalf("the chosen rule should survive a failed read, got %q", v.Rule)
	}
	if v.Choices == nil {
		t.Fatal("Choices must serialize as [] rather than null")
	}
	if v.Suggestion != "" || v.Label != "" {
		t.Fatalf("nothing to score, so nothing to say: %+v", v)
	}
}

func TestTheAverageIsPerMatchAndTheBestIsTheQuietest(t *testing.T) {
	recent := played("a", "b", "c", "d")
	fires := map[string]map[string]int{"no_tp": {"a": 4, "b": 2, "c": 0, "d": 2}}
	v := Score(habits, recent, fires, "no_tp")

	if v.Average != 2 {
		t.Fatalf("average = %v, want 8 fires over 4 matches", v.Average)
	}
	if v.Best != 0 {
		t.Fatalf("best = %d, want the quietest match", v.Best)
	}
	if v.Label != "No TP scroll" || v.Advice != "Buy one every time you go back" {
		t.Fatalf("the chosen rule's wording should be carried over: %+v", v)
	}
	if len(v.Recent) != 4 || v.Recent[0].Count != 4 || v.Recent[0].Hero != "Anti-Mage" {
		t.Fatalf("recent = %+v", v.Recent)
	}
}

func TestOnlyTheChosenRuleGetsTheDetail(t *testing.T) {
	fires := map[string]map[string]int{"no_tp": {"a": 1}, "unspent": {"a": 5}}
	v := Score(habits, played("a"), fires, "no_tp")
	if len(v.Recent) != 1 || v.Average != 1 {
		t.Fatalf("the detail should be the chosen rule's: %+v", v)
	}
	if len(v.Choices) != 2 {
		t.Fatalf("every habit stays a choice: %+v", v.Choices)
	}
}

func TestTheWorstHabitIsSuggestedFirst(t *testing.T) {
	fires := map[string]map[string]int{"no_tp": {"a": 1}, "unspent": {"a": 5}}
	v := Score(habits, played("a"), fires, "")
	if v.Choices[0].Rule != "unspent" {
		t.Fatalf("choices should be worst first: %+v", v.Choices)
	}
	if v.Suggestion != "unspent" || v.SuggestionLabel != "Unspent gold" {
		t.Fatalf("suggestion = %q/%q, want the worst habit", v.Suggestion, v.SuggestionLabel)
	}
}

func TestAHabitThatNeverFiresIsNotSuggested(t *testing.T) {
	v := Score(habits, played("a"), map[string]map[string]int{}, "")
	if v.Suggestion != "" {
		t.Fatalf("nothing fired, so there is nothing to drill: %q", v.Suggestion)
	}
	if len(v.Choices) != 2 {
		t.Fatalf("the habits are still offered: %+v", v.Choices)
	}
}
