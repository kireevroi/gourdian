package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"dotatrainer/internal/ai"
	"dotatrainer/internal/coach"
	"dotatrainer/internal/config"
	"dotatrainer/internal/dotadata"
	"dotatrainer/internal/gsi"
	"dotatrainer/internal/stats"
)

const token = "test-token"

func newTestServer(t testing.TB, mutate func(*config.Settings)) (*Server, http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"token":"`+token+`"}`), 0o600)
	store, err := config.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	set := store.Settings()
	set.Voice = config.VoiceOff
	set.AI.Enabled = false
	if mutate != nil {
		mutate(&set)
	}
	if err := store.UpdateSettings(set); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	opendota := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(opendota.Close)
	data := dotadata.New(t.TempDir(), log)
	data.SetBaseURL(opendota.URL)
	st, err := stats.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv := New(store, coach.New(nil, log), st, data, nil, dir, t.TempDir(), log)
	t.Cleanup(func() { srv.Close() })
	return srv, srv.Handler(), dir
}

func payload(clock int, mutate func(*gsi.State)) *gsi.State {
	s := &gsi.State{
		Auth:   &gsi.Auth{Token: token},
		Map:    &gsi.Map{MatchID: "7001", ClockTime: clock, GameTime: clock + 90, GameState: gsi.StateInProgress},
		Player: &gsi.Player{TeamName: "radiant", Gold: 600, LastHits: clock / 10},
		Hero:   &gsi.Hero{ID: 74, Name: "npc_dota_hero_invoker", Level: 5, Alive: true, HealthPercent: 90, MaxHealth: 800, XPos: 100, YPos: 100},
		Items:  map[string]gsi.Item{"teleport0": {Name: "empty"}},
	}
	if mutate != nil {
		mutate(s)
	}
	return s
}

func post(t testing.TB, h http.Handler, body []byte) int {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/gsi", bytes.NewReader(body)))
	return rec.Code
}

func postState(t testing.TB, h http.Handler, s *gsi.State) int {
	t.Helper()
	body, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return post(t, h, body)
}

func TestGSIRejectsWrongToken(t *testing.T) {
	_, h, dir := newTestServer(t, nil)
	s := payload(100, nil)
	s.Auth.Token = "nope"
	if code := postState(t, h, s); code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "stats", stats.TimelineFile)); !os.IsNotExist(err) {
		t.Fatal("rejected payload was recorded")
	}
}

func TestGSIRecordsMatch(t *testing.T) {
	_, h, dir := newTestServer(t, nil)
	for clock := 280; clock <= 420; clock++ {
		if code := postState(t, h, payload(clock, nil)); code != http.StatusOK {
			t.Fatalf("clock %d: status %d", clock, code)
		}
	}
	end := payload(421, func(s *gsi.State) { s.Map.GameState, s.Map.WinTeam = gsi.StatePostGame, "radiant" })
	postState(t, h, end)

	st := openStats(t, dir)
	matches, err := st.Matches()
	if err != nil || len(matches) != 1 {
		t.Fatalf("matches = %+v, %v", matches, err)
	}
	if m := matches[0]; m.Result != "win" || m.TipCounts["no_tp"] == 0 || m.LastHitsAt["5:00"] != 30 {
		t.Fatalf("match = %+v", m)
	}
	timeline, _ := st.Timeline()
	if len(timeline) != 3 {
		t.Fatalf("want samples at minutes 5, 6 and 7, got %+v", timeline)
	}
	tips, _ := st.Tips()
	if !slices.ContainsFunc(tips, func(t stats.TipRecord) bool { return strings.Contains(t.Text, "No TP scroll") }) {
		t.Fatalf("the TP warning was not recorded: %+v", tips)
	}
}

func TestGSIToleratesUnexpectedFieldTypes(t *testing.T) {
	srv, h, _ := newTestServer(t, nil)
	body := []byte(`{"auth":{"token":"` + token + `"},
		"map":{"matchid":"7001","clock_time":300,"game_time":390.5,"game_state":"DOTA_GAMERULES_STATE_GAME_IN_PROGRESS"},
		"player":{"team_name":"radiant","gold":"lots","last_hits":31},
		"hero":{"id":74,"level":6,"alive":true}}`)
	if code := post(t, h, body); code != http.StatusOK {
		t.Fatalf("status %d, want 200", code)
	}
	snap := srv.engine.Snapshot(srv.cfg.Settings())
	if !snap.InMatch || snap.Clock != 300 || snap.Player.LastHits != 31 {
		t.Fatalf("other fields should still decode: %+v", snap)
	}
}

func TestHeroRoleIsRememberedAndApplied(t *testing.T) {
	srv, h, _ := newTestServer(t, func(s *config.Settings) { s.Role = config.RoleCarry })
	postState(t, h, payload(100, nil))

	rec := httptest.NewRecorder()
	next := srv.cfg.Settings()
	next.Role = config.RoleMid
	body, _ := json.Marshal(next)
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body)))
	if rec.Code != http.StatusOK || srv.cfg.Settings().HeroRoles["74"] != config.RoleMid {
		t.Fatalf("role not remembered: %d %+v", rec.Code, srv.cfg.Settings().HeroRoles)
	}

	set := srv.cfg.Settings()
	set.Role = config.RoleHardSupport
	srv.cfg.UpdateSettings(set)
	srv.roleHero = 0
	postState(t, h, payload(10, func(s *gsi.State) { s.Map.MatchID = "7002" }))
	if got := srv.cfg.Settings().Role; got != config.RoleMid {
		t.Fatalf("role on Invoker = %s, want the remembered mid", got)
	}
}

func recordingNames(t *testing.T, dir string) []string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "recordings", "*.jsonl.gz"))
	var names []string
	for _, f := range files {
		names = append(names, filepath.Base(f))
	}
	return names
}

func TestAutoRecordingPerMatchWithPruning(t *testing.T) {
	srv, h, dir := newTestServer(t, func(s *config.Settings) { s.Recording.Keep = 2 })
	playMatch := func(id string) {
		for clock := 295; clock <= 305; clock++ {
			postState(t, h, payload(clock, func(s *gsi.State) { s.Map.MatchID = id }))
		}
		postState(t, h, payload(306, func(s *gsi.State) { s.Map.MatchID, s.Map.GameState = id, gsi.StatePostGame }))
	}

	manual, err := srv.StartRecording()
	if err != nil {
		t.Fatal(err)
	}
	playMatch("8001")
	if got := recordingNames(t, dir); len(got) != 1 || !strings.HasPrefix(got[0], manualPrefix) {
		t.Fatalf("a manual recording should take precedence over auto-recording: %v", got)
	}
	srv.StopRecording()
	os.Remove(manual)

	for _, id := range []string{"8002", "8003", "8004"} {
		playMatch(id)
	}
	got := recordingNames(t, dir)
	if len(got) != 2 || !strings.HasSuffix(got[0], "_8003.jsonl.gz") || !strings.HasSuffix(got[1], "_8004.jsonl.gz") {
		t.Fatalf("want the two newest match recordings, got %v", got)
	}
	if srv.recordingPath() != "" {
		t.Fatal("recording should stop when the match ends")
	}

	playMatch("sim-1")
	if len(recordingNames(t, dir)) != 2 {
		t.Fatal("simulated matches must not be auto-recorded")
	}
}

func TestSpeakable(t *testing.T) {
	info := coach.Tip{Severity: coach.Info, Category: "timing"}
	warn := coach.Tip{Severity: coach.Warn}
	ai := coach.Tip{Severity: coach.Info, Category: "ai"}
	urgent := coach.Tip{Severity: coach.Urgent}
	cases := []struct {
		level string
		tip   coach.Tip
		want  bool
	}{
		{config.SpeakAll, info, true},
		{config.SpeakImportant, info, false},
		{config.SpeakImportant, warn, true},
		{config.SpeakImportant, ai, true},
		{config.SpeakUrgent, warn, false},
		{config.SpeakUrgent, urgent, true},
	}
	for _, c := range cases {
		if got := speakable(c.tip, c.level); got != c.want {
			t.Errorf("speakable(%+v, %s) = %v", c.tip, c.level, got)
		}
	}
}

func TestSettingsRejectCrossOriginWrites(t *testing.T) {
	_, h, _ := newTestServer(t, nil)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"role":"mid"}`))
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", rec.Code)
	}
}

