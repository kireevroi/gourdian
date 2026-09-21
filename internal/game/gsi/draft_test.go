package gsi

import (
	"encoding/json"
	"slices"
	"testing"
)

// draftJSON is the shape Dota sends: the slots are named flat inside each team, not as an
// object each, and an empty slot carries id 0.
const draftJSON = `{
  "player": {"team_name": "dire"},
  "draft": {
    "activeteam": 3, "pick": true, "activeteam_time_remaining": 25,
    "team2": {
      "home_team": true,
      "pick0_class": "npc_dota_hero_antimage", "pick0_id": 1,
      "pick1_class": "npc_dota_hero_sniper", "pick1_id": 35,
      "pick2_class": "", "pick2_id": 0,
      "ban0_class": "npc_dota_hero_pudge", "ban0_id": 14
    },
    "team3": {
      "home_team": false,
      "pick0_class": "npc_dota_hero_lion", "pick0_id": 26,
      "pick1_class": "", "pick1_id": 0,
      "ban0_class": "npc_dota_hero_tiny", "ban0_id": 19
    }
  }
}`

func TestDraftBoardReadsTheFlatSlots(t *testing.T) {
	var s State
	if err := json.Unmarshal([]byte(draftJSON), &s); err != nil {
		t.Fatalf("draft payload didn't parse: %v", err)
	}
	ours, theirs, bans, ok := s.DraftBoard()
	if !ok {
		t.Fatal("a board with picks reported nothing")
	}
	// The player is dire, so team3 is ours whatever home_team says.
	if !slices.Equal(ours, []int{26}) {
		t.Errorf("our picks %v, want [26]", ours)
	}
	if !slices.Equal(theirs, []int{1, 35}) {
		t.Errorf("their picks %v, want [1 35] in slot order", theirs)
	}
	if len(bans) != 2 || !slices.Contains(bans, 14) || !slices.Contains(bans, 19) {
		t.Errorf("bans %v, want both teams' bans", bans)
	}
}

func TestDraftBoardDefaultsToRadiant(t *testing.T) {
	var s State
	if err := json.Unmarshal([]byte(draftJSON), &s); err != nil {
		t.Fatal(err)
	}
	s.Player = nil
	// With no player to ask, home_team decides: team2 says it is home, so team2 is ours.
	ours, theirs, _, _ := s.DraftBoard()
	if !slices.Equal(ours, []int{1, 35}) || !slices.Equal(theirs, []int{26}) {
		t.Errorf("ours %v theirs %v, want [1 35] and [26]", ours, theirs)
	}
}

// A player's real feed carries no draft at all, and that must not look like an empty board.
func TestDraftBoardWithoutTheBlock(t *testing.T) {
	var s State
	if err := json.Unmarshal([]byte(`{"player":{"team_name":"radiant"}}`), &s); err != nil {
		t.Fatal(err)
	}
	if _, _, _, ok := s.DraftBoard(); ok {
		t.Error("reported a board with no draft block")
	}
	if err := json.Unmarshal([]byte(`{"draft":{"team2":{"pick0_id":0},"team3":{}}}`), &s); err != nil {
		t.Fatal(err)
	}
	if _, _, _, ok := s.DraftBoard(); ok {
		t.Error("reported a board where every slot is empty")
	}
}

// Dota sends a player "draft": {} all game, and reporting that as a block Dota offers is how
// a reader of the log concludes the draft is there when it never is.
func TestAnEmptyDraftBlockIsNotAnExtra(t *testing.T) {
	for _, payload := range []string{`{"draft":{}}`, `{"draft":{},"map":{"name":"start"}}`} {
		var s State
		if err := json.Unmarshal([]byte(payload), &s); err != nil {
			t.Fatal(err)
		}
		if got := s.Extras(); slices.Contains(got, "draft") {
			t.Errorf("%s was reported as sending a draft block: %v", payload, got)
		}
	}
	var s State
	if err := json.Unmarshal([]byte(draftJSON), &s); err != nil {
		t.Fatal(err)
	}
	if got := s.Extras(); !slices.Contains(got, "draft") {
		t.Errorf("a real draft block went unreported: %v", got)
	}
}
