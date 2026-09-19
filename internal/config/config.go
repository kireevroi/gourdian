// Package config persists trainer settings and holds the patch timings built into this version.
package config

import (
	"cmp"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"

	"gourdian/internal/dota"
	"gourdian/internal/hotkey"
)

const (
	VoiceSystem  = "system"
	VoiceBrowser = "browser"
	VoiceOff     = "off"
)

// Languages the dashboard can be shown in.
var Languages = []string{"en", "ru"}

// Voice levels decide which tips are spoken; every tip still shows on screen.
const (
	SpeakAll       = "all"
	SpeakImportant = "important"
	SpeakUrgent    = "urgent"
)

// AIChoice is which provider and model answer one kind of request.
type AIChoice struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Effort   string `json:"effort,omitempty"`
	// Typed is a model the player named that the provider doesn't list; it's never replaced.
	Typed bool `json:"typed,omitempty"`
}

type AISettings struct {
	Enabled bool     `json:"enabled"` // live tips
	Review  bool     `json:"review"`  // match reviews
	Live    AIChoice `json:"live"`
	Reviews AIChoice `json:"reviews"`
	// Fallback answers when the chosen provider is logged out or out of usage; empty means none.
	Fallback     AIChoice          `json:"fallback"`
	Interval     int               `json:"interval"`
	Profile      string            `json:"profile"`
	Instructions string            `json:"instructions"`
	CLIPaths     map[string]string `json:"cli_paths,omitempty"` // provider id -> path to its CLI
	CustomURL    string            `json:"custom_url,omitempty"`

	// Before 1.1 only Claude Code was supported; Open moves these into Live and Reviews.
	LegacyModel      string `json:"model,omitempty"`
	LegacyEffort     string `json:"effort,omitempty"`
	LegacyClaudePath string `json:"claude_path,omitempty"`
}

// migrate moves pre-1.1 Claude settings into the provider choices.
func (a *AISettings) migrate() {
	if a.LegacyModel != "" {
		a.Live.Model, a.Reviews.Model = a.LegacyModel, a.LegacyModel
	}
	if a.LegacyEffort != "" {
		a.Live.Effort = a.LegacyEffort
	}
	if a.LegacyClaudePath != "" {
		if a.CLIPaths == nil {
			a.CLIPaths = map[string]string{}
		}
		a.CLIPaths["claude"] = a.LegacyClaudePath
	}
	a.LegacyModel, a.LegacyEffort, a.LegacyClaudePath = "", "", ""
	// The Gemini CLI needed Node.js, so 1.1 dropped it; its API key provider stays.
	for _, c := range []*AIChoice{&a.Live, &a.Reviews} {
		if c.Provider == "gemini" {
			c.Provider, c.Model = "claude", ""
		}
	}
	if a.Fallback.Provider == "gemini" {
		a.Fallback = AIChoice{}
	}
	delete(a.CLIPaths, "gemini")
}

const maxPromptText = 2000

var AIEfforts = []string{"low", "medium", "high", "xhigh", "max"}

var HUDCorners = []string{"top-right", "top-left", "top-center"}

// OverlaySettings are the in-game HUD's position, size and look.
type OverlaySettings struct {
	HUDCorner string `json:"hud_corner"`
	// HUDPlaced means the player dragged the HUD to HUDX, HUDY; picking a corner clears it.
	HUDPlaced  bool `json:"hud_placed"`
	HUDX       int  `json:"hud_x"`
	HUDY       int  `json:"hud_y"`
	HUDScale   int  `json:"hud_scale"`   // percent
	HUDOpacity int  `json:"hud_opacity"` // percent, for the whole HUD
	HUDWidth   int  `json:"hud_width"`   // pixels at 100% size
	// HUDBackground is the panels' opacity in percent; at 0 only the text shows.
	HUDBackground int  `json:"hud_background"`
	HUDShadow     bool `json:"hud_shadow"` // outline text so it reads over the game
}

