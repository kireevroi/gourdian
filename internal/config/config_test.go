package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCreatesTokenAndKeepsDefaultsForMissingKeys(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"settings":{"role":"mid","timings":{"tormentor_spawn":1200}}}`), 0o600)
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := st.Get()
	if c.Token == "" || c.Listen != "127.0.0.1:4570" || c.Settings.Role != RoleMid {
		t.Fatalf("config = %+v", c)
	}
	if !c.Settings.AI.Enabled || c.Settings.Recording.Keep != 20 {
		t.Fatalf("defaults not merged: %+v", c.Settings)
	}
	if c.Settings.Timings.TormentorSpawn != 1200 {
		t.Fatal("timings come from the app version, not config.json")
	}
	reopened, _ := Open(dir)
	if reopened.Get().Token != c.Token {
		t.Fatal("token changed between opens")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "config.json")); strings.Contains(string(data), "timings") {
		t.Fatalf("saved config still has timings:\n%s", data)
	}
}

func TestSettingsCopiesDontShareMemory(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateSettings(func() Settings { s := st.Settings(); s.DisabledRules = []string{"a", "b"}; return s }()); err != nil {
		t.Fatal(err)
	}
	copy := st.Settings()
	if err := json.Unmarshal([]byte(`{"disabled_rules":["x"]}`), &copy); err != nil {
		t.Fatal(err)
	}
	if got := st.Settings().DisabledRules[0]; got != "a" {
		t.Fatalf("decoding into a copy changed the stored settings: %q", got)
	}
	if err := st.UpdateSettings(copy); err != nil {
		t.Fatal(err)
	}
	copy.DisabledRules[0] = "y"
	copy.Timings.NeutralTiers[0] = 99
	if got := st.Settings(); got.DisabledRules[0] != "x" || got.Timings.NeutralTiers[0] != 300 {
		t.Fatalf("stored settings alias the caller's slices: %v %v", got.DisabledRules, got.Timings.NeutralTiers)
	}
}

func TestValidateRejectsBadValues(t *testing.T) {
	base := Default().Settings
	for name, mutate := range map[string]func(*Settings){
		"role":       func(s *Settings) { s.Role = "jungler" },
		"voice":      func(s *Settings) { s.Voice = "loud" },
		"level":      func(s *Settings) { s.VoiceLevel = "some" },
		"effort":     func(s *Settings) { s.AI.Live.Effort = "extreme" },
		"provider":   func(s *Settings) { s.AI.Reviews.Provider = "" },
		"custom url": func(s *Settings) { s.AI.CustomURL = "localhost:11434" },
		"interval":   func(s *Settings) { s.AI.Interval = 5 },
		"hero role":  func(s *Settings) { s.HeroRoles = map[string]string{"1": "tank"} },
		"long about": func(s *Settings) { s.AI.Profile = string(make([]byte, 2001)) },
		"steam64":    func(s *Settings) { s.AccountID = "76561197960365728" },
		"hud scale":  func(s *Settings) { s.Overlay.HUDScale = 20 },
		"opacity":    func(s *Settings) { s.Overlay.HUDOpacity = 0 },
		"account":    func(s *Settings) { s.AccountID = "abc" },
		"hotkey":     func(s *Settings) { s.Hotkeys.Dashboard = "F11" },
		"width":      func(s *Settings) { s.Overlay.HUDWidth = 50 },
		"timers":     func(s *Settings) { widgetByID(s, WidgetTimers).Count = 0 },
		"kind":       func(s *Settings) { widgetByID(s, WidgetTimers).Kinds = []string{"lotus"} },
		"same keys":  func(s *Settings) { s.Hotkeys.Dashboard = "shift+ctrl+f10" },
	} {
		s := base.Clone()
		mutate(&s)
		if s.Validate() == nil {
			t.Errorf("%s: invalid settings accepted", name)
		}
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
}

func TestNormalizeWidgetsKeepsOrderAndAddsNewOnes(t *testing.T) {
	got := normalizeWidgets([]HUDWidget{{ID: WidgetTimers, On: true, Count: 2}, {ID: "clock"}, {ID: WidgetAlerts, On: false}, {ID: WidgetTimers}})
	if len(got) != len(DefaultWidgets()) || got[0].ID != WidgetTimers || got[0].Count != 2 || got[1].ID != WidgetAlerts || got[1].On {
		t.Fatalf("widgets = %+v", got)
	}
	for _, w := range got[2:] {
		if w.ID == WidgetStats && w.On || w.ID == WidgetPace && !w.On {
			t.Fatalf("widgets missing from a saved layout keep their default state: %+v", w)
		}
	}
	if fresh := normalizeWidgets(nil); !fresh[0].On {
		t.Fatal("no saved layout means the defaults")
	}
}

func TestOldClaudeSettingsMigrate(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"settings":{"ai":{"enabled":true,"model":"claude-sonnet-5","effort":"high","claude_path":"C:\\claude.exe"}}}`), 0o600)
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ai := st.Settings().AI
	if ai.Live != (AIChoice{Provider: "claude", Model: "claude-sonnet-5", Effort: "high"}) || ai.Reviews.Model != "claude-sonnet-5" || ai.CLIPaths["claude"] != `C:\claude.exe` || ai.LegacyModel != "" {
		t.Fatalf("migrated = %+v", ai)
	}
}

func widgetByID(s *Settings, id string) *HUDWidget {
	for i := range s.HUDWidgets {
		if s.HUDWidgets[i].ID == id {
			return &s.HUDWidgets[i]
		}
	}
	return &HUDWidget{}
}

// The Go code finds a custom install folder through the installer's uninstall entry, so its
// AppID must be the installer's.
func TestAppIDMatchesTheInstaller(t *testing.T) {
	iss, err := os.ReadFile("../../installer/Gourdian.iss")
	if err != nil {
		t.Fatal(err)
	}
	want := `#define AppGuid "` + strings.Trim(AppID, "{}") + `"`
	if !strings.Contains(string(iss), want) {
		t.Fatalf("installer/Gourdian.iss should have %s", want)
	}
}
