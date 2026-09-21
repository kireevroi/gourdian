package prompts

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gourdian/internal/ai"
	"gourdian/internal/coaching/coach"
	"gourdian/internal/coaching/picks"
	"gourdian/internal/data/ingest"
	"gourdian/internal/data/opendota"
	"gourdian/internal/game/dota"
	"gourdian/internal/game/gsi"
	"gourdian/internal/game/model"
	"gourdian/internal/sys/config"
)

func TestPromptIncludesLiveStateAndHistory(t *testing.T) {
	in := Input{
		Reason: "regular check-in",
		Role:   "carry",
		Snapshot: coach.Snapshot{
			Clock: 750, Team: "radiant", Daytime: true,
			Hero:   &coach.HeroView{Name: "Anti-Mage", Level: 11, Alive: true, HealthPercent: 64, BuybackCost: 900},
			Player: &gsi.Player{Kills: 2, Deaths: 1, Assists: 4, LastHits: 58, Gold: 1900, GPM: 450},
			Pace:   &coach.Pace{LastHits: 58, Expected: 84, Checkpoint: "15:00", Target: 110},
			Items:  []coach.ItemView{{Slot: "slot0", DName: "Power Treads"}, {Slot: "stash0", DName: "Broadsword"}},
			Build: []coach.BuildView{
				{BuildItem: opendota.BuildItem{DName: "Wraith Band"}, Skipped: true},
				{BuildItem: opendota.BuildItem{DName: "Battle Fury"}, Remaining: 1250, Next: true},
			},
			Timers: []coach.Timer{{Label: "Power rune", At: 840}},
		},
		Tips:     []coach.Tip{{Clock: 700, Text: "No TP scroll. Buy one (100g)"}},
		Timeline: []model.Sample{{Clock: 600, LastHits: 45, GPM: 420}, {Clock: 660, LastHits: 50, GPM: 430}},
		Context: Context{
			History: History{Matches: 8, WinRate: 0.375, AvgDeaths: 7.25, AvgGPM: 402, AvgLH10: 41, Habits: []string{"No TP scroll 2.1"}},
			MMR:     []model.MMREntry{{Date: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), MMR: 2310}},
			Focus:   "Carry a TP scroll at all times",
			Profile: "Archon 3, mostly mid",
		},
	}
	p := Prompt(in)
	for _, want := range []string{
		"Clock 12:30 (day)", "Anti-Mage, level 11", "Last hits 58", "expected 84 by now", "Stash: Broadsword.",
		"Battle Fury (1250g to finish, the player can pay for it now)", "Power rune in 1:30", "[11:40] No TP scroll", "10:00: 45 LH",
		"win rate 38%", "No TP scroll 2.1", "2310 on Sep 1", "last match review: Carry a TP scroll", "themselves: Archon 3, mostly mid",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q:\n%s", want, p)
		}
	}
	if strings.Contains(p, "Wraith Band") {
		t.Error("skipped build items should not be suggested")
	}
}

func TestReviewPrompt(t *testing.T) {
	p := ReviewPrompt(ReviewInput{
		Match: model.MatchSummary{Hero: "Lina", Role: "mid", Team: "dire", Result: "loss", DurationSec: 2100, Deaths: 9,
			LastHitsAt: map[string]int{"10:00": 38}, DeathClocks: []int{312, 1500}},
		Targets:  map[string]int{"10:00": 60},
		Timeline: []model.Sample{{Clock: 60}, {Clock: 120}, {Clock: 360, LastHits: 20}},
		Warnings: []string{"No TP scroll ×3"},
	})
	for _, want := range []string{"Lina as mid (dire), loss after 35:00", "10:00 38 (target 60)", "Died at: 5:12, 25:00", "No TP scroll ×3", "6:00: 20 LH"} {
		if !strings.Contains(p, want) {
			t.Errorf("review prompt missing %q:\n%s", want, p)
		}
	}
}