// Limits for the overlay's size and opacity.
const (
	MinHUDScale   = 60
	MaxHUDScale   = 200
	MinHUDOpacity = 20
	MinHUDWidth   = 300
	MaxHUDWidth   = 1000
)

// HUDWidget is one kind of line on the HUD; the list order is the order on screen. Options
// only apply to the widgets that use them.
type HUDWidget struct {
	ID string `json:"id"`
	On bool   `json:"on"`
	// Alerts: the least severe tip to show, and whether AI coach tips show.
	MinSeverity string `json:"min_severity,omitempty"`
	Coach       bool   `json:"coach,omitempty"`
	// Timers: how many, which kinds, and only those due within this many seconds (0 = any).
	Count  int      `json:"count,omitempty"`
	Kinds  []string `json:"kinds,omitempty"`
	Within int      `json:"within,omitempty"`
}

// HUD widget ids.
const (
	WidgetAlerts   = "alerts"
	WidgetPosition = "position"
	WidgetDrill    = "drill"
	WidgetPicks    = "picks"
	WidgetBriefing = "briefing"
	WidgetFocus    = "focus"
	WidgetDeath    = "death"
	WidgetTimers   = "timers"
	WidgetPace     = "pace"
	WidgetNextItem = "next_item"
	WidgetSkill    = "skill"
	WidgetItemGoal = "item_goal"
	WidgetStats    = "stats"
)

var (
	TimerKinds    = []string{"rune", "neutral", "objective", "stack"}
	TipSeverities = []string{"info", "warn", "urgent"}
)

func DefaultWidgets() []HUDWidget {
	return []HUDWidget{
		{ID: WidgetAlerts, On: true, MinSeverity: "info", Coach: true},
		{ID: WidgetPosition, On: true},
		{ID: WidgetDrill, On: true},
		{ID: WidgetPicks, On: true},
		{ID: WidgetBriefing, On: true},
		{ID: WidgetFocus, On: true},
		{ID: WidgetDeath, On: true},
		{ID: WidgetTimers, On: true, Count: 3, Kinds: slices.Clone(TimerKinds)},
		{ID: WidgetPace, On: true},
		{ID: WidgetNextItem, On: true},
		{ID: WidgetSkill, On: true},
		{ID: WidgetItemGoal, On: true},
		{ID: WidgetStats, On: false},
	}
}

// normalizeWidgets drops unknown widgets and duplicates, and appends widgets added in newer
// versions with their default on/off state, so a saved layout keeps working.
func normalizeWidgets(list []HUDWidget) []HUDWidget {
	defaults := DefaultWidgets()
	known := map[string]HUDWidget{}
	for _, w := range defaults {
		known[w.ID] = w
	}
	var out []HUDWidget
	seen := map[string]bool{}
	for _, w := range list {
		if _, ok := known[w.ID]; ok && !seen[w.ID] {
			seen[w.ID] = true
			out = append(out, w)
		}
	}
	for _, w := range defaults {
		if !seen[w.ID] {
			out = append(out, w)
		}
	}
	return out
}

// HotkeySettings are the in-game shortcuts, written like "Ctrl+Shift+F10".
type HotkeySettings struct {
	HUDEdit   string `json:"hud_edit"`
	HUDToggle string `json:"hud_toggle"`
	Dashboard string `json:"dashboard"`
}

type RecordingSettings struct {
	Auto bool `json:"auto"`
	Keep int  `json:"keep"`
}

