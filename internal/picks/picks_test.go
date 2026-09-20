package picks

import (
	"slices"
	"strings"
	"testing"
	"time"

	"gourdian/internal/dota"
	"gourdian/internal/dotadata"
	"gourdian/internal/model"
)

var now = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

// games makes n matches on a hero, wins of them won, the newest ago old.
func games(id int, name string, n, wins int, ago time.Duration) []model.MatchSummary {
	var out []model.MatchSummary
	for i := range n {
		result := "loss"
		if i < wins {
			result = "win"
		}
		out = append(out, model.MatchSummary{
			MatchID: name + string(rune('a'+i)), HeroID: id, Hero: name, Role: dota.Mid,
			Result: result, EndedAt: now.Add(-ago - time.Duration(i)*time.Hour),
		})
	}
	return out
}

func find(list []Hero, name string) (Hero, bool) {
	i := slices.IndexFunc(list, func(h Hero) bool { return h.Name == name })
	if i < 0 {
		return Hero{}, false
	}
	return list[i], true
}

// A long steady record must beat a short hot one: three games at 100% is not evidence.
func TestShrinkageFavoursTheLongerRecord(t *testing.T) {
	in := Input{Role: dota.Mid}
	in.History = append(games(1, "Steady", 40, 26, time.Hour), games(2, "Lucky", 3, 3, time.Hour)...)
	b := Rank(in, now)
	if b == nil || len(b.Best) != 2 {
		t.Fatalf("want both heroes ranked, got %+v", b)
	}
	if b.Best[0].Name != "Steady" {
		t.Errorf("ranked %q above Steady: %d vs %d", b.Best[0].Name, b.Best[0].Score, b.Best[1].Score)
	}
	steady, _ := find(b.Best, "Steady")
	if steady.WinPct != 65 || steady.Games != 40 {
		t.Errorf("record shown as %d%% of %d, want 65%% of 40", steady.WinPct, steady.Games)
	}
}

// The same record counts for less when it is old, and says so.
func TestRecencyDemotesARustyHero(t *testing.T) {
	in := Input{Role: dota.Mid}
	in.History = append(games(1, "Fresh", 10, 7, time.Hour), games(2, "Rusty", 10, 7, 100*24*time.Hour)...)
	b := Rank(in, now)
	fresh, ok := find(b.Best, "Fresh")
	rusty, ok2 := find(b.Best, "Rusty")
	if !ok || !ok2 {
		t.Fatalf("want both heroes, got %+v", b.Best)
	}
	if fresh.Score <= rusty.Score {
		t.Errorf("rusty hero scored %d, fresh one %d", rusty.Score, fresh.Score)
	}
	if !slices.ContainsFunc(rusty.Why, func(s string) bool { return len(s) > 0 && s[0] == 'r' }) {
		t.Errorf("no reason mentions the rust: %q", rusty.Why)
	}
}

// Fewer than MinGames is not a record, so such a hero is not ranked at all.
func TestTooFewGamesIsNotARecord(t *testing.T) {
	in := Input{Role: dota.Mid, History: games(1, "Once", 2, 2, time.Hour)}
	if b := Rank(in, now); b != nil {
		t.Errorf("ranked a hero on 2 games: %+v", b)
	}
}

// A losing hero goes on the list to avoid, not into the ranked pool.
func TestLosingHeroesGoOnTheAvoidList(t *testing.T) {
	in := Input{Role: dota.Mid}
	in.History = append(games(1, "Good", 10, 7, time.Hour), games(2, "Bad", 10, 3, time.Hour)...)
	b := Rank(in, now)
	if _, ok := find(b.Avoid, "Bad"); !ok {
		t.Errorf("Bad is not on the avoid list: %+v", b.Avoid)
	}
	if _, ok := find(b.Best, "Bad"); ok {
		t.Errorf("Bad is also in the ranked pool: %+v", b.Best)
	}
}

