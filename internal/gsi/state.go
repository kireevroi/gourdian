// Package gsi models the player-mode Game State Integration payload.
package gsi

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	StateWaitForPlayers = "DOTA_GAMERULES_STATE_WAIT_FOR_PLAYERS_TO_LOAD"
	StateHeroSelection  = "DOTA_GAMERULES_STATE_HERO_SELECTION"
	StateStrategyTime   = "DOTA_GAMERULES_STATE_STRATEGY_TIME"
	StatePreGame        = "DOTA_GAMERULES_STATE_PRE_GAME"
	StateInProgress     = "DOTA_GAMERULES_STATE_GAME_IN_PROGRESS"
	StatePostGame       = "DOTA_GAMERULES_STATE_POST_GAME"
)

type State struct {
	Provider  *Provider          `json:"provider,omitempty"`
	Map       *Map               `json:"map,omitempty"`
	Player    *Player            `json:"player,omitempty"`
	Hero      *Hero              `json:"hero,omitempty"`
	Abilities map[string]Ability `json:"abilities,omitempty"`
	Items     map[string]Item    `json:"items,omitempty"`
	Events    []Event            `json:"events,omitempty"`
	Auth      *Auth              `json:"auth,omitempty"`
	// Buildings and Draft only arrive in some game modes; Dota sends the player nothing about
	// enemy heroes' positions or items.
	Buildings map[string]map[string]Building `json:"buildings,omitempty"`
	Draft     *Draft                         `json:"draft,omitempty"`
}

// Building is one tower, barracks, shrine or ancient.
type Building struct {
	Health    int `json:"health"`
	MaxHealth int `json:"max_health"`
}

// Draft is the pick and ban phase, when Dota reports it to the player.
type Draft struct {
	ActiveTeam        int             `json:"activeteam"`
	Pick              bool            `json:"pick"`
	ActiveTeamTimeRem int             `json:"activeteam_time_remaining"`
	Team2             map[string]Slot `json:"team2"`
	Team3             map[string]Slot `json:"team3"`
}

// Slot is one pick or ban in the draft.
type Slot struct {
	Class string `json:"class"`
	ID    int    `json:"id"`
}

// Extras names the optional blocks this payload carried, for the log.
func (s *State) Extras() []string {
	var out []string
	if len(s.Buildings) > 0 {
		out = append(out, "buildings")
	}
	if s.Draft != nil {
		out = append(out, "draft")
	}
	return out
}

type Auth struct {
	Token string `json:"token"`
}

type Provider struct {
	Name      string `json:"name"`
	AppID     int    `json:"appid"`
	Version   int    `json:"version"`
	Timestamp int64  `json:"timestamp"`
}

type Map struct {
	Name                 string `json:"name"`
	MatchID              string `json:"matchid"`
	GameTime             int    `json:"game_time"`
	ClockTime            int    `json:"clock_time"`
	Daytime              bool   `json:"daytime"`
	NightstalkerNight    bool   `json:"nightstalker_night"`
	RadiantScore         int    `json:"radiant_score"`
	DireScore            int    `json:"dire_score"`
	GameState            string `json:"game_state"`
	Paused               bool   `json:"paused"`
	WinTeam              string `json:"win_team"`
	CustomGameName       string `json:"customgamename"`
	WardPurchaseCooldown int    `json:"ward_purchase_cooldown"`
}

type Player struct {
	SteamID        string `json:"steamid"`
	AccountID      string `json:"accountid"`
	Name           string `json:"name"`
	Activity       string `json:"activity"`
	Kills          int    `json:"kills"`
	Deaths         int    `json:"deaths"`
	Assists        int    `json:"assists"`
	LastHits       int    `json:"last_hits"`
	Denies         int    `json:"denies"`
	KillStreak     int    `json:"kill_streak"`
	CommandsIssued int    `json:"commands_issued"`
	TeamName       string `json:"team_name"`
	Gold           int    `json:"gold"`
	GoldReliable   int    `json:"gold_reliable"`
	GoldUnreliable int    `json:"gold_unreliable"`
	GoldFromCreeps int    `json:"gold_from_creep_kills"`
	GoldFromHeroes int    `json:"gold_from_hero_kills"`
	GoldFromIncome int    `json:"gold_from_income"`
	GoldFromShared int    `json:"gold_from_shared"`
	GPM            int    `json:"gpm"`
	XPM            int    `json:"xpm"`
	// TeamSlot is 0 to 4 within the team; nil when Dota didn't send it.
	TeamSlot *int `json:"team_slot"`
}

// PlayerID is the player's number in match events: radiant 0 to 4, dire 5 to 9.
func (p *Player) PlayerID() (int, bool) {
	if p == nil || p.TeamSlot == nil {
		return 0, false
	}
	if p.TeamName == "dire" {
		return *p.TeamSlot + 5, true
	}
	return *p.TeamSlot, true
}