type Settings struct {
	Role      string `json:"role"`
	Language  string `json:"language"`   // "en" or "ru": the dashboard's text
	MMRPrompt bool   `json:"mmr_prompt"` // ask for the new MMR after a match
	// Drill is the rule whose habit the player is working on, counted live and after each match.
	Drill string `json:"drill,omitempty"`
	// QuietInFights holds back spoken reminders while the hero is losing health fast.
	QuietInFights bool              `json:"quiet_in_fights"`
	Voice         string            `json:"voice"`
	VoiceRate     int               `json:"voice_rate"` // -10 (slow) .. 10 (fast)
	VoiceLevel    string            `json:"voice_level"`
	DisabledRules []string          `json:"disabled_rules"`
	Timings       dota.Timings      `json:"-"` // always dota.DefaultTimings()
	AI            AISettings        `json:"ai"`
	Overlay       OverlaySettings   `json:"overlay"`
	Recording     RecordingSettings `json:"recording"`
	Hotkeys       HotkeySettings    `json:"hotkeys"`
	// DashboardWindow opens the dashboard in its own app window instead of a browser tab.
	DashboardWindow bool        `json:"dashboard_window"`
	HUDWidgets      []HUDWidget `json:"hud_widgets"`
	// TiltCheck suggests a break after a run of losses.
	TiltCheck bool `json:"tilt_check"`
	// HeroRoles remembers the role last played on each hero, keyed by hero id.
	HeroRoles map[string]string `json:"hero_roles"`
	// PiperVoices is the natural voice picked for each language on Linux, by voice id.
	PiperVoices map[string]string `json:"piper_voices,omitempty"`
	// AccountID is the player's 32-bit Steam account id, learned from Dota or set by hand.
	AccountID string `json:"account_id"`
}

func (s Settings) RuleEnabled(id string) bool { return !slices.Contains(s.DisabledRules, id) }

// Clone copies every slice and map, so decoding JSON into a copy can't write into the stored settings.
func (s Settings) Clone() Settings {
	s.DisabledRules = slices.Clone(s.DisabledRules)
	s.HUDWidgets = slices.Clone(s.HUDWidgets)
	for i := range s.HUDWidgets {
		s.HUDWidgets[i].Kinds = slices.Clone(s.HUDWidgets[i].Kinds)
	}
	s.HeroRoles = maps.Clone(s.HeroRoles)
	s.PiperVoices = maps.Clone(s.PiperVoices)
	s.AI.CLIPaths = maps.Clone(s.AI.CLIPaths)
	s.Timings.WaterRunes = slices.Clone(s.Timings.WaterRunes)
	s.Timings.NeutralTiers = slices.Clone(s.Timings.NeutralTiers)
	return s
}