func TestReviewPromptWithParsedReplay(t *testing.T) {
	detail := &ingest.Detail{
		PlayerDetail: opendota.PlayerDetail{Parsed: true, NetWorth: 14200, NetWorthRank: 2, HeroDamage: 21000, ObsPlaced: 3,
			TeamfightParticipation: 0.61, Percentiles: map[string]float64{"gold_per_min": 0.34, "deaths_per_min": 0.81}},
		LaneRoleName: "mid lane", LaneOpponents: []string{"Storm Spirit"}, Allies: []string{"Axe"}, Enemies: []string{"Storm Spirit", "Lion"},
		CoreItems: []ingest.ItemTime{{Name: "Black King Bar", Time: 1150}},
	}
	p := ReviewPrompt(ReviewInput{Match: model.MatchSummary{Hero: "Lina", Role: "mid"}, Detail: detail})
	for _, want := range []string{"played mid lane against Storm Spirit", "Enemies: Storm Spirit, Lion", "Net worth 14200 (#2 on the team)",
		"teamfight participation 61%", "GPM 34", "deaths (higher means dying more) 81", "Black King Bar 19:10"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q:\n%s", want, p)
		}
	}
	p = ReviewPrompt(ReviewInput{Match: model.MatchSummary{Hero: "Lina"}, Detail: &ingest.Detail{}})
	if !strings.Contains(p, "hadn't parsed the replay") {
		t.Errorf("unparsed replay should be called out:\n%s", p)
	}
}

func TestPromptHandlesEmptySnapshot(t *testing.T) {
	if p := Prompt(Input{Reason: "test"}); !strings.Contains(p, "Items: none.") {
		t.Fatalf("unexpected prompt for empty input:\n%s", p)
	}
}

// fakeProvider answers with canned JSON or an error.
type fakeProvider struct {
	answer string
	err    error
	got    ai.Request
}

func (f *fakeProvider) Info() ai.Info                    { return ai.Info{ID: "fake", Name: "Fake"} }
func (f *fakeProvider) Status(context.Context) ai.Status { return ai.Status{State: ai.StateReady} }
func (f *fakeProvider) Complete(_ context.Context, req ai.Request) (json.RawMessage, error) {
	f.got = req
	return json.RawMessage(f.answer), f.err
}

func TestSuggestParsesAnswer(t *testing.T) {
	set := config.Default().Settings.AI
	set.Instructions = "Be blunt."
	p := &fakeProvider{answer: `{"tips":["Buy Battle Fury now."," "]}`}
	tips, err := Suggest(t.Context(), p, set.Live, set, "en", "prompt")
	if err != nil || len(tips) != 1 || tips[0] != "Buy Battle Fury now." {
		t.Fatalf("Suggest = %q, %v", tips, err)
	}
	if p.got.Model != "sonnet" || p.got.Effort != "low" || !strings.Contains(p.got.System, "Be blunt.") || p.got.Schema != liveSchema {
		t.Fatalf("request = %+v", p.got)
	}
	p = &fakeProvider{err: &ai.Error{Kind: ai.ErrAuth, Msg: "Not logged in"}}
	if _, err := Suggest(t.Context(), p, set.Live, set, "en", "prompt"); ai.KindOf(err) != ai.ErrAuth {
		t.Fatalf("want the provider's login error, got %v", err)
	}
	p = &fakeProvider{answer: `{"summary":"x","strengths":[],"improve":[],"next_game_focus":""}`}
	if _, err := RequestReview(t.Context(), p, set.Reviews, set, "en", "prompt", []string{"deaths"}); err == nil {
		t.Fatal("an empty review should be rejected")
	}
	p = &fakeProvider{answer: `{"summary":"x","strengths":[],"improve":["a","b","c"],"next_game_focus":"Ward",
		"goals":[{"metric":"deaths","comparator":"at_most","target":6,"label":"6 deaths or fewer"},{"metric":"networth","comparator":"at_least","target":1,"label":"x"}]}`}
	if !json.Valid([]byte(reviewSchema([]string{"deaths"}))) {
		t.Fatal("review schema isn't valid JSON")
	}
	r, err := RequestReview(t.Context(), p, set.Reviews, set, "en", "prompt", []string{"deaths", "lh_10"})
	if err != nil || len(r.Goals) != 1 || r.Goals[0].Metric != "deaths" || !strings.Contains(p.got.Schema, `"enum":["deaths","lh_10"]`) {
		t.Fatalf("review goals = %+v, %v, schema %s", r.Goals, err, p.got.Schema)
	}
}

func TestRussianAsksForRussianAnswers(t *testing.T) {
	system := withInstructions("Be a coach.", config.AISettings{Instructions: "Be blunt."}, "ru")
	if !strings.Contains(system, "Answer in Russian") || !strings.Contains(system, "Be blunt.") {
		t.Fatalf("system prompt = %q", system)
	}
	if english := withInstructions("Be a coach.", config.AISettings{}, "en"); strings.Contains(english, "Russian") {
		t.Fatalf("english prompt mentions Russian: %q", english)
	}
}