type Hero struct {
	XPos            int    `json:"xpos"`
	YPos            int    `json:"ypos"`
	ID              int    `json:"id"`
	Name            string `json:"name"`
	Level           int    `json:"level"`
	XP              int    `json:"xp"`
	Alive           bool   `json:"alive"`
	RespawnSeconds  int    `json:"respawn_seconds"`
	BuybackCost     int    `json:"buyback_cost"`
	BuybackCooldown int    `json:"buyback_cooldown"`
	Health          int    `json:"health"`
	MaxHealth       int    `json:"max_health"`
	HealthPercent   int    `json:"health_percent"`
	Mana            int    `json:"mana"`
	MaxMana         int    `json:"max_mana"`
	ManaPercent     int    `json:"mana_percent"`
	Silenced        bool   `json:"silenced"`
	Stunned         bool   `json:"stunned"`
	Disarmed        bool   `json:"disarmed"`
	MagicImmune     bool   `json:"magicimmune"`
	Hexed           bool   `json:"hexed"`
	Muted           bool   `json:"muted"`
	Break           bool   `json:"break"`
	AghanimsScepter bool   `json:"aghanims_scepter"`
	AghanimsShard   bool   `json:"aghanims_shard"`
	Smoked          bool   `json:"smoked"`
	HasDebuff       bool   `json:"has_debuff"`
	Talent1         bool   `json:"talent_1"`
	Talent2         bool   `json:"talent_2"`
	Talent3         bool   `json:"talent_3"`
	Talent4         bool   `json:"talent_4"`
	Talent5         bool   `json:"talent_5"`
	Talent6         bool   `json:"talent_6"`
	Talent7         bool   `json:"talent_7"`
	Talent8         bool   `json:"talent_8"`
	AttributesLevel int    `json:"attributes_level"`
}

func (h *Hero) TalentsTaken() int {
	n := 0
	for _, t := range []bool{h.Talent1, h.Talent2, h.Talent3, h.Talent4, h.Talent5, h.Talent6, h.Talent7, h.Talent8} {
		if t {
			n++
		}
	}
	return n
}

type Ability struct {
	Name          string `json:"name"`
	Level         int    `json:"level"`
	CanCast       bool   `json:"can_cast"`
	Passive       bool   `json:"passive"`
	AbilityActive bool   `json:"ability_active"`
	Cooldown      int    `json:"cooldown"`
	Ultimate      bool   `json:"ultimate"`
}

type Item struct {
	Name      string `json:"name"`
	Purchaser int    `json:"purchaser"`
	CanCast   bool   `json:"can_cast"`
	Cooldown  int    `json:"cooldown"`
	Passive   bool   `json:"passive"`
	Charges   int    `json:"charges"`
}

func (i Item) Empty() bool { return i.Name == "" || i.Name == "empty" }

func (i Item) Short() string { return strings.TrimPrefix(i.Name, "item_") }

type Event struct {
	GameTime       int    `json:"game_time"`
	EventType      string `json:"event_type"`
	Team           string `json:"team"`
	KilledByTeam   string `json:"killed_by_team"`
	PlayerID       int    `json:"player_id"`
	KillerPlayerID int    `json:"killer_player_id"`
	Snatched       bool   `json:"snatched"`
	Data           string `json:"data"` // a generic_event's Chat, as JSON
}

// Key dedupes events: GSI resends each event on every update for 30 seconds.
func (e Event) Key() string {
	return fmt.Sprintf("%s@%d/%d/%s/%s", e.EventType, e.GameTime, e.PlayerID, e.Team, e.Data)
}

// Chat is the line a generic_event prints in the game's chat, such as CHAT_MESSAGE_HERO_KILL.
// What the numbers mean depends on the type.
type Chat struct {
	Type    string  `json:"type"`
	Value   int     `json:"value"`
	Player1 int     `json:"playerid1"`
	Time    float64 `json:"time"`
}

func (e Event) Chat() (Chat, bool) {
	var c Chat
	if e.EventType != "generic_event" || json.Unmarshal([]byte(e.Data), &c) != nil {
		return Chat{}, false
	}
	return c, true
}

// TeamNumber is team_name as chat events number it, or 0 for a spectator.
func TeamNumber(name string) int {
	switch name {
	case "radiant":
		return 2
	case "dire":
		return 3
	}
	return 0
}

func (s *State) Clock() (int, bool) {
	if s == nil || s.Map == nil {
		return 0, false
	}
	return s.Map.ClockTime, true
}

func (s *State) InMatch() bool {
	if s == nil || s.Map == nil || s.Hero == nil || s.Player == nil || s.Hero.ID == 0 {
		return false
	}
	return s.Map.GameState == StatePreGame || s.Map.GameState == StateInProgress
}

const (
	Inventory = "inventory" // slot0-5
	Backpack  = "backpack"  // slot6-8
	Stash     = "stash"     // stash0-5
)

func (s *State) slots(kind string) []Item {
	var prefix string
	var from, to int
	switch kind {
	case Inventory:
		prefix, from, to = "slot", 0, 5
	case Backpack:
		prefix, from, to = "slot", 6, 8
	case Stash:
		prefix, from, to = "stash", 0, 5
	}
	var out []Item
	for i := from; i <= to; i++ {
		if it, ok := s.Items[fmt.Sprintf("%s%d", prefix, i)]; ok && !it.Empty() {
			out = append(out, it)
		}
	}
	return out
}

func (s *State) ItemsIn(kinds ...string) []Item {
	var out []Item
	for _, k := range kinds {
		out = append(out, s.slots(k)...)
	}
	return out
}

func (s *State) FindItem(name string, kinds ...string) (Item, bool) {
	name = strings.TrimPrefix(name, "item_")
	for _, it := range s.ItemsIn(kinds...) {
		if it.Short() == name {
			return it, true
		}
	}
	return Item{}, false
}

func (s *State) Teleport() Item { return s.Items["teleport0"] }

func (s *State) Neutral() Item { return s.Items["neutral0"] }
