package tilt

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"gourdian/internal/game/model"
)

var base = time.Date(2026, 9, 17, 18, 0, 0, 0, time.UTC)

func game(minutes int, result string) model.MatchSummary {
	return model.MatchSummary{MatchID: fmt.Sprint(minutes), Result: result, Source: model.SourceLive,
		EndedAt: base.Add(time.Duration(minutes) * time.Minute)}
}

func TestWhenASessionCallsForABreak(t *testing.T) {
	cases := []struct {
		name    string
		matches []model.MatchSummary
		mmr     []model.MMREntry
		want    string
	}{
		{"nothing played", nil, nil, ""},
		{"one loss is not a run", []model.MatchSummary{game(0, "loss")}, nil, ""},
		{"two losses", []model.MatchSummary{game(0, "win"), game(45, "loss"), game(90, "loss")}, nil, "Two losses in a row"},
		{"three losses", []model.MatchSummary{game(0, "loss"), game(45, "loss"), game(90, "loss")}, nil, "Three losses in a row"},
		{"gap ends the session", []model.MatchSummary{game(0, "loss"), game(300, "loss")}, nil, ""},
		{"three of four", []model.MatchSummary{game(0, "loss"), game(40, "loss"), game(80, "win"), game(120, "loss")}, nil, "Three of your last four"},
		{"three of four needs the last one lost", []model.MatchSummary{game(0, "loss"), game(40, "loss"), game(80, "loss"), game(120, "win")}, nil, ""},
		{"win breaks it", []model.MatchSummary{game(0, "loss"), game(40, "loss"), game(80, "win")}, nil, ""},
		{"mmr drop", []model.MatchSummary{game(0, "win"), game(40, "loss")},
			[]model.MMREntry{{Date: base, MMR: 3000}, {Date: base.Add(50 * time.Minute), MMR: 2940}}, "down 60 MMR"},
		{"small mmr drop is ignored", []model.MatchSummary{game(0, "win"), game(40, "loss")},
			[]model.MMREntry{{Date: base, MMR: 3000}, {Date: base.Add(50 * time.Minute), MMR: 2980}}, ""},
		{"mmr drop needs the last game lost", []model.MatchSummary{game(0, "loss"), game(40, "win")},
			[]model.MMREntry{{Date: base, MMR: 3000}, {Date: base.Add(50 * time.Minute), MMR: 2900}}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Reason(c.matches, c.mmr, "en")
			if c.want == "" && got != "" || !strings.Contains(got, c.want) {
				t.Fatalf("Reason = %q, want %q", got, c.want)
			}
		})
	}
}

func TestOnlyDecidedRealMatchesCount(t *testing.T) {
	practice := game(45, "loss")
	practice.Source = model.SourcePractice
	simulated := game(90, "loss")
	simulated.Simulated = true
	for _, skipped := range []model.MatchSummary{game(45, "unknown"), practice, simulated} {
		matches := []model.MatchSummary{game(0, "loss"), skipped}
		if got := Reason(matches, nil, "en"); got != "" {
			t.Errorf("%+v should not build a run with a real loss: %q", skipped, got)
		}
	}
}

func TestTheOrderMatchesArriveInDoesNotMatter(t *testing.T) {
	forwards := []model.MatchSummary{game(0, "win"), game(45, "loss"), game(90, "loss")}
	backwards := []model.MatchSummary{game(90, "loss"), game(45, "loss"), game(0, "win")}
	if Reason(forwards, nil, "en") != Reason(backwards, nil, "en") {
		t.Fatal("matches are sorted by end time before the run is read")
	}
}

func TestTheReasonIsWordedInThePlayersLanguage(t *testing.T) {
	matches := []model.MatchSummary{game(0, "loss"), game(45, "loss")}
	en, ru := Reason(matches, nil, "en"), Reason(matches, nil, "ru")
	if en == "" || ru == "" {
		t.Fatalf("both languages should give a reason: en=%q ru=%q", en, ru)
	}
	if en == ru {
		t.Fatalf("Russian should not read as English: %q", ru)
	}
}
