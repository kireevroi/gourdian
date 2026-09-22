package position

import (
	"sync"
	"testing"
	"time"

	"gourdian/internal/game/dota"
)

var now = time.Date(2026, 9, 21, 18, 0, 0, 0, time.UTC)

// Only a position the trainer wrote over the player's head stops being their answer, and only
// while the settings still hold it.
func TestOnlyTheTrainersOwnGuessIsNotTheAnswer(t *testing.T) {
	var m Memory
	if !m.Mine(dota.Mid) {
		t.Fatal("with nothing guessed, the settings hold the player's own answer")
	}
	if m.Mine("") {
		t.Fatal("no position at all is nobody's answer")
	}
	m.Guessed(dota.Offlane)
	if m.Mine(dota.Offlane) {
		t.Fatal("the trainer's own guess must not pass for the player's answer")
	}
	if !m.Mine(dota.Mid) {
		t.Fatal("a guess says nothing about a different position")
	}
	m.Own()
	if !m.Mine(dota.Offlane) {
		t.Fatal("the player naming a position settles it")
	}
}

func TestAPickStandsForTheLengthOfADraft(t *testing.T) {
	var m Memory
	if m.Picked(now) {
		t.Fatal("nothing picked yet")
	}
	m.Choose(dota.Mid, now)
	if !m.Picked(now.Add(time.Minute)) {
		t.Fatal("a pick a minute old still stands")
	}
	if m.Picked(now.Add(time.Hour)) {
		t.Fatal("an abandoned draft must not claim a game an hour later")
	}
}

func TestTheDraftPickIsHandedOverOnceAndCleared(t *testing.T) {
	var m Memory
	m.Choose(dota.Mid, now)

	got := ""
	m.OnNewHero(13, now, func(chosen string) string { got = chosen; return "" })
	if got != dota.Mid {
		t.Fatalf("settle got %q, want the draft pick", got)
	}
	if m.Picked(now) {
		t.Fatal("taking the pick should clear it")
	}

	got = "untouched"
	m.OnNewHero(99, now, func(chosen string) string { got = chosen; return "" })
	if got != "" {
		t.Fatalf("the next hero got %q, want nothing left to hand over", got)
	}
}

func TestAStalePickIsNotHandedOver(t *testing.T) {
	var m Memory
	m.Choose(dota.Mid, now)
	got := "untouched"
	m.OnNewHero(13, now.Add(time.Hour), func(chosen string) string { got = chosen; return "" })
	if got != "" {
		t.Fatalf("settle got %q, want the stale pick withheld", got)
	}
}

func TestAHeroIsSettledOnlyOnce(t *testing.T) {
	var m Memory
	calls := 0
	m.OnNewHero(13, now, func(string) string { calls++; return "" })
	m.OnNewHero(13, now, func(string) string { calls++; return "" })
	if calls != 1 {
		t.Fatalf("settled %d times, want once per hero", calls)
	}
	m.OnNewHero(99, now, func(string) string { calls++; return "" })
	if calls != 2 {
		t.Fatalf("settled %d times, want a new hero to settle again", calls)
	}
}

func TestTakeSettledReportsAndForgets(t *testing.T) {
	var m Memory
	m.OnNewHero(13, now, func(string) string { return "" })
	if got := m.TakeSettled(); got != 13 {
		t.Fatalf("TakeSettled = %d, want the settled hero", got)
	}
	calls := 0
	m.OnNewHero(13, now, func(string) string { calls++; return "" })
	if calls != 1 {
		t.Fatal("after forgetting, the same hero settles again")
	}
}

func TestTheMatchLockIsPerMatch(t *testing.T) {
	var m Memory
	if m.Locked("7001") {
		t.Fatal("nothing locked yet")
	}
	m.Lock("7001")
	if !m.Locked("7001") || m.Locked("7002") {
		t.Fatal("the lock belongs to one match")
	}
	if m.Locked("") {
		t.Fatal("no match id can never be locked")
	}
}

// settle must not run for two heroes at once, or one hero's position could be written over
// another's. Run with -race.
func TestOnlyOneHeroSettlesAtATime(t *testing.T) {
	var m Memory
	var inside, most int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for hero := range 8 {
		wg.Go(func() {
			m.OnNewHero(hero+1, now, func(string) string {
				mu.Lock()
				inside++
				most = max(most, inside)
				mu.Unlock()
				time.Sleep(time.Millisecond)
				mu.Lock()
				inside--
				mu.Unlock()
				return ""
			})
		})
	}
	wg.Wait()
	if most != 1 {
		t.Fatalf("%d settles overlapped, want them serialized", most)
	}
}
