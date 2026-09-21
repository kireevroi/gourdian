package mmr

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"gourdian/internal/model"
)

var day = time.Date(2026, 9, 17, 18, 0, 0, 0, time.UTC)

func logged(entries ...model.MMREntry) []model.MMREntry { return entries }

func TestBeforeTakesTheLatestEarlierEntry(t *testing.T) {
	entries := logged(
		model.MMREntry{Date: day, MMR: 3000},
		model.MMREntry{Date: day.Add(time.Hour), MMR: 3025},
	)
	got, ok := Before(entries, "", time.Time{})
	if !ok || got != 3025 {
		t.Fatalf("Before = %d/%v, want the last entry", got, ok)
	}
}

func TestBeforeSkipsTheMatchesOwnEntry(t *testing.T) {
	entries := logged(
		model.MMREntry{Date: day, MMR: 3000},
		model.MMREntry{Date: day.Add(time.Hour), MMR: 3025, MatchID: "m1"},
	)
	got, ok := Before(entries, "m1", time.Time{})
	if !ok || got != 3000 {
		t.Fatalf("Before = %d/%v, want the entry before m1 so its win isn't counted twice", got, ok)
	}
}

func TestBeforeIgnoresEntriesLoggedAfterTheMatchEnded(t *testing.T) {
	entries := logged(
		model.MMREntry{Date: day, MMR: 3000},
		model.MMREntry{Date: day.Add(3 * time.Hour), MMR: 3100},
	)
	got, ok := Before(entries, "", day.Add(time.Hour))
	if !ok || got != 3000 {
		t.Fatalf("Before = %d/%v, want only what was logged by then", got, ok)
	}
}

func TestBeforeReportsWhenThereIsNothingToGoOn(t *testing.T) {
	if _, ok := Before(nil, "", time.Time{}); ok {
		t.Fatal("nothing logged, so there is no earlier MMR")
	}
	only := logged(model.MMREntry{Date: day, MMR: 3000, MatchID: "m1"})
	if _, ok := Before(only, "m1", time.Time{}); ok {
		t.Fatal("the only entry is the match's own, so there is nothing before it")
	}
}

func TestForFindsTheMatchesEntry(t *testing.T) {
	entries := logged(
		model.MMREntry{Date: day, MMR: 3000},
		model.MMREntry{Date: day.Add(time.Hour), MMR: 3025, MatchID: "m1"},
	)
	if got := For(entries, "m1"); got != 3025 {
		t.Fatalf("For = %d, want 3025", got)
	}
	if got := For(entries, "m2"); got != 0 {
		t.Fatalf("For = %d, want 0 for a match with nothing logged", got)
	}
}

func TestConfirmingRankedKeepsThePromptAndMarksIt(t *testing.T) {
	var p Prompts
	p.Ask(&Prompt{MatchID: "m1"})
	next, changed := p.ConfirmRanked("m1", true)
	if !changed || next == nil || !next.Ranked {
		t.Fatalf("ConfirmRanked = %+v/%v, want the prompt marked ranked", next, changed)
	}
	if got := p.Pending(); got == nil || !got.Ranked {
		t.Fatalf("pending = %+v, want it kept and marked", got)
	}
}

func TestAnUnrankedMatchTakesThePromptAway(t *testing.T) {
	var p Prompts
	p.Ask(&Prompt{MatchID: "m1"})
	next, changed := p.ConfirmRanked("m1", false)
	if !changed || next != nil {
		t.Fatalf("ConfirmRanked = %+v/%v, want nothing left to announce", next, changed)
	}
	if p.Pending() != nil {
		t.Fatal("an unranked match should clear the prompt")
	}
}

func TestAnotherMatchLeavesThePromptAlone(t *testing.T) {
	var p Prompts
	p.Ask(&Prompt{MatchID: "m1"})
	if next, changed := p.ConfirmRanked("m2", true); changed || next != nil {
		t.Fatalf("ConfirmRanked = %+v/%v, want no change for another match", next, changed)
	}
	if got := p.Pending(); got == nil || got.MatchID != "m1" || got.Ranked {
		t.Fatalf("pending = %+v, want m1 untouched", got)
	}
}

func TestConfirmingWithNoPromptChangesNothing(t *testing.T) {
	var p Prompts
	if next, changed := p.ConfirmRanked("m1", true); changed || next != nil {
		t.Fatalf("ConfirmRanked = %+v/%v, want no change", next, changed)
	}
}

// A prompt handed out for encoding must not change underneath the encoder (run with -race).
func TestAHandedOutPromptIsNeverChangedInPlace(t *testing.T) {
	var p Prompts
	p.Ask(&Prompt{MatchID: "m1"})
	held := p.Pending()
	p.ConfirmRanked("m1", true)
	if held.Ranked {
		t.Fatal("the prompt handed out earlier was changed in place")
	}

	p.Ask(&Prompt{MatchID: "m2"})
	var wg sync.WaitGroup
	wg.Go(func() {
		for range 200 {
			p.ConfirmRanked("m2", true)
		}
	})
	wg.Go(func() {
		for range 200 {
			if _, err := json.Marshal(p.Pending()); err != nil {
				t.Error(err)
			}
		}
	})
	wg.Wait()
	if got := p.Pending(); got == nil || !got.Ranked {
		t.Fatalf("pending = %+v, want it marked ranked", got)
	}
}