func TestImageProxyRejectsOtherPaths(t *testing.T) {
	_, h, _ := newTestServer(t, nil)
	for _, p := range []string{"/img/etc/passwd", "/img/apps/dota2/images/../../../secret.png", "/img/apps/dota2/images/x.exe"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code == http.StatusOK {
			t.Errorf("%s served", p)
		}
	}
}

func TestImportNeedsAccountID(t *testing.T) {
	srv, h, _ := newTestServer(t, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/import", strings.NewReader(`{}`)))
	if rec.Code != http.StatusBadRequest || srv.importing.Load() {
		t.Fatalf("status %d, want 400 without an account id", rec.Code)
	}
}

// fakeClaude writes a script standing in for the Claude Code CLI; it reports logged in when
// the file "logged-in" exists next to it.
func fakeClaude(t *testing.T) (exe, loginFlag string) {
	t.Helper()
	dir := t.TempDir()
	exe, loginFlag = filepath.Join(dir, "claude"), filepath.Join(dir, "logged-in")
	script := `#!/bin/sh
if [ "$1" = auth ]; then
  if [ -e "` + loginFlag + `" ]; then echo '{"loggedIn":true,"authMethod":"claude.ai"}'; else echo '{"loggedIn":false,"authMethod":"none"}'; fi
  exit 0
fi
echo '{"is_error":true,"subtype":"success","result":"Failed to authenticate: OAuth session expired and could not be refreshed"}'
`
	if err := os.WriteFile(exe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe, loginFlag
}

func TestLoggedOutClaudePausesCoachAndWarnsOnce(t *testing.T) {
	exe, loginFlag := fakeClaude(t)
	srv, h, _ := newTestServer(t, func(s *config.Settings) { s.AI.CLIPaths = map[string]string{"claude": exe}; s.AI.Enabled = true })
	claude := srv.ai.providers.byID["claude"]
	if !srv.aiReady() {
		t.Fatal("coach should start ready")
	}
	err := &ai.Error{Kind: ai.ErrAuth, Msg: "claude (success): Failed to authenticate"}
	srv.aiFailed(claude, err)
	if len(srv.engine.RecentTips()) != 0 {
		t.Fatal("outside a match the warning waits for the dashboard or the next match")
	}
	postState(t, h, payload(100, nil))
	srv.aiFailed(claude, err)
	postState(t, h, payload(101, nil))
	if srv.aiReady() || srv.healthOf("claude").Problem != aiLoggedOut || srv.aiBanner().Provider != "claude" {
		t.Fatalf("coach should pause: %+v", srv.healthOf("claude"))
	}
	warnings := 0
	for _, tip := range srv.engine.RecentTips() {
		if tip.Rule == "ai_problem" {
			warnings++
		}
	}
	if warnings != 1 {
		t.Fatalf("want one warning, got %d", warnings)
	}
	if st := srv.checkProvider(t.Context(), "claude"); st.State != ai.StateLogin {
		t.Fatalf("still logged out: %+v", st)
	}
	os.WriteFile(loginFlag, nil, 0o600)
	if st := srv.checkProvider(t.Context(), "claude"); st.State != ai.StateReady || !srv.aiReady() {
		t.Fatalf("logging in should resume the coach: %+v %+v", st, srv.healthOf("claude"))
	}
}

func TestFallbackProviderAnswersWhileMainIsPaused(t *testing.T) {
	srv, _, _ := newTestServer(t, func(s *config.Settings) {
		s.AI.Fallback = config.AIChoice{Provider: "openrouter", Model: "openai/gpt-5-mini"}
	})
	srv.aiFailed(srv.ai.providers.byID["claude"], &ai.Error{Kind: ai.ErrAuth, Msg: "logged out"})
	set := srv.cfg.Settings().AI
	p, choice, ok := srv.pick(set.Live, set)
	if !ok || p.Info().ID != "openrouter" || choice.Model != "openai/gpt-5-mini" {
		t.Fatalf("pick = %v %+v %v", p, choice, ok)
	}
	if msg := srv.healthOf("claude").Message; !strings.Contains(msg, "OpenRouter answers until then") {
		t.Fatalf("message should mention the fallback: %q", msg)
	}
}

func TestUsageLimitBacksOff(t *testing.T) {
	exe, _ := fakeClaude(t)
	srv, _, _ := newTestServer(t, func(s *config.Settings) { s.AI.CLIPaths = map[string]string{"claude": exe}; s.AI.Enabled = true })
	srv.aiFailed(srv.ai.providers.byID["claude"], errors.New("claude (success): Claude AI usage limit reached|1789000000"))
	if srv.aiReady() {
		t.Fatal("limit should pause the coach")
	}
	p := srv.ai.providers
	p.mu.Lock()
	h := p.health["claude"]
	h.Until = time.Now().Add(-time.Second)
	p.health["claude"] = h
	p.mu.Unlock()
	if !srv.aiReady() {
		t.Fatal("coach should resume after the back-off")
	}
}

func TestSettingsRejectUnknownProvider(t *testing.T) {
	_, h, _ := newTestServer(t, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"ai":{"live":{"provider":"skynet","model":"x"}}}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", rec.Code)
	}
}

func TestProviderKeyIsStoredAndMasked(t *testing.T) {
	_, h, dir := newTestServer(t, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/ai/providers/openrouter/key", strings.NewReader(`{"key":"sk-or-v1-secretvalue1234"}`)))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"key_masked":"sk-…1234"`) || strings.Contains(rec.Body.String(), "secretvalue") {
		t.Fatalf("key response %d: %s", rec.Code, rec.Body)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "secrets.json")); len(data) == 0 {
		t.Fatal("key not stored")
	}
}