func TestPromptsCarryTheProfessionalBuild(t *testing.T) {
	facts := &HeroFacts{Name: "Storm Spirit", Roles: []string{"Carry", "Escape", "Nuker"},
		Build: map[string][]string{"early": {"Bottle", "Power Treads"}, "mid": {"Orchid Malevolence", "Kaya and Sange"}},
		Owned: []string{"Bottle"}}
	live := Prompt(Input{Context: Context{Hero: facts}, Reason: "test"})
	for _, want := range []string{"Storm Spirit's roles: Carry, Escape, Nuker.", "Professional mid-game items for Storm Spirit from up to 100 recent professional games in every position, won or lost, in the order they buy them: Orchid Malevolence, Kaya and Sange.", "The player has: Bottle."} {
		if !strings.Contains(live, want) {
			t.Errorf("live prompt is missing %q:\n%s", want, live)
		}
	}
	if !strings.Contains(liveSystemPrompt, "Recommend only items from that build that suit the player's position") {
		t.Error("the live instructions no longer hold the coach to the professional build")
	}
	review := ReviewPrompt(ReviewInput{Context: Context{Hero: facts}})
	if !strings.Contains(review, "Professional early-game items for Storm Spirit") {
		t.Errorf("review prompt is missing the build:\n%s", review)
	}
	facts.BuildPosition, facts.BuildGames = 2, 1067
	if mid := Prompt(Input{Context: Context{Hero: facts}, Role: "mid"}); !strings.Contains(mid, "items for Storm Spirit as mid (position 2), from 1067 professional games") {
		t.Errorf("the prompt should say the build is from the player's position:\n%s", mid)
	}
}

func TestEveryRequestOpensWithHeroAndPosition(t *testing.T) {
	facts := &HeroFacts{Name: "Chen", Roles: []string{"Support"}, Build: map[string][]string{"mid": {"Drum of Endurance"}}}
	live := Prompt(Input{Reason: "test", Role: "carry", Context: Context{Hero: facts}})
	want := "The player is playing Chen as carry (position 1)."
	if !strings.HasPrefix(live, want) {
		t.Errorf("live prompt should open with %q:\n%s", want, live)
	}
	review := ReviewPrompt(ReviewInput{Match: model.MatchSummary{Hero: "Chen", Role: "soft_support"}})
	if want := "The player played Chen as soft support (position 4)."; !strings.HasPrefix(review, want) {
		t.Errorf("review prompt should open with %q:\n%s", want, review)
	}
	for name, system := range map[string]string{"live": liveSystemPrompt, "review": reviewSystemPrompt} {
		if !strings.Contains(system, "suit the player's position") {
			t.Errorf("the %s instructions don't hold items to the player's position", name)
		}
	}
}

func TestPromptCarriesTheMatchFacts(t *testing.T) {
	in := Input{Reason: "regular check-in", Role: "mid",
		Snapshot: coach.Snapshot{Clock: 828, Team: "radiant", Hero: &coach.HeroView{Name: "Storm Spirit", Level: 11, Alive: true}},
		Facts: coach.MatchFacts{TeamKills: 12, EnemyKills: 20, Deaths: []int{483, 644}, LostBuildings: []string{"mid tier 1 tower"},
			Abilities: []coach.AbilityFact{{Name: "Ball Lightning", Level: 2, Ready: true, Ultimate: true}, {Name: "Electric Vortex", Level: 1, Cooldown: 8}},
			Parts: []coach.PartsFact{{Item: "Aghanim's Scepter", Have: []string{"Point Booster", "Ogre Axe", "Staff of Wizardry"},
				Missing: []string{"Blade of Alacrity (1000g)"}, Left: 1000}}},
		Tips: []coach.Tip{{Rule: "no_tp", Clock: 800, Text: "No TP scroll"}, {Rule: "ai", Clock: 600, Text: "Farm the mid wave"}},
	}
	p := Prompt(in)
	for _, want := range []string{"mid game", "Kills: the player's team 12, the enemy 20.", "lost: mid tier 1 tower", "Died at: 8:03, 10:44.",
		"Ball Lightning level 2, ready, ultimate", "Electric Vortex level 1, on cooldown 8s", "Stash: empty.", "doesn't carry the Aegis",
		"Toward Aghanim's Scepter the player has Point Booster, Ogre Axe, Staff of Wizardry; missing Blade of Alacrity (1000g); 1000g to finish.",
		"alerts in the last 5 minutes, don't repeat them: [13:20] No TP scroll", "Your earlier advice this match, don't repeat it unless something changed: [10:00] Farm the mid wave"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q:\n%s", want, p)
		}
	}
	set := config.Default().Settings.AI
	if tips, err := Suggest(t.Context(), &fakeProvider{answer: `{"tips":[]}`}, set.Live, set, "en", p); err != nil || len(tips) != 0 {
		t.Fatalf("nothing new to say is a fine answer: %q, %v", tips, err)
	}
}