func (s Settings) Validate() error {
	if !slices.Contains(dota.Roles, s.Role) {
		return fmt.Errorf("unknown role %q", s.Role)
	}
	if !slices.Contains(Languages, s.Language) {
		return fmt.Errorf("unknown language %q", s.Language)
	}
	if !slices.Contains([]string{VoiceSystem, VoiceBrowser, VoiceOff}, s.Voice) {
		return fmt.Errorf("unknown voice mode %q", s.Voice)
	}
	if !slices.Contains([]string{SpeakAll, SpeakImportant, SpeakUrgent}, s.VoiceLevel) {
		return fmt.Errorf("unknown voice level %q", s.VoiceLevel)
	}
	for hero, role := range s.HeroRoles {
		if !slices.Contains(dota.Roles, role) {
			return fmt.Errorf("unknown role %q for hero %s", role, hero)
		}
	}
	if s.VoiceRate < -10 || s.VoiceRate > 10 {
		return fmt.Errorf("voice_rate must be between -10 and 10")
	}
	if !slices.Contains(HUDCorners, s.Overlay.HUDCorner) {
		return fmt.Errorf("unknown HUD position %q", s.Overlay.HUDCorner)
	}
	if id, err := strconv.ParseUint(s.AccountID, 10, 32); s.AccountID != "" && (err != nil || id == 0) {
		return fmt.Errorf("account id must be your Dota Friend ID, a number like 123456789")
	}
	if o := s.Overlay; o.HUDScale < MinHUDScale || o.HUDScale > MaxHUDScale {
		return fmt.Errorf("HUD size must be between %d%% and %d%%", MinHUDScale, MaxHUDScale)
	}
	if o := s.Overlay; o.HUDOpacity < MinHUDOpacity || o.HUDOpacity > 100 || o.HUDBackground < 0 || o.HUDBackground > 100 {
		return fmt.Errorf("HUD opacity must be %d%% to 100%% and its background 0%% to 100%%", MinHUDOpacity)
	}
	if o := s.Overlay; o.HUDWidth < MinHUDWidth || o.HUDWidth > MaxHUDWidth {
		return fmt.Errorf("HUD width must be %d to %d pixels", MinHUDWidth, MaxHUDWidth)
	}
	for _, w := range s.HUDWidgets {
		switch {
		case w.ID == WidgetTimers && (w.Count < 1 || w.Count > 7 || w.Within < 0):
			return fmt.Errorf("the HUD shows 1 to 7 timers")
		case w.ID == WidgetAlerts && !slices.Contains(TipSeverities, w.MinSeverity):
			return fmt.Errorf("unknown alert level %q", w.MinSeverity)
		}
		for _, k := range w.Kinds {
			if !slices.Contains(TimerKinds, k) {
				return fmt.Errorf("unknown timer kind %q", k)
			}
		}
	}
	seen := map[string]bool{}
	for _, hk := range []string{s.Hotkeys.HUDEdit, s.Hotkeys.HUDToggle, s.Hotkeys.Dashboard} {
		h, err := hotkey.Parse(hk)
		if err != nil {
			return err
		}
		if seen[h.String()] {
			return fmt.Errorf("%s is used for two things", h)
		}
		seen[h.String()] = true
	}
	if s.Recording.Keep < 1 || s.Recording.Keep > 500 {
		return fmt.Errorf("recordings to keep must be between 1 and 500")
	}
	for _, c := range []AIChoice{s.AI.Live, s.AI.Reviews, s.AI.Fallback} {
		if c.Effort != "" && !slices.Contains(AIEfforts, c.Effort) {
			return fmt.Errorf("unknown AI effort %q", c.Effort)
		}
	}
	if s.AI.Live.Provider == "" || s.AI.Reviews.Provider == "" {
		return fmt.Errorf("choose an AI provider for live tips and for reviews")
	}
	if u := s.AI.CustomURL; u != "" && !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return fmt.Errorf("the custom AI URL must start with http:// or https://")
	}
	if s.AI.Interval < 60 {
		return fmt.Errorf("AI interval must be at least 60 seconds")
	}
	if len(s.AI.Profile) > maxPromptText || len(s.AI.Instructions) > maxPromptText {
		return fmt.Errorf("AI profile and instructions are limited to %d characters each", maxPromptText)
	}
	return nil
}

type Config struct {
	Listen   string   `json:"listen"`
	Token    string   `json:"token"`
	Settings Settings `json:"settings"`
}

func Default() Config {
	return Config{
		Listen: "127.0.0.1:4570",
		Settings: Settings{
			Role:          dota.Carry,
			Voice:         VoiceSystem,
			Language:      "en",
			MMRPrompt:     true,
			QuietInFights: true,
			VoiceRate:     1,
			VoiceLevel:    SpeakAll,
			Timings:       dota.DefaultTimings(),
			AI: AISettings{Enabled: true, Review: true, Interval: 180,
				// Claude Code's aliases follow Anthropic's newest models, so the defaults never go stale.
				Live:    AIChoice{Provider: "claude", Model: "sonnet", Effort: "low"},
				Reviews: AIChoice{Provider: "claude", Model: "opus", Effort: "medium"}},
			Overlay:         OverlaySettings{HUDCorner: "top-right", HUDScale: 100, HUDOpacity: 100, HUDWidth: 460, HUDBackground: 70, HUDShadow: true},
			HUDWidgets:      DefaultWidgets(),
			Recording:       RecordingSettings{Auto: true, Keep: 20},
			Hotkeys:         HotkeySettings{HUDEdit: "Ctrl+Shift+F10", HUDToggle: "Ctrl+Shift+F9", Dashboard: "Ctrl+Shift+F11"},
			DashboardWindow: true,
			TiltCheck:       true,
		},
	}
}

