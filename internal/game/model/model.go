// Package model is the trainer's record of play: matches, their timelines, tips, items, MMR,
// reviews and weekly goals. stats stores them; the coach, the HUD and the AI read them without
// depending on storage.
package model

import "time"

type MatchSummary struct {
	MatchID     string         `json:"match_id"`
	HeroID      int            `json:"hero_id"`
	Hero        string         `json:"hero"`
	Role        string         `json:"role"`
	Team        string         `json:"team"`
	Result      string         `json:"result"`
	EndedAt     time.Time      `json:"ended_at"`
	DurationSec int            `json:"duration_sec"`
	Kills       int            `json:"kills"`
	Deaths      int            `json:"deaths"`
	Assists     int            `json:"assists"`
	LastHits    int            `json:"last_hits"`
	Denies      int            `json:"denies"`
	GPM         int            `json:"gpm"`
	XPM         int            `json:"xpm"`
	LastHitsAt  map[string]int `json:"last_hits_at"`
	DeathClocks []int          `json:"death_clocks"`
	// LastHitsByMinute is OpenDota's per-minute count, for matches the trainer didn't sample live.
	LastHitsByMinute []int          `json:"-"`
	TipCounts        map[string]int `json:"tip_counts"`
	RankTier         int            `json:"rank_tier,omitempty"`
	// GameMode is OpenDota's mode number, 0 until it says; kept since Turbo pays about double.
	GameMode  int    `json:"game_mode,omitempty"`
	Simulated bool   `json:"simulated,omitempty"`
	Ranked    bool   `json:"ranked,omitempty"`
	Source    string `json:"source,omitempty"`

	// Filled in from OpenDota once the replay is parsed.
	Parsed                 bool     `json:"parsed,omitempty"`
	LaneRole               int      `json:"lane_role,omitempty"`
	NetWorth               int      `json:"net_worth,omitempty"`
	HeroDamage             int      `json:"hero_damage,omitempty"`
	TowerDamage            int      `json:"tower_damage,omitempty"`
	ObsPlaced              int      `json:"obs_placed,omitempty"`
	SenPlaced              int      `json:"sen_placed,omitempty"`
	CampsStacked           int      `json:"camps_stacked,omitempty"`
	TeamfightParticipation float64  `json:"teamfight_participation,omitempty"`
	GPMPct                 float64  `json:"gpm_pct,omitempty"`
	LHPct                  float64  `json:"lh_pct,omitempty"`
	HeroDamagePct          float64  `json:"hero_damage_pct,omitempty"`
	EnemyHeroes            []string `json:"enemy_heroes,omitempty"`

	// Items bought during a live match, saved to items.csv rather than matches.csv.
	Items []ItemTiming `json:"-"`
}

// GameModeTurbo is the mode that pays double, as OpenDota numbers the modes.
const GameModeTurbo = 23

// Turbo reports a Turbo match, which pays about double, so history leaves it out. A match
// OpenDota hasn't described yet counts as normal, as nearly all are.
func (m MatchSummary) Turbo() bool { return m.GameMode == GameModeTurbo }

const (
	SourceLive     = "live"
	SourceOpenDota = "opendota"
	SourceSim      = "sim"
	SourcePractice = "practice" // lobby and bot games
	// SourceGSI marks item timings the trainer saw live, as opposed to OpenDota's.
	SourceGSI = "gsi"
)

// Real reports whether a match counts toward trends: not simulated and not practice.
func (m MatchSummary) Real() bool { return !m.Simulated && m.Source != SourcePractice }

// Coached is a match the trainer watched, so it has tips; imported history has only the scoreboard.
func (m MatchSummary) Coached() bool { return m.Source != SourceOpenDota }

type ItemTiming struct {
	MatchID string `json:"match_id"`
	Hero    string `json:"hero"`
	Item    string `json:"item"`
	Time    int    `json:"time"`
	Source  string `json:"source"`
}

type Sample struct {
	MatchID  string `json:"match_id"`
	Clock    int    `json:"clock"`
	Gold     int    `json:"gold"`
	GPM      int    `json:"gpm"`
	XPM      int    `json:"xpm"`
	LastHits int    `json:"last_hits"`
	Denies   int    `json:"denies"`
	Kills    int    `json:"kills"`
	Deaths   int    `json:"deaths"`
	Assists  int    `json:"assists"`
	Level    int    `json:"level"`
	Alive    bool   `json:"alive"`
}

type TipRecord struct {
	At       time.Time
	MatchID  string
	Clock    int
	Rule     string
	Category string
	Severity string
	Habit    bool
	Text     string
}

type MMREntry struct {
	Date    time.Time `json:"date"`
	MMR     int       `json:"mmr"`
	Note    string    `json:"note,omitempty"`
	MatchID string    `json:"match_id,omitempty"`
}

type Review struct {
	Date          time.Time `json:"date"`
	MatchID       string    `json:"match_id"`
	Hero          string    `json:"hero"`
	HeroID        int       `json:"hero_id,omitempty"`
	Role          string    `json:"role,omitempty"`
	Result        string    `json:"result"`
	Summary       string    `json:"summary"`
	FollowedFocus string    `json:"followed_focus,omitempty"`
	Strengths     []string  `json:"strengths"`
	Improve       []string  `json:"improve"`
	NextGameFocus string    `json:"next_game_focus"`
}