// Only matches in the position and matches the player really played count.
func TestOnlyRealGamesInThePositionCount(t *testing.T) {
	in := Input{Role: dota.Mid, History: games(1, "Mid", 5, 4, time.Hour)}
	for i := range in.History {
		in.History[i].Role = dota.Carry
	}
	if b := Rank(in, now); b != nil {
		t.Errorf("counted carry games as mid: %+v", b)
	}
	in.History = games(1, "Mid", 5, 4, time.Hour)
	for i := range in.History {
		in.History[i].Simulated = true
	}
	if b := Rank(in, now); b != nil {
		t.Errorf("counted simulated games: %+v", b)
	}
}

// With no history at all, the meta still has something to say, kept in its own list.
func TestNoHistoryStillSuggestsFromTheMeta(t *testing.T) {
	in := Input{Role: dota.Mid, Rank: 43, // Archon
		Heroes: []dotadata.HeroInfo{{ID: 5, LocalizedName: "Strong"}, {ID: 6, LocalizedName: "Weak"}},
		Meta: map[int]dotadata.HeroMeta{
			5: bracketMeta(4, 1000, 560),
			6: bracketMeta(4, 1000, 440),
		}}
	b := Rank(in, now)
	if b == nil || len(b.Fresh) != 1 || b.Fresh[0].Name != "Strong" {
		t.Fatalf("want only Strong suggested, got %+v", b)
	}
	if !slices.ContainsFunc(b.Fresh[0].Why, func(s string) bool { return s == "meta 56% at Archon" }) {
		t.Errorf("reason doesn't name the bracket: %q", b.Fresh[0].Why)
	}
}

// A hero the player already plays is never offered as one they don't.
func TestFreshSkipsHeroesThePlayerPlays(t *testing.T) {
	in := Input{Role: dota.Mid, Rank: 43, History: games(5, "Strong", 5, 3, time.Hour),
		Heroes: []dotadata.HeroInfo{{ID: 5, LocalizedName: "Strong"}},
		Meta:   map[int]dotadata.HeroMeta{5: bracketMeta(4, 1000, 560)}}
	b := Rank(in, now)
	if len(b.Fresh) != 0 {
		t.Errorf("offered a hero the player already plays: %+v", b.Fresh)
	}
}

// A support is a poor suggestion for a core position, whatever the meta says.
func TestRoleFitPenalisesASupportInACorePosition(t *testing.T) {
	m := bracketMeta(4, 1000, 500)
	m.Roles = []string{"Support", "Disabler"}
	if got := fit(m.Roles, dota.Mid); got != -fitBonus {
		t.Errorf("support at mid scored %d, want %d", got, -fitBonus)
	}
	if got := fit(m.Roles, dota.HardSupport); got != fitBonus {
		t.Errorf("support at position 5 scored %d, want %d", got, fitBonus)
	}
}

// bracketMeta is a hero with picks and wins at one bracket only.
func bracketMeta(bracket, pick, win int) dotadata.HeroMeta {
	var m dotadata.HeroMeta
	m.Pick[bracket], m.Win[bracket] = pick, win
	m.Pick[0], m.Win[0] = pick, win
	return m
}

// A record leans on recent games: the same twenty wins count for much less half a year on,
// though the board still shows every one of them.
func TestOldGamesCountForLessButAreStillShown(t *testing.T) {
	in := Input{Role: dota.Mid}
	in.History = append(games(1, "Recent", 20, 14, time.Hour), games(2, "Faded", 20, 14, 180*24*time.Hour)...)
	b := Rank(in, now)
	recent, ok := find(b.Best, "Recent")
	faded, ok2 := find(b.Best, "Faded")
	if !ok || !ok2 {
		t.Fatalf("want both heroes ranked, got %+v", b.Best)
	}
	if faded.Games != 20 || faded.WinPct != 70 {
		t.Errorf("the board hides the old record: %d%% of %d, want 70%% of 20", faded.WinPct, faded.Games)
	}
	if recent.Score-faded.Score < 5 {
		t.Errorf("half a year made almost no difference: %d against %d", faded.Score, recent.Score)
	}
}

// The window must not be a cliff. By its far end a match is worth so little that cutting it
// off costs nothing, so a hero can't drop off the board the day its games turn too old.
func TestTheFarEndOfTheWindowBarelyCounts(t *testing.T) {
	in := Input{Role: dota.Mid, History: games(1, "Ancient", 30, 30, DefaultTuning().Window()-72*time.Hour)}
	b := Rank(in, now)
	h, ok := find(b.Best, "Ancient")
	if !ok {
		t.Fatalf("board = %+v", b)
	}
	// Thirty straight wins, but a year ago: nothing of the record survives the weighting, so
	// all that is left of the score is the rust.
	if want := even - rustyCap; h.Score != want {
		t.Errorf("score = %d, want %d: a year-old record should count for nothing but the rust", h.Score, want)
	}
}