// DashboardHost is the host:port to reach a trainer listening on addr, which may be a wildcard.
func DashboardHost(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host == "" || host == "0.0.0.0" || host == "::" {
		return "127.0.0.1:" + port
	}
	return net.JoinHostPort(host, port)
}

// GSIURI is where Dota sends game state for a trainer listening on addr.
func GSIURI(addr string) string { return "http://" + DashboardHost(addr) + "/gsi" }

const (
	AppName = "Gourdian"
	AppExe  = "Gourdian.exe"
	// The app was called Dota Trainer before 1.5. An upgrade keeps the old install folder,
	// where the data lives, and the old name's exe may still be running there.
	LegacyAppName = "Dota Trainer"
	LegacyAppExe  = "Dota Trainer.exe"
	// dataName is the data folder's name outside the installed app; it keeps the old name so
	// existing settings and statistics stay where they are.
	dataName = "dotatrainer"
)

// IsAppExe reports whether an exe file name is the installed app's, under either name.
func IsAppExe(name string) bool { return name == AppExe || name == LegacyAppExe }

// AppID is the installer's AppId (installer/Gourdian.iss). Its uninstall entry records the
// folder the app was installed in, which the player can choose.
const AppID = "{6F4C2B1E-8D2A-4C5B-9E3F-1A7D2C9B4E51}"

const uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\` + AppID + `_is1`

// InstallDirs are where the installed app may be: the folder its installer recorded, the
// default folder, and the one that installs upgraded from Dota Trainer keep.
func InstallDirs(localAppData string) []string {
	var dirs []string
	if dir := registeredInstallDir(); dir != "" {
		dirs = append(dirs, dir)
	}
	if localAppData != "" {
		dirs = append(dirs, filepath.Join(localAppData, "Programs", AppName), filepath.Join(localAppData, "Programs", LegacyAppName))
	}
	return dirs
}

// HomeOverride is the data folder a test profile sets with GOURDIAN_HOME (or DOTATRAINER_HOME,
// its name before 1.5), or "".
func HomeOverride() string { return cmp.Or(os.Getenv("GOURDIAN_HOME"), os.Getenv("DOTATRAINER_HOME")) }

// Dir prefers the installed app's folder, even from other builds, so every entry point shares one set of data.
func Dir() (string, error) {
	if d := HomeOverride(); d != "" {
		return d, nil
	}
	if exe, err := os.Executable(); err == nil && IsAppExe(filepath.Base(exe)) {
		return filepath.Dir(exe), nil
	}
	localAppData, appData := os.Getenv("LOCALAPPDATA"), os.Getenv("APPDATA")
	if runtime.GOOS == "linux" {
		localAppData, appData = wslFolders()
	}
	for _, app := range InstallDirs(localAppData) {
		if isAppDir(app) {
			return app, nil
		}
	}
	if appData != "" {
		return filepath.Join(appData, dataName), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, dataName), nil
}

func isAppDir(dir string) bool {
	for _, exe := range []string{AppExe, LegacyAppExe} {
		if _, err := os.Stat(filepath.Join(dir, exe)); err == nil {
			return true
		}
	}
	return false
}

// wslFolders returns %LOCALAPPDATA% and %APPDATA% as /mnt/... paths when running under WSL.
func wslFolders() (string, string) {
	if os.Getenv("WSL_DISTRO_NAME") == "" {
		return "", ""
	}
	cmdExe, err := exec.LookPath("cmd.exe")
	if err != nil {
		return "", ""
	}
	cmd := exec.Command(cmdExe, "/c", "echo %LOCALAPPDATA%&echo %APPDATA%")
	cmd.Dir = "/mnt/c" // cmd.exe refuses to start in a Linux working directory
	out, err := cmd.Output()
	lines := strings.Split(strings.ReplaceAll(string(out), "\r", ""), "\n")
	if err != nil || len(lines) < 2 {
		return "", ""
	}
	return mntPath(lines[0]), mntPath(lines[1])
}

func mntPath(win string) string {
	win = strings.TrimSpace(win)
	if len(win) < 3 || win[1] != ':' {
		return ""
	}
	return "/mnt/" + strings.ToLower(win[:1]) + "/" + strings.ReplaceAll(win[3:], `\`, "/")
}

// repairSettings rebuilds settings that don't validate: each top-level setting from the file
// is kept if the settings stay valid with it, and the others keep their defaults, which it
// returns the names of.
func repairSettings(file []byte) (Settings, []string) {
	var raw struct {
		Settings map[string]json.RawMessage `json:"settings"`
	}
	_ = json.Unmarshal(file, &raw) // it parsed before, as the whole config
	set := Default().Settings
	var reset []string
	for _, key := range slices.Sorted(maps.Keys(raw.Settings)) {
		one, _ := json.Marshal(map[string]json.RawMessage{key: raw.Settings[key]})
		try := set.Clone()
		err := json.Unmarshal(one, &try)
		try.HUDWidgets = normalizeWidgets(try.HUDWidgets)
		try.AI.migrate()
		if err != nil || try.Validate() != nil {
			reset = append(reset, key)
			continue
		}
		set = try
	}
	return set, reset
}

// Repaired lists the settings Open put back to their defaults because config.json had them
// wrong; the file as it was is kept as config.json.bad.
func (s *Store) Repaired() []string { return s.repaired }

type Store struct {
	path     string
	mu       sync.RWMutex
	cfg      Config
	repaired []string
}

func Open(dir string) (*Store, error) {
	s := &Store{path: filepath.Join(dir, "config.json"), cfg: Default()}
	data, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		// Unmarshal over defaults so keys missing from an older file keep their defaults.
		if err := json.Unmarshal(data, &s.cfg); err != nil {
			return nil, fmt.Errorf("parse %s: %w", s.path, err)
		}
	}
	s.cfg.Settings.HUDWidgets = normalizeWidgets(s.cfg.Settings.HUDWidgets)
	s.cfg.Settings.AI.migrate()
	if s.cfg.Settings.Validate() != nil {
		// A hand edit broke something: every later save would fail, so put back what's broken
		// and keep the original next to it.
		s.cfg.Settings, s.repaired = repairSettings(data)
		if err := os.WriteFile(s.path+".bad", data, 0o600); err != nil {
			return nil, err
		}
		if err := s.save(); err != nil {
			return nil, err
		}
	}
	if s.cfg.Token == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		s.cfg.Token = hex.EncodeToString(b)
		if err := s.save(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) Path() string { return s.path }

func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := s.cfg
	c.Settings = c.Settings.Clone()
	return c
}

func (s *Store) Settings() Settings { return s.Get().Settings }

// UpdateSettings replaces all the settings. To change some of them, use Update, which can't
// undo a change made meanwhile.
func (s *Store) UpdateSettings(next Settings) error {
	_, err := s.Update(func(cur *Settings) error {
		*cur = next.Clone()
		return nil
	})
	return err
}

// Update changes the settings in place: change edits the current settings under the store's
// lock, so changes made at the same time from different places all land. If change fails or
// leaves the settings invalid, nothing changes. It returns the settings after the change.
func (s *Store) Update(change func(*Settings) error) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.cfg.Settings.Clone()
	if err := change(&next); err != nil {
		return s.cfg.Settings.Clone(), err
	}
	if err := next.Validate(); err != nil {
		return s.cfg.Settings.Clone(), err
	}
	next.Timings = dota.DefaultTimings()
	next.HUDWidgets = normalizeWidgets(next.HUDWidgets)
	if reflect.DeepEqual(next, s.cfg.Settings) {
		return next.Clone(), nil
	}
	prev := s.cfg.Settings
	s.cfg.Settings = next
	if err := s.save(); err != nil {
		s.cfg.Settings = prev
		return prev.Clone(), err
	}
	return next.Clone(), nil
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