func TestRoleGuessForNewHero(t *testing.T) {
	srv, h, dir := newTestServer(t, func(s *config.Settings) { s.Role = config.RoleCarry })
	st := openStats(t, dir)
	for _, id := range []string{"1", "2"} {
		st.AppendMatch(stats.MatchSummary{MatchID: id, HeroID: 74, Hero: "Invoker", Role: config.RoleMid, Source: stats.SourceOpenDota})
	}
	st.AppendMatch(stats.MatchSummary{MatchID: "3", HeroID: 74, Hero: "Invoker", Role: config.RoleCarry, Source: stats.SourceOpenDota})
	postState(t, h, payload(-60, func(s *gsi.State) { s.Map.GameState = gsi.StatePreGame }))
	if got := srv.cfg.Settings().Role; got != config.RoleMid {
		t.Fatalf("role = %s, want the most common role on this hero", got)
	}
	tips := srv.engine.RecentTips()
	if len(tips) == 0 || !strings.Contains(tips[0].Text, "your usual role on") {
		t.Fatalf("role line should say why: %+v", tips)
	}
}

func TestRoleFromHeroRoles(t *testing.T) {
	cases := map[string][]string{
		config.RoleSoftSupport: {"Support", "Disabler", "Nuker", "Initiator"},
		config.RoleCarry:       {"Carry", "Pusher", "Escape"},
		"":                     {"Initiator", "Durable"},
	}
	for want, roles := range cases {
		if got := roleFromHeroRoles(roles); got != want {
			t.Errorf("roleFromHeroRoles(%v) = %q, want %q", roles, got, want)
		}
	}
	if got := roleFromHeroRoles([]string{"Carry", "Support"}); got != config.RoleCarry {
		t.Errorf("the first listed role wins, got %q", got)
	}
}