// However long a hero has been left alone, the fade stops: it means "out of practice", not
// "worse than a hero you lose on".
func TestTheRustPenaltyIsCapped(t *testing.T) {
	in := Input{Role: dota.Mid}
	in.History = append(games(1, "Stale", 5, 3, 250*24*time.Hour), games(2, "Staler", 5, 3, 350*24*time.Hour)...)
	b := Rank(in, now)
	stale, _ := find(b.Best, "Stale")
	staler, _ := find(b.Best, "Staler")
	if stale.Score != staler.Score {
		t.Errorf("the fade kept going: %d after 250 days, %d after 350", stale.Score, staler.Score)
	}
}

// TrustAfter is the knob that decides the short-hot-streak question, so turning it off must
// flip the answer: taken at face value the shorter, better record wins; shrunk, it doesn't.
// (The rates are kept modest so neither record runs into yoursCap, which would hide this.)
func TestTrustAfterDecidesWhetherAStreakWins(t *testing.T) {
	in := Input{Role: dota.Mid}
	in.History = append(games(1, "Steady", 40, 24, time.Hour), games(2, "Lucky", 8, 5, time.Hour)...)

	in.Tuning = DefaultTuning()
	if b := Rank(in, now); b.Best[0].Name != "Steady" {
		t.Errorf("by default %q ranks first, want Steady", b.Best[0].Name)
	}
	in.Tuning.TrustAfter = 0
	if b := Rank(in, now); b.Best[0].Name != "Lucky" {
		t.Errorf("with no shrinkage %q ranks first, want Lucky", b.Best[0].Name)
	}
}

// A longer half-life forgives an older record, which is the whole point of the setting.
func TestHalfLifeDecidesHowFastARecordFades(t *testing.T) {
	in := Input{Role: dota.Mid, Tuning: DefaultTuning()}
	in.History = append(games(1, "Now", 20, 13, time.Hour), games(2, "Then", 20, 15, 120*24*time.Hour)...)

	gap := func(tune Tuning) int {
		in.Tuning = tune
		b := Rank(in, now)
		nowHero, _ := find(b.Best, "Now")
		thenHero, _ := find(b.Best, "Then")
		return nowHero.Score - thenHero.Score
	}
	fast := DefaultTuning()
	slow := DefaultTuning()
	slow.HalfLifeDays = 365
	if gap(fast) <= gap(slow) {
		t.Errorf("a fast fade (%d) didn't punish the old record more than a slow one (%d)", gap(fast), gap(slow))
	}
}

// The lists are sized from the settings, and no meta suggestions at all is a valid choice.
func TestTheListSizesComeFromTheSettings(t *testing.T) {
	in := Input{Role: dota.Mid, Rank: 43, Tuning: DefaultTuning(),
		Heroes: []dotadata.HeroInfo{{ID: 5, LocalizedName: "One"}, {ID: 6, LocalizedName: "Two"}},
		Meta:   map[int]dotadata.HeroMeta{5: bracketMeta(4, 1000, 560), 6: bracketMeta(4, 1000, 550)}}
	in.History = append(games(1, "A", 5, 4, time.Hour), games(2, "B", 5, 3, time.Hour)...)

	in.Tuning.Show, in.Tuning.Fresh = 1, 1
	b := Rank(in, now)
	if len(b.Best) != 1 || len(b.Fresh) != 1 {
		t.Errorf("lists = %d best, %d fresh, want one each", len(b.Best), len(b.Fresh))
	}
	in.Tuning.Fresh = 0
	if b := Rank(in, now); len(b.Fresh) != 0 {
		t.Errorf("meta suggestions were turned off but %d came back", len(b.Fresh))
	}
}