// The coach must not be told it is blind to the draft when the player has turned on reading
// it off their screen, nor told it can see one when it can't.
func TestTheDraftPromptSaysWhetherTheEnemyIsKnown(t *testing.T) {
	board := &picks.Board{Role: dota.Mid, Best: []picks.Hero{{Name: "Puck", Games: 9, WinPct: 60}}}
	blind := DraftPrompt(DraftInput{Role: dota.Mid, Board: board})
	if strings.Contains(blind, "has taken") {
		t.Errorf("the prompt talks about enemy picks with none known:\n%s", blind)
	}
	board.Enemies = []picks.Hero{{Name: "Sniper"}, {Name: "Lina"}}
	seeing := DraftPrompt(DraftInput{Role: dota.Mid, Board: board})
	if !strings.Contains(seeing, "The other team has taken: Sniper, Lina.") {
		t.Errorf("the prompt doesn't name the enemy picks:\n%s", seeing)
	}
	if !strings.Contains(blindToTheDraft, "NOT given") || strings.Contains(seesTheDraft, "NOT given") {
		t.Error("the two sets of instructions say the same thing about the draft")
	}
}

// The model has told a player to buy an item they were 470 gold short of, and one the shop
// doesn't sell before 15:00, so the prompt does that arithmetic and says so outright.
func TestPromptSaysWhatThePlayerCannotBuyYet(t *testing.T) {
	in := Input{
		Reason:  "regular check-in",
		Role:    "offlane",
		Timings: dota.DefaultTimings(),
		Snapshot: coach.Snapshot{
			Clock:  534,
			Player: &gsi.Player{Gold: 930},
			Build: []coach.BuildView{
				{BuildItem: opendota.BuildItem{Name: dota.ShardItem, DName: "Aghanim's Shard"}, Remaining: 1400, Next: true},
				{BuildItem: opendota.BuildItem{Name: "black_king_bar", DName: "Black King Bar"}, Remaining: 4050},
			},
		},
	}
	p := Prompt(in)
	for _, want := range []string{
		"Aghanim's Shard (1400g to finish, 470g short)",
		"Black King Bar (4050g to finish, 3120g short)",
		"Aghanim's Shard goes on sale at 15:00 and cannot be bought before then.",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q:\n%s", want, p)
		}
	}
	in.Snapshot.Clock = 1200
	if p := Prompt(in); strings.Contains(p, "cannot be bought before then") {
		t.Errorf("past 15:00 the Shard is on sale:\n%s", p)
	}
}

// The trainer puts its own item goals on the player's screen, so the coach is told what they
// are and cannot quietly send the player after a third item instead.
func TestPromptCarriesTheTrainersItemGoals(t *testing.T) {
	in := Input{
		Reason: "regular check-in",
		Role:   "offlane",
		Snapshot: coach.Snapshot{
			Clock: 780, ItemGames: 6,
			Player: &gsi.Player{Gold: 400},
			ItemGoals: []coach.ItemGoalView{
				{ItemGoal: coach.ItemGoal{Item: "blade_mail", Name: "Blade Mail", By: 900}, Remaining: 2084},
				{ItemGoal: coach.ItemGoal{Item: "black_king_bar", Name: "Black King Bar", By: 1800}, Owned: true, At: 1500},
			},
		},
	}
	p := Prompt(in)
	for _, want := range []string{
		"from the core items the player finished first in their last 6 games on this hero",
		"Blade Mail by 15:00 (2084g to finish)",
		"Black King Bar by 30:00, bought at 25:00",
		"name the item you would buy instead and say why it beats them",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q:\n%s", want, p)
		}
	}
}