func TestLaningMidSwitchesRoleUnlessPlayerPicked(t *testing.T) {
	srv, h, _ := newTestServer(t, func(s *config.Settings) { s.Role = config.RoleSoftSupport })
	for clock := 0; clock <= 160; clock++ {
		postState(t, h, payload(clock, nil))
	}
	if got := srv.cfg.Settings().Role; got != config.RoleMid {
		t.Fatalf("role = %s, want mid after laning mid", got)
	}

	srv, h, _ = newTestServer(t, func(s *config.Settings) { s.Role = config.RoleSoftSupport })
	postState(t, h, payload(5, nil))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/role", strings.NewReader(`{"role":"hard_support"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("pick role: %d %s", rec.Code, rec.Body)
	}
	for clock := 6; clock <= 160; clock++ {
		postState(t, h, payload(clock, nil))
	}
	if got := srv.cfg.Settings().Role; got != config.RoleHardSupport {
		t.Fatalf("role = %s, the player's pick must win over lane detection", got)
	}
	if tips := srv.engine.RecentTips(); !slices.ContainsFunc(tips, func(t coach.Tip) bool { return t.Rule == "role_pick" }) {
		t.Fatalf("picking a role should confirm it: %+v", tips)
	}
}

func TestPositionMessagesInRussian(t *testing.T) {
	srv, h, _ := newTestServer(t, func(s *config.Settings) { s.Role, s.Language = config.RoleSoftSupport, "ru" })
	for clock := 0; clock <= 160; clock++ {
		postState(t, h, payload(clock, nil))
	}
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/role", strings.NewReader(`{"role":"carry"}`)))
	seen := map[string]bool{}
	for _, tip := range srv.engine.RecentTips() {
		if tip.Rule != "role_check" && tip.Rule != "role_pick" {
			continue
		}
		seen[tip.Text] = true
		if !strings.Contains(tip.Text, "Тренирую") && !strings.Contains(tip.Text, "Вы стоите на миде") || strings.Contains(tip.Text, "carry") {
			t.Errorf("an English position message on a Russian trainer: %q", tip.Text)
		}
		if tip.SpeechEN == "" || !strings.Contains(tip.SpeechEN, "Coaching you as") {
			t.Errorf("no English line for a machine without a Russian voice: %+v", tip)
		}
	}
	if len(seen) < 2 {
		t.Fatalf("want the lane switch and the pick confirmed, got %v", seen)
	}
}

