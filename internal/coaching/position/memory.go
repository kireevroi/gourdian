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
	guessed  string
}

func (m *Memory) Choose(role string, now time.Time) {
	m.mu.Lock()
	m.chosen, m.chosenAt = role, now
	m.mu.Unlock()
}

// Own is the player naming their position, wherever they name it and whenever.
func (m *Memory) Own() {
	m.mu.Lock()
	m.guessed = ""
	m.mu.Unlock()
}

func (m *Memory) Guessed(role string) {
	m.mu.Lock()
	m.guessed = role
	m.mu.Unlock()
}

// Mine reports whether role is the player's own answer rather than one the trainer worked out
// from the hero or the lane. Which heroes are worth taking depends on the position entirely.
func (m *Memory) Mine(role string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return role != "" && m.guessed != role
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
// position over a newer hero's. It returns any position it worked out for itself.
func (m *Memory) OnNewHero(heroID int, now time.Time, settle func(chosen string) string) {
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
	if guessed := settle(chosen); guessed != "" {
		m.guessed = guessed
	}
}

// TakeSettled reports the hero last settled for and forgets it, so the next post settles again.
func (m *Memory) TakeSettled() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	hero := m.hero
	m.hero = 0
	return hero
}
