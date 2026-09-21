package focus

import (
	"testing"
	"time"

	"gourdian/internal/dota"
	"gourdian/internal/model"
)

func reviewed(rs ...model.Review) []model.Review {
	day := time.Date(2026, 9, 17, 18, 0, 0, 0, time.UTC)
	for i := range rs {
		rs[i].Date = day.Add(time.Duration(i) * time.Hour)
	}
	return rs
}

var history = reviewed(
	model.Review{MatchID: "1", Hero: "Lion", HeroID: 26, Role: dota.HardSupport, NextGameFocus: "Stack the ancient camp every minute"},
	model.Review{MatchID: "2", Hero: "Puck", HeroID: 13, Role: dota.Mid, NextGameFocus: "Hit 60 last hits by 10:00"},
	model.Review{MatchID: "3", Hero: "Storm Spirit", HeroID: 17, Role: dota.Mid, NextGameFocus: "Leave lane with a bottle full"},
)

func TestTheSameHeroAndPositionWins(t *testing.T) {
	if got := Pick(history, dota.Mid, 13, "en"); got != "Hit 60 last hits by 10:00" {
		t.Fatalf("mid on Puck got %q", got)
	}
}

func TestANewHeroFallsBackToThePosition(t *testing.T) {
	if got := Pick(history, dota.Mid, 99, "en"); got != "Leave lane with a bottle full" {
		t.Fatalf("mid on a new hero got %q, want the latest mid review", got)
	}
}

func TestAPositionWithNoReviewBorrowsAndSaysSo(t *testing.T) {
	want := "Leave lane with a bottle full (from your Storm Spirit game as mid)"
	if got := Pick(history, dota.Carry, 99, "en"); got != want {
		t.Fatalf("carry got %q, want %q", got, want)
	}
}

func TestABorrowedFocusIsWordedInThePlayersLanguage(t *testing.T) {
	en := Pick(history, dota.Carry, 99, "en")
	ru := Pick(history, dota.Carry, 99, "ru")
	if ru == "" || ru == en {
		t.Fatalf("Russian should not read as English: %q", ru)
	}
}

func TestNoReviewsMeansNoFocus(t *testing.T) {
	if got := Pick(nil, dota.Mid, 13, "en"); got != "" {
		t.Fatalf("Pick = %q, want nothing to work on yet", got)
	}
}

func TestABlankFocusIsNotOffered(t *testing.T) {
	blank := reviewed(model.Review{MatchID: "1", Hero: "Lion", HeroID: 26, Role: dota.Mid, NextGameFocus: "   "})
	if got := Pick(blank, dota.Mid, 26, "en"); got != "" {
		t.Fatalf("Pick = %q, want a blank focus ignored", got)
	}
}

func TestAReviewWithoutAPositionCountsForAny(t *testing.T) {
	loose := reviewed(model.Review{MatchID: "1", Hero: "Lion", HeroID: 26, NextGameFocus: "Ward the high ground"})
	if got := Pick(loose, dota.Carry, 99, "en"); got != "Ward the high ground" {
		t.Fatalf("Pick = %q, want the position-less review used as it is", got)
	}
}

func TestTheLatestReviewForThePositionWins(t *testing.T) {
	if got := Last(history, dota.Mid, 13); got != "Leave lane with a bottle full" {
		t.Fatalf("Last = %q, want the newest mid review", got)
	}
	if got := Last(history, dota.HardSupport, 26); got != "Stack the ancient camp every minute" {
		t.Fatalf("Last = %q", got)
	}
	if got := Last(nil, dota.Mid, 13); got != "" {
		t.Fatalf("Last = %q, want nothing without reviews", got)
	}
}
