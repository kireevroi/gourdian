package position

import (
	"sync"
	"time"
)

// chosenFor bounds the wait, so an abandoned draft can't claim a game an hour later.
const chosenFor = 10 * time.Minute

// Memory is what the player has settled about their position, and for which hero and match.
type Memory struct {
	mu       sync.Mutex
	hero     int
	chosen   string
	chosenAt time.Time
	locked   string
}

func (m *Memory) Choose(role string, now time.Time) {
	m.mu.Lock()
	m.chosen, m.chosenAt = role, now
	m.mu.Unlock()
}

func (m *Memory) Picked(now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.standing(now)
}

func (m *Memory) standing(now time.Time) bool {
	return m.chosen != "" && now.Sub(m.chosenAt) < chosenFor
}

func (m *Memory) Lock(matchID string) {
	m.mu.Lock()
	m.locked = matchID
	m.mu.Unlock()
}

func (m *Memory) Locked(matchID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return matchID != "" && m.locked == matchID
}

// settle runs while the memory is held, so a post about an older hero cannot write its
// position over a newer hero's. It is handed the draft pick, and taking it clears it.
func (m *Memory) OnNewHero(heroID int, now time.Time, settle func(chosen string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hero == heroID {
		return
	}
	m.hero = heroID
	chosen := ""
	if m.standing(now) {
		chosen, m.chosen = m.chosen, ""
	}
	settle(chosen)
}

// TakeSettled reports the hero last settled for and forgets it, so the next post settles again.
func (m *Memory) TakeSettled() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	hero := m.hero
	m.hero = 0
	return hero
}
