package mmr

import (
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"

	"gourdian/internal/game/model"
)

var day = time.Date(2026, 9, 17, 18, 0, 0, 0, time.UTC)

func TestBeforeTakesTheLatestEarlierEntry(t *testing.T) {
	entries := []model.MMREntry{
		{Date: day, MMR: 3000},
		{Date: day.Add(time.Hour), MMR: 3025},
	}
	got, ok := Before(entries, "", time.Time{})
	if !ok || got != 3025 {
		t.Fatalf("Before = %d/%v, want the last entry", got, ok)
	}
}

func TestBeforeSkipsTheMatchesOwnEntry(t *testing.T) {
	entries := []model.MMREntry{
		{Date: day, MMR: 3000},
		{Date: day.Add(time.Hour), MMR: 3025, MatchID: "m1"},
	}
	got, ok := Before(entries, "m1", time.Time{})
	if !ok || got != 3000 {
		t.Fatalf("Before = %d/%v, want the entry before m1 so its win isn't counted twice", got, ok)
	}
}

func TestBeforeIgnoresEntriesLoggedAfterTheMatchEnded(t *testing.T) {
	entries := []model.MMREntry{
		{Date: day, MMR: 3000},
		{Date: day.Add(3 * time.Hour), MMR: 3100},
	}
	got, ok := Before(entries, "", day.Add(time.Hour))
	if !ok || got != 3000 {
		t.Fatalf("Before = %d/%v, want only what was logged by then", got, ok)
	}
}

func TestBeforeReportsWhenThereIsNothingToGoOn(t *testing.T) {
	if _, ok := Before(nil, "", time.Time{}); ok {
		t.Fatal("nothing logged, so there is no earlier MMR")
	}
	only := []model.MMREntry{{Date: day, MMR: 3000, MatchID: "m1"}}
	if _, ok := Before(only, "m1", time.Time{}); ok {
		t.Fatal("the only entry is the match's own, so there is nothing before it")
	}
}

func TestForFindsTheMatchesEntry(t *testing.T) {
	entries := []model.MMREntry{
		{Date: day, MMR: 3000},
		{Date: day.Add(time.Hour), MMR: 3025, MatchID: "m1"},
	}
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

func daysAgo(n float64) time.Time { return day.Add(-time.Duration(n * float64(24*time.Hour))) }

func ranked(id string, ended time.Time) model.MatchSummary {
	return model.MatchSummary{MatchID: id, Ranked: true, Source: model.SourceLive, EndedAt: ended}
}

func TestTowardCountsMatchesAndDays(t *testing.T) {
	entries := []model.MMREntry{{Date: daysAgo(20), MMR: 3000}, {Date: day, MMR: 3100}}
	var matches []model.MatchSummary
	for i := range 10 {
		matches = append(matches, ranked(strconv.Itoa(i), daysAgo(float64(i*2)+0.5)))
	}
	f, ok := Toward(3234, entries, matches)
	if !ok || f.Status != Closing || f.Change != 100 || f.Matches != 10 || f.Days != 20 {
		t.Fatalf("Toward = %+v/%v, want +100 over 10 matches and 20 days", f, ok)
	}
	// 134 to go at +10 a match and +5 a day.
	if f.MatchesLeft != 14 || f.DaysLeft != 27 {
		t.Fatalf("left: %d matches, %d days; want 14 and 27", f.MatchesLeft, f.DaysLeft)
	}
}

func TestTowardMeasuresFromTheEntryBeforeTheWindow(t *testing.T) {
	entries := []model.MMREntry{
		{Date: daysAgo(60), MMR: 2000},
		{Date: daysAgo(35), MMR: 2900},
		{Date: daysAgo(10), MMR: 3000},
		{Date: day, MMR: 3100},
	}
	matches := []model.MatchSummary{ranked("old", daysAgo(40)), ranked("at the entry", daysAgo(35)), ranked("in", daysAgo(30))}
	f, _ := Toward(3234, entries, matches)
	if !f.Since.Equal(daysAgo(35)) || f.Change != 200 || f.Matches != 1 {
		t.Fatalf("Toward = %+v, want +200 and one match since the entry 35 days back", f)
	}
	if f.MatchesLeft != 0 || f.DaysLeft != 24 {
		t.Fatalf("left: %d matches, %d days; one match is too few to go on, and 134 at 200 in 35 days is 24", f.MatchesLeft, f.DaysLeft)
	}
}

func TestTowardCountsOnlyRealRankedMatchesInTheStretch(t *testing.T) {
	entries := []model.MMREntry{
		{Date: daysAgo(3), MMR: 3000},
		{Date: daysAgo(1), MMR: 3050, MatchID: "logged"},
		{Date: day, MMR: 3060},
	}
	unranked := ranked("unranked", daysAgo(2))
	unranked.Ranked = false
	logged := ranked("logged", daysAgo(1))
	logged.Ranked = false
	practice := ranked("practice", daysAgo(2))
	practice.Source = model.SourcePractice
	matches := []model.MatchSummary{ranked("in", daysAgo(2)), logged, unranked, practice,
		ranked("before", daysAgo(4)), ranked("not logged yet", day.Add(time.Hour))}
	f, _ := Toward(3234, entries, matches)
	if f.Matches != 2 {
		t.Fatalf("counted %d matches, want the ranked one and the one with its MMR logged", f.Matches)
	}
}

func TestTowardSaysWhereTheGoalStands(t *testing.T) {
	var five []model.MatchSummary
	for i := range 5 {
		five = append(five, ranked(strconv.Itoa(i), daysAgo(1)))
	}
	for _, c := range []struct {
		name    string
		entries []model.MMREntry
		matches []model.MatchSummary
		goal    int
		want    string
	}{
		{"reached", []model.MMREntry{{Date: day, MMR: 3100}}, nil, 3080, Reached},
		{"one entry", []model.MMREntry{{Date: day, MMR: 3000}}, nil, 3080, Early},
		{"a losing week", []model.MMREntry{{Date: daysAgo(10), MMR: 3100}, {Date: day, MMR: 3050}}, nil, 3234, Stalled},
		{"five matches in a day", []model.MMREntry{{Date: daysAgo(1.5), MMR: 3000}, {Date: day, MMR: 3040}}, five, 3080, Closing},
	} {
		f, ok := Toward(c.goal, c.entries, c.matches)
		if !ok || f.Status != c.want {
			t.Errorf("%s: status %q/%v, want %q", c.name, f.Status, ok, c.want)
		}
	}
	f, _ := Toward(3080, []model.MMREntry{{Date: daysAgo(1.5), MMR: 3000}, {Date: day, MMR: 3040}}, five)
	if f.MatchesLeft != 5 || f.DaysLeft != 0 {
		t.Errorf("left: %d matches, %d days; want 5 at +8 a match, and no date from a day and a half", f.MatchesLeft, f.DaysLeft)
	}
	if _, ok := Toward(3080, nil, five); ok {
		t.Error("nothing logged, so there is nothing to forecast")
	}
}