func TestSettingsPatchMergesButReplacesHeroRoles(t *testing.T) {
	srv, h, _ := newTestServer(t, func(s *config.Settings) {
		s.HeroRoles = map[string]string{"1": config.RoleCarry, "26": config.RoleMid}
	})
	put := func(body string) {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT %s: %d %s", body, rec.Code, rec.Body)
		}
	}
	put(`{"recording":{"keep":5}}`)
	if got := srv.cfg.Settings(); got.Recording.Keep != 5 || !got.Recording.Auto || len(got.HeroRoles) != 2 {
		t.Fatalf("partial patch should only change keep: %+v", got)
	}
	put(`{"hero_roles":{"26":"mid"}}`)
	if got := srv.cfg.Settings().HeroRoles; len(got) != 1 || got["26"] != config.RoleMid {
		t.Fatalf("hero roles = %v, want only Lion", got)
	}
}

func TestRulesAPISaveApplyAndTestOnRecording(t *testing.T) {
	srv, h, dir := newTestServer(t, nil)
	do := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec
	}
	rule := `{"name":"Stack gold","enabled":true,"category":"economy","match":"all","when":{"type":"state","for":2},
		"if":[{"field":"gold","op":"ge","num":600}],"then":{"text":"{gold} gold to spend","severity":"warn"},"cooldown":30}`
	if rec := do(http.MethodPut, "/api/rules/custom", rule); rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	custom := srv.rules.Get().Custom
	if len(custom) != 1 || !slices.ContainsFunc(srv.engine.Rules(), func(r coach.Rule) bool { return r.ID == custom[0].ID }) {
		t.Fatalf("rule not applied: %+v", custom)
	}

	os.MkdirAll(filepath.Join(dir, "recordings"), 0o755)
	var buf bytes.Buffer
	for clock := 100; clock < 140; clock++ {
		payload, _ := json.Marshal(payload(clock, nil))
		fmt.Fprintf(&buf, `{"t":%d,"payload":%s}`+"\n", clock*1000, payload)
	}
	os.WriteFile(filepath.Join(dir, "recordings", "test.jsonl"), buf.Bytes(), 0o644)
	rec := do(http.MethodPost, "/api/rules/test", fmt.Sprintf(`{"recording":"test.jsonl","rule":%s}`, rule))
	var fires []testFire
	json.Unmarshal(rec.Body.Bytes(), &fires)
	if rec.Code != http.StatusOK || len(fires) != 2 || fires[0].Clock != 102 || fires[0].Text != "600 gold to spend" {
		t.Fatalf("test run %d: %s", rec.Code, rec.Body)
	}
	if rec := do(http.MethodPost, "/api/rules/test", fmt.Sprintf(`{"recording":"../config.json","rule":%s}`, rule)); rec.Code != http.StatusBadRequest {
		t.Fatalf("paths outside recordings must be refused: %d", rec.Code)
	}
	if rec := do(http.MethodPut, "/api/rules/builtin/low_hp", `{"spec":{"name":"","category":"survival"}}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("an invalid rule edit was accepted: %d", rec.Code)
	}
	if rec := do(http.MethodPut, "/api/rules/builtin/low_hp", `{"severity":"urgent"}`); rec.Code != http.StatusOK {
		t.Fatalf("changing a built-in rule's severity: %d %s", rec.Code, rec.Body)
	}
	if rec := do(http.MethodDelete, "/api/rules/custom/"+custom[0].ID, ""); rec.Code != http.StatusOK || len(srv.rules.Get().Custom) != 0 {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
}

func TestPersonalTargetsFromHistory(t *testing.T) {
	srv, _, dir := newTestServer(t, nil)
	opendota := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/scenarios/itemTimings" && r.URL.Query().Get("item") == "bfury" {
			w.Write([]byte(`[{"time":720,"games":"63","wins":"45"},{"time":900,"games":"263","wins":"154"},{"time":1200,"games":"167","wins":"71"}]`))
			return
		}
		w.Write([]byte(`[]`))
	}))
	defer opendota.Close()
	cache := t.TempDir()
	os.WriteFile(filepath.Join(cache, "items.json"), []byte(`{"bfury":{"id":145,"dname":"Battle Fury","cost":4100},"manta":{"id":147,"dname":"Manta Style","cost":4650}}`), 0o644)
	os.WriteFile(filepath.Join(cache, "heroes.json"), []byte(`{"1":{"id":1,"localized_name":"Anti-Mage"}}`), 0o644)
	data := dotadata.New(cache, slog.New(slog.NewTextHandler(io.Discard, nil)))
	data.SetBaseURL(opendota.URL)
	data.Start(t.Context())
	data.WaitReady(t.Context())
	srv.data = data

	st := openStats(t, dir)
	for i, lh := range []int{40, 44, 38} {
		id := fmt.Sprint(900 + i)
		st.AppendMatch(stats.MatchSummary{MatchID: id, HeroID: 1, Hero: "Anti-Mage", Role: config.RoleCarry, Source: stats.SourceOpenDota,
			LastHitsAt: map[string]int{"10:00": lh}})
		st.AppendItems([]stats.ItemTiming{
			{MatchID: id, Item: "bfury", Time: 1000 + 60*i, Source: stats.SourceOpenDota},
			{MatchID: id, Item: "manta", Time: 1500 + 60*i, Source: stats.SourceOpenDota},
		})
	}
	var got coach.Targets
	for range 50 { // item timings load in the background
		got = srv.targets.TargetsFor(1, config.RoleCarry)
		if len(got.Items) == 2 && got.Items[0].By > 0 {
			break
		}
		srv.targets.reset()
		time.Sleep(20 * time.Millisecond)
	}
	if got.LastHits[1] != 44 || got.Usual[1] != 40 {
		t.Fatalf("10:00 target should be 110%% of the usual 40: %+v", got)
	}
	// Battle Fury: the usual 17:40 minus a minute (16:40, rounded to 16:30) is later than the
	// 12:00 bucket, so it's the goal. Manta has no OpenDota data, so its goal is also personal.
	if len(got.Items) != 2 || got.Items[0].Item != "bfury" || got.Items[0].By != 990 || got.Items[1].Item != "manta" || got.Items[1].By != 1500 {
		t.Fatalf("item goals = %+v", got.Items)
	}
}

func TestTiltReason(t *testing.T) {
	base := time.Date(2026, 9, 17, 18, 0, 0, 0, time.UTC)
	game := func(minutes int, result string) stats.MatchSummary {
		return stats.MatchSummary{MatchID: fmt.Sprint(minutes), Result: result, Source: stats.SourceLive, EndedAt: base.Add(time.Duration(minutes) * time.Minute)}
	}
	cases := []struct {
		name    string
		matches []stats.MatchSummary
		mmr     []stats.MMREntry
		want    string
	}{
		{"two losses", []stats.MatchSummary{game(0, "win"), game(45, "loss"), game(90, "loss")}, nil, "Two losses in a row"},
		{"gap ends the session", []stats.MatchSummary{game(0, "loss"), game(300, "loss")}, nil, ""},
		{"three of four", []stats.MatchSummary{game(0, "loss"), game(40, "loss"), game(80, "win"), game(120, "loss")}, nil, "Three of your last four"},
		{"win breaks it", []stats.MatchSummary{game(0, "loss"), game(40, "loss"), game(80, "win")}, nil, ""},
		{"mmr drop", []stats.MatchSummary{game(0, "win"), game(40, "loss")},
			[]stats.MMREntry{{Date: base, MMR: 3000}, {Date: base.Add(50 * time.Minute), MMR: 2940}}, "down 60 MMR"},
	}
	for _, c := range cases {
		if got := tiltReason(c.matches, c.mmr); c.want == "" && got != "" || !strings.Contains(got, c.want) {
			t.Errorf("%s: %q", c.name, got)
		}
	}
}

func TestBriefingBeforeHorn(t *testing.T) {
	srv, h, dir := newTestServer(t, func(s *config.Settings) {
		s.Role = config.RoleMid
		s.HeroRoles = map[string]string{"74": config.RoleMid}
	})
	st := openStats(t, dir)
	for i, result := range []string{"win", "loss", "win"} {
		st.AppendMatch(stats.MatchSummary{MatchID: fmt.Sprint(i + 1), HeroID: 74, Hero: "invoker", Role: config.RoleMid, Result: result,
			Source: stats.SourceOpenDota, LastHitsAt: map[string]int{"10:00": 50 + i*5}})
	}
	postState(t, h, payload(-30, func(s *gsi.State) { s.Map.GameState = gsi.StatePreGame }))
	snap := srv.snapshot(srv.cfg.Settings())
	if b := snap.Briefing; b == nil || b.Games != 3 || b.Wins != 2 || b.Usual10 != 55 || b.Target10 != 61 {
		t.Fatalf("briefing = %+v", snap.Briefing)
	}
	spoken := 0
	for _, tip := range srv.engine.RecentTips() {
		if tip.Rule == "briefing" {
			spoken++
			if !strings.Contains(tip.Text, "aim for 61 last hits at 10:00") {
				t.Fatalf("briefing tip = %q", tip.Text)
			}
		}
	}
	postState(t, h, payload(-29, func(s *gsi.State) { s.Map.GameState = gsi.StatePreGame }))
	if spoken != 1 || len(slices.DeleteFunc(srv.engine.RecentTips(), func(t coach.Tip) bool { return t.Rule != "briefing" })) != 1 {
		t.Fatal("the briefing is spoken once per match")
	}
}

func openStats(t testing.TB, dir string) *stats.Store {
	t.Helper()
	st, err := stats.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}