func TestTuningIsChecked(t *testing.T) {
	for name, tune := range map[string]func(*Tuning){
		"no history at all":              func(t *Tuning) { t.Days = 0 },
		"a fade longer than the history": func(t *Tuning) { t.HalfLifeDays = t.Days + 1 },
		"a negative trust":               func(t *Tuning) { t.TrustAfter = -1 },
		"no games needed":                func(t *Tuning) { t.MinGames = 0 },
		"an avoid line above even":       func(t *Tuning) { t.AvoidPct = 60 },
		"a list of eleven":               func(t *Tuning) { t.Show = 11 },
	} {
		tune := tune
		tuning := DefaultTuning()
		tune(&tuning)
		if err := tuning.Validate(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if err := DefaultTuning().Validate(); err != nil {
		t.Errorf("the defaults don't pass their own check: %v", err)
	}
}

// A caller that fills in no tuning gets the defaults rather than nothing.
func TestAnEmptyTuningFallsBackToTheDefaults(t *testing.T) {
	in := Input{Role: dota.Mid, History: games(1, "Steady", 40, 26, time.Hour)}
	if b := Rank(in, now); b == nil || len(b.Best) != 1 {
		t.Fatalf("board = %+v", b)
	}
}

// against builds an enemy's record against other heroes, as OpenDota reports it: from the
// enemy's point of view.
func against(enemy int, wins map[int]int, games int) map[int]map[int]dotadata.Matchup {
	out := map[int]dotadata.Matchup{}
	for hero, pct := range wins {
		out[hero] = dotadata.Matchup{HeroID: hero, Games: games, Wins: games * pct / 100}
	}
	return map[int]map[int]dotadata.Matchup{enemy: out}
}

// A hero the enemy's pick loses to should rise, and one it beats should fall.
func TestCounteringTheEnemyPicks(t *testing.T) {
	in := Input{Role: dota.Mid, Tuning: DefaultTuning()}
	in.History = append(games(1, "Good", 20, 10, time.Hour), games(2, "Bad", 20, 10, time.Hour)...)
	plain := Rank(in, now)
	before := map[string]int{}
	for _, h := range plain.Best {
		before[h.Name] = h.Score
	}

	// Sniper is on the other side; he loses to Good and beats Bad, over plenty of games.
	in.Enemies = []int{35}
	in.Matchups = against(35, map[int]int{1: 35, 2: 65}, 4000)
	after := Rank(in, now)
	good, _ := find(after.Best, "Good")
	bad, _ := find(after.Best, "Bad")
	if good.Score <= before["Good"] {
		t.Errorf("the hero that beats their pick didn't rise: %d then %d", before["Good"], good.Score)
	}
	if bad.Score >= before["Bad"] {
		t.Errorf("the hero their pick beats didn't fall: %d then %d", before["Bad"], bad.Score)
	}
	if !slices.ContainsFunc(good.Why, func(s string) bool { return strings.Contains(s, "against") }) {
		t.Errorf("no reason mentions the matchup: %q", good.Why)
	}
}

// A handful of games between two heroes says nothing, and must move the score by nothing.
func TestAThinMatchupBarelyCounts(t *testing.T) {
	in := Input{Role: dota.Mid, Tuning: DefaultTuning(), History: games(1, "Good", 20, 10, time.Hour)}
	in.Enemies = []int{35}
	in.Matchups = against(35, map[int]int{1: 0}, 5) // five games, all lost by the enemy
	thin, _ := find(Rank(in, now).Best, "Good")
	in.Matchups = against(35, map[int]int{1: 0}, 5000)
	thick, _ := find(Rank(in, now).Best, "Good")
	if thin.Score >= thick.Score {
		t.Errorf("five games counted as much as five thousand: %d against %d", thin.Score, thick.Score)
	}
	if thin.Score > even+2 {
		t.Errorf("five games moved the score to %d", thin.Score)
	}
}

// However lopsided the draft, the matchups may only nudge the order.
func TestCounteringIsCapped(t *testing.T) {
	in := Input{Role: dota.Mid, Tuning: DefaultTuning(), History: games(1, "Good", 20, 10, time.Hour)}
	in.Enemies = []int{35, 26, 14, 8, 11}
	in.Matchups = map[int]map[int]dotadata.Matchup{}
	for _, enemy := range in.Enemies {
		for e, m := range against(enemy, map[int]int{1: 0}, 9000) {
			in.Matchups[e] = m
		}
	}
	h, _ := find(Rank(in, now).Best, "Good")
	if h.Score > even+yoursCap+counterCap+metaCap+fitBonus {
		t.Errorf("score ran away to %d", h.Score)
	}
	if h.Score <= even {
		t.Errorf("a whole enemy team it beats didn't help at all: %d", h.Score)
	}
}

// Without the draft the board says nothing about it, which is the usual case.
func TestNoEnemiesMeansNoMatchupTalk(t *testing.T) {
	in := Input{Role: dota.Mid, Tuning: DefaultTuning(), History: games(1, "Good", 20, 10, time.Hour)}
	b := Rank(in, now)
	if len(b.Enemies) != 0 {
		t.Errorf("enemies appeared from nowhere: %+v", b.Enemies)
	}
	for _, why := range b.Best[0].Why {
		if strings.Contains(why, "against") {
			t.Errorf("a matchup was mentioned with no draft: %q", why)
		}
	}
}

// shaped makes a line-up of heroes with the given roles, and the meta to go with it.
func shaped(start int, roles ...[]string) ([]int, map[int]dotadata.HeroMeta) {
	ids := []int{}
	meta := map[int]dotadata.HeroMeta{}
	for i, r := range roles {
		id := start + i
		ids = append(ids, id)
		m := bracketMeta(4, 1000, 500)
		m.Roles, m.AttackType = r, "Ranged"
		meta[id] = m
	}
	return ids, meta
}

// What a line-up is short of is worth saying, but only once enough of it is known: "nobody
// here can stun" is not a gap when two heroes have been picked.
func TestNotesOnTheShapeOfTheSides(t *testing.T) {
	in := Input{Role: dota.Mid, Tuning: DefaultTuning(), History: games(1, "Mine", 5, 3, time.Hour)}
	allies, meta := shaped(100, []string{"Carry"}, []string{"Nuker"}, []string{"Escape"}, []string{"Pusher"})
	in.Allies, in.Meta = allies, meta
	notes := Rank(in, now).Notes
	if !slices.ContainsFunc(notes, func(s string) bool { return strings.Contains(s, "stun") }) {
		t.Errorf("a side with no disabler drew no note: %q", notes)
	}
	if !slices.ContainsFunc(notes, func(s string) bool { return strings.Contains(s, "beating") }) {
		t.Errorf("a side with nobody durable drew no note: %q", notes)
	}

	// Three picked is not a line-up yet.
	in.Allies = allies[:3]
	if notes := Rank(in, now).Notes; len(notes) != 0 {
		t.Errorf("three heroes were judged as a line-up: %q", notes)
	}
}

func TestNotesOnWhatTheOtherSideBrings(t *testing.T) {
	in := Input{Role: dota.Mid, Tuning: DefaultTuning(), History: games(1, "Mine", 5, 3, time.Hour)}
	enemies, meta := shaped(200, []string{"Disabler"}, []string{"Disabler"}, []string{"Disabler"}, []string{"Carry"})
	in.Enemies, in.Meta = enemies, meta
	notes := Rank(in, now).Notes
	if !slices.ContainsFunc(notes, func(s string) bool { return strings.Contains(s, "3 of them") }) {
		t.Errorf("three disablers drew no note: %q", notes)
	}
	if !slices.ContainsFunc(notes, func(s string) bool { return strings.Contains(s, "ranged") }) {
		t.Errorf("an all-ranged side drew no note: %q", notes)
	}
	// And the heroes themselves carry what they are for.
	if b := Rank(in, now); len(b.Enemies) != 4 || len(b.Enemies[0].Roles) == 0 {
		t.Errorf("the enemy heroes don't say what they are: %+v", b.Enemies)
	}
}

// With no draft in sight, which is the usual case, nothing is said about either side.
func TestNoNotesWithoutADraft(t *testing.T) {
	in := Input{Role: dota.Mid, Tuning: DefaultTuning(), History: games(1, "Mine", 5, 3, time.Hour)}
	if notes := Rank(in, now).Notes; len(notes) != 0 {
		t.Errorf("notes appeared with no draft: %q", notes)
	}
}
