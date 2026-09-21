package server

import (
	"fmt"
	"strings"
	"testing"

	"gourdian/internal/coaching/coach"
	"gourdian/internal/game/model"
)

// The briefing was built with fmt.Sprintf and reached Russian players in English: a real one
// read "Dawnbreaker: aim for 40 last hits at 10:00 · Blade Mail by 15:00 · goal: 45 добиваний
// к 10:00", the drill's own label being the only Russian in it.
func TestBriefingIsSpokenInThePlayersLanguage(t *testing.T) {
	b := &coach.Briefing{
		Hero:     "Dawnbreaker",
		Target10: 40,
		Items:    []coach.ItemGoal{{Name: "Blade Mail", By: 900}},
		Goals:    []model.GoalProgress{{Goal: model.Goal{Label: "45 добиваний к 10:00"}, Met: 1}},
	}
	text, speech := briefingWords(b, "ru")
	want := fmt.Sprintf("Dawnbreaker: цель — 40 добиваний к 10:00 · Blade Mail к 15:00 · цель: 45 добиваний к 10:00 (1/%d)", model.GoalsDone)
	if text != want {
		t.Errorf("text = %q, want %q", text, want)
	}
	for _, en := range []string{"aim for", "last hits", "by 15:00", "goal:"} {
		if strings.Contains(text, en) || strings.Contains(speech, en) {
			t.Errorf("English %q left in %q / %q", en, text, speech)
		}
	}
	if text, _ := briefingWords(b, "en"); !strings.Contains(text, "aim for 40 last hits at 10:00") {
		t.Errorf("English briefing = %q", text)
	}
}
