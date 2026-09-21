// Package gsi models the player-mode Game State Integration payload.
package gsi

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
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

// Draft is the pick and ban phase. Valve fills it in for spectators and observers only, so a
// player's feed carries an empty one and DraftBoard finds nothing; see
// https://github.com/ValveSoftware/Dota2-Gameplay/issues/19408. It is parsed anyway so that the
// day Valve opens it up, the trainer notices instead of us guessing.
type Draft struct {
	ActiveTeam        int        `json:"activeteam"`
	Pick              bool       `json:"pick"`
	ActiveTeamTimeRem int        `json:"activeteam_time_remaining"`
	Team2             *DraftTeam `json:"team2"` // radiant
	Team3             *DraftTeam `json:"team3"` // dire
}

// DraftTeam is one side of the board. Dota names the slots flat inside the team — "pick0_id",
// "pick0_class", "ban0_id", "home_team" — rather than as an object each, so they are gathered
// by hand; how many slots there are changes with the game mode and the patch.
type DraftTeam struct {
	Home  bool
	Picks []int
	Bans  []int
}

func (t *DraftTeam) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	picks, bans := map[int]int{}, map[int]int{}
	for key, value := range raw {
		switch {
		case key == "home_team":
			json.Unmarshal(value, &t.Home)
		case strings.HasPrefix(key, "pick"):
			draftSlot(key, "pick", value, picks)
		case strings.HasPrefix(key, "ban"):
			draftSlot(key, "ban", value, bans)
		}
	}
	t.Picks, t.Bans = inSlotOrder(picks), inSlotOrder(bans)
	return nil
}

// draftSlot reads a "pick3_id" or "ban3_id" into into[3], ignoring the "_class" twin and an
// empty slot, whose id is 0.
func draftSlot(key, prefix string, value json.RawMessage, into map[int]int) {
	n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(key, prefix), "_id"))
	if err != nil || !strings.HasSuffix(key, "_id") {
		return
	}
	var id int
	if json.Unmarshal(value, &id) == nil && id > 0 {
		into[n] = id
	}
}

// inSlotOrder lists the heroes by the slot they went in.
func inSlotOrder(slots map[int]int) []int {
	if len(slots) == 0 {
		return nil
	}
	order := slices.Sorted(maps.Keys(slots))
	out := make([]int, 0, len(order))
	for _, n := range order {
		out = append(out, slots[n])
	}
	return out
}

// DraftBoard is the heroes each side has taken and every ban, from the player's point of view.
// ok is false when Dota sent no draft or nothing in it, which is what happens in a real match.
func (s *State) DraftBoard() (ours, theirs, bans []int, ok bool) {
	if s == nil || s.Draft == nil {
		return nil, nil, nil, false
	}
	side := TeamNumber("radiant")
	if s.Player != nil && TeamNumber(s.Player.TeamName) != 0 {
		side = TeamNumber(s.Player.TeamName)
	} else if s.Draft.Team3 != nil && s.Draft.Team3.Home {
		side = TeamNumber("dire")
	}
	mine, other := s.Draft.Team2, s.Draft.Team3
	if side == TeamNumber("dire") {
		mine, other = other, mine
	}
	for _, t := range []*DraftTeam{mine, other} {
		if t != nil {
			bans = append(bans, t.Bans...)
		}
	}
	if mine != nil {
		ours = mine.Picks
	}
	if other != nil {
		theirs = other.Picks
	}
	return ours, theirs, bans, len(ours)+len(theirs) > 0
}

// Extras names the optional blocks this payload carried, for the log.
func (s *State) Extras() []string {
	var out []string
	if len(s.Buildings) > 0 {
		out = append(out, "buildings")
	}
	if !s.Draft.empty() {
		out = append(out, "draft")
	}
	return out
}

// Dota sends players a bare "draft": {} throughout a match, which is not it offering a draft.
func (d *Draft) empty() bool {
	return d == nil || (d.ActiveTeam == 0 && !d.Pick && d.ActiveTeamTimeRem == 0 && d.Team2 == nil && d.Team3 == nil)
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

// slotNames are the item slots of each kind, spelled out: rules look through them many times
// a second, and formatting the names each time was a third of the rules' work.
var slotNames = map[string][]string{
	Inventory: {"slot0", "slot1", "slot2", "slot3", "slot4", "slot5"},
	Backpack:  {"slot6", "slot7", "slot8"},
	Stash:     {"stash0", "stash1", "stash2", "stash3", "stash4", "stash5"},
}

func (s *State) slots(kind string) []Item {
	var out []Item
	for _, name := range slotNames[kind] {
		if it, ok := s.Items[name]; ok && !it.Empty() {
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
