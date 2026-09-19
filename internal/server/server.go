// Package server receives GSI posts and serves the live dashboard.
package server

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gourdian/internal/ai"
	"gourdian/internal/autostart"
	"gourdian/internal/buildinfo"
	"gourdian/internal/coach"
	"gourdian/internal/config"
	"gourdian/internal/dota"
	"gourdian/internal/dotadata"
	"gourdian/internal/gsi"
	"gourdian/internal/hud"
	"gourdian/internal/matchdata"
	"gourdian/internal/model"
	"gourdian/internal/platform"
	"gourdian/internal/rules"
	"gourdian/internal/secrets"
	"gourdian/internal/speech"
	"gourdian/internal/stats"
)

//go:embed web
var webFS embed.FS

type Server struct {
	cfg      *config.Store
	engine   *coach.Engine
	stats    *stats.Store
	data     *dotadata.Client
	speaker  *speech.Speaker
	log      *slog.Logger
	workDir  string
	cacheDir string

	hub       hub
	dirty     atomic.Bool
	authWarns atomic.Int32
	typeWarns atomic.Int32
	extras    atomic.Value // GSI blocks seen beyond the basics, logged once
	accountID atomic.Value
	quit      func()

	recMu sync.Mutex
	rec   *recorder

	firstGSI   sync.Once
	onFirstGSI func()

	// hudQueue lets alerts take turns on the HUD, across its redraws.
	hudMu    sync.Mutex
	hudQueue hud.Queue
	// voiceBusy is set while a Windows voice is being installed.
	voiceBusy atomic.Bool
	piper     piperState

	overlayMu      sync.Mutex
	hotkeyProblems map[string]string
	hudError       string
	hudReported    bool

	mmr     mmrState
	keys    *secrets.Store
	rules   *rules.Store
	targets *targetCache
	brief   briefingCache
	picks   pickCache

	roleMu   sync.Mutex
	roleHero int
	roleLock string // match in which the player picked the role themselves
	ai       aiState

	matches   matchdata.Service
	importing atomic.Bool
	// baseCtx bounds background work (OpenDota waits, imports) to the server's lifetime.
	baseCtx context.Context
	cancel  context.CancelFunc
	// tasks counts the background work Close waits for; closing stops new work starting.
	tasksMu sync.Mutex
	closing bool
	tasks   sync.WaitGroup
}

func New(cfg *config.Store, engine *coach.Engine, st *stats.Store, data *dotadata.Client, speaker *speech.Speaker, workDir, cacheDir string, log *slog.Logger) *Server {
	s := &Server{cfg: cfg, engine: engine, stats: st, data: data, speaker: speaker, workDir: workDir, cacheDir: cacheDir, log: log,
		hub: newHub(log), matches: matchdata.Service{Data: data, Stats: st, Log: log}}
	s.baseCtx, s.cancel = context.WithCancel(context.Background())
	s.keys = secrets.Open(workDir)
	ai.SetToolsDir(workDir)
	store, err := rules.Open(workDir, st)
	if err != nil {
		log.Error("couldn't read rules.json; custom rules are off until it's fixed", "err", err)
	}
	s.rules = store
	s.applyRules()
	s.targets = &targetCache{s: s}
	engine.SetTargetSource(s.targets)
	s.ai.providers = newProviders(ai.NewProviders(s.aiEnv(s.keys)))
	s.applyFocus(cfg.Settings().Role, 0)
	return s
}

// webBuild is a short fingerprint of the dashboard's files.
var webBuild = sync.OnceValue(func() string {
	h := fnv.New64a()
	fs.WalkDir(webFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := webFS.ReadFile(path)
		if err != nil {
			return err
		}
		h.Write([]byte(path))
		h.Write(data)
		return nil
	})
	return strconv.FormatUint(h.Sum64(), 36)
})

func (s *Server) Handler() http.Handler {
	static, _ := fs.Sub(webFS, "web")
	mux := http.NewServeMux()
	mux.HandleFunc("POST /gsi", s.handleGSI)
	mux.HandleFunc("GET /events", s.handleEvents)
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, s.snapshot(s.cfg.Settings()))
	})
	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("PUT /api/settings", s.handlePutSettings)
	mux.HandleFunc("POST /api/role", s.handleRole)
	mux.HandleFunc("PUT /api/autostart", s.handleAutostart)
	mux.HandleFunc("POST /api/folders/{name}", s.handleOpenFolder)
	mux.HandleFunc("POST /api/export/csv", s.handleExportCSV)
	mux.HandleFunc("POST /api/hud/edit", s.handleHUDEdit)
	mux.HandleFunc("GET /api/hud", s.handleHUD)
	mux.HandleFunc("GET /api/rules", s.handleRules)
	mux.HandleFunc("PUT /api/rules/custom", s.handleSaveRule)
	mux.HandleFunc("DELETE /api/rules/custom/{id}", s.handleDeleteRule)
	mux.HandleFunc("PUT /api/rules/builtin/{id}", s.handleOverride)
	mux.HandleFunc("DELETE /api/rules/builtin/{id}", s.handleResetOverride)
	mux.HandleFunc("POST /api/rules/check", s.handleCheckRule)
	mux.HandleFunc("POST /api/rules/test", s.handleTestRule)
	mux.HandleFunc("GET /api/rules/export", s.handleExportRules)
	mux.HandleFunc("POST /api/rules/import", s.handleImportRules)
	mux.HandleFunc("GET /api/recordings", s.handleRecordings)
	mux.HandleFunc("GET /api/catalog", s.handleCatalog)
	mux.HandleFunc("GET /api/goals", s.handleGoals)
	mux.HandleFunc("POST /api/overlay/status", s.handleOverlayStatus)
	mux.HandleFunc("GET /api/history", s.handleHistory)
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("GET /api/stats/files/{name}", s.handleStatsFile)
	mux.HandleFunc("GET /api/drill", s.handleDrill)
	mux.HandleFunc("PUT /api/drill", s.handleSetDrill)
	mux.HandleFunc("GET /api/mmr", s.handleMMRList)
	mux.HandleFunc("POST /api/mmr", s.handleMMR)
	mux.HandleFunc("POST /api/mmr/change", s.handleMMRChange)
	mux.HandleFunc("GET /api/mmr/prompt", s.handleMMRPrompt)
	mux.HandleFunc("PUT /api/matches/{id}/ranked", s.handleMatchRanked)
	mux.HandleFunc("POST /api/matches/{id}/mmr", s.handleMatchMMR)
	mux.HandleFunc("DELETE /api/mmr/prompt", s.handleMMRPrompt)
	mux.HandleFunc("POST /api/ai/ask", s.handleAIAsk)
	mux.HandleFunc("GET /api/ai/status", s.handleAIStatus)
	mux.HandleFunc("POST /api/ai/check", s.handleAICheck)
	mux.HandleFunc("POST /api/ai/login", s.handleAILogin)
	mux.HandleFunc("GET /api/ai/providers", s.handleProviders)
	mux.HandleFunc("POST /api/ai/providers/{id}/check", s.handleProviderCheck)
	mux.HandleFunc("POST /api/ai/providers/{id}/login", s.handleProviderLogin)
	mux.HandleFunc("POST /api/ai/providers/{id}/switch", s.handleProviderSwitch)
	mux.HandleFunc("POST /api/ai/providers/{id}/install", s.handleProviderInstall)
	mux.HandleFunc("GET /api/ai/setup", s.handleAISetup)
	mux.HandleFunc("POST /api/ai/setup", s.handleAISetupStart)
	mux.HandleFunc("DELETE /api/ai/setup", s.handleAISetupStop)
	mux.HandleFunc("PUT /api/ai/providers/{id}/key", s.handleProviderKey)
	mux.HandleFunc("GET /api/ai/providers/{id}/models", s.handleProviderModels)
	mux.HandleFunc("POST /api/ai/providers/{id}/test", s.handleProviderTest)
	mux.HandleFunc("GET /api/reviews", s.handleReviews)
	mux.HandleFunc("POST /api/matches/{id}/review", s.handleReviewMatch)
	mux.HandleFunc("POST /api/voice/test", s.handleVoiceTest)
	mux.HandleFunc("POST /api/voice/install", s.handleVoiceInstall)
	mux.HandleFunc("POST /api/voice/recheck", s.handleVoiceRecheck)
	mux.HandleFunc("GET /img/{path...}", s.handleImage)
	mux.HandleFunc("POST /api/recording", s.handleRecording)
	mux.HandleFunc("POST /api/quit", s.handleQuit)
	mux.HandleFunc("POST /api/import", s.handleImport)
	mux.HandleFunc("GET /api/setup", s.handleSetup)
	mux.HandleFunc("POST /api/setup/install", s.handleSetupInstall)
	mux.Handle("GET /", http.FileServerFS(static))
	return sameOrigin(mux)
}

// sameOrigin blocks other websites open in the browser from changing settings via localhost.
func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.URL.Path != "/gsi" {
			if o := r.Header.Get("Origin"); o != "" {
				if u, err := url.Parse(o); err != nil || u.Host != r.Host {
					http.Error(w, "cross-origin request refused", http.StatusForbidden)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// spawn runs work in the background for the server's lifetime: its ctx ends when the server
// closes, and Close waits for it, so nothing writes to the data file after main closes it.
// Once the server is closing, spawn does nothing.
func (s *Server) spawn(work func(ctx context.Context)) {
	if !s.track() {
		return
	}
	go func() {
		defer s.tasks.Done()
		work(s.baseCtx)
	}()
}

// track counts work that Close waits for, and reports false once the server is closing.
func (s *Server) track() bool {
	s.tasksMu.Lock()
	defer s.tasksMu.Unlock()
	if s.closing {
		return false
	}
	s.tasks.Add(1)
	return true
}

// Run pushes dashboard snapshots until ctx ends: on change, and every few seconds so
// the connection indicator notices when Dota stops posting.
func (s *Server) Run(ctx context.Context) {
	// Finishing a match writes to the data file, so Close waits for this loop too.
	if !s.track() {
		return
	}
	defer s.tasks.Done()
	s.spawn(func(context.Context) { s.resumePending() })
	s.spawn(func(context.Context) { s.startupAICheck() })
	s.spawn(func(context.Context) { s.ensurePiper() })
	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	var n int
	var lastHUD string
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			n++
			if s.dirty.Swap(false) || n%10 == 0 {
				s.hub.publish("snapshot", s.snapshot(s.cfg.Settings()))
			}
			// Alerts expire with time, so the HUD view is rebuilt every tick, but only sent when it changes.
			if data, err := json.Marshal(s.hudPayload()); err == nil && string(data) != lastHUD {
				lastHUD = string(data)
				s.hub.publishRaw("hud", data)
			}
			if m := s.engine.Expire(matchIdleTimeout); m != nil {
				s.recordMatch(m, s.cfg.Settings())
			}
		}
	}
}

func (s *Server) handleGSI(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
	if err != nil {
		http.Error(w, "bad payload", http.StatusBadRequest)
		return
	}
	var st gsi.State
	// A type mismatch still fills every other field, so one odd value from a new Dota
	// version doesn't blind the trainer.
	if err := json.Unmarshal(body, &st); err != nil {
		var typeErr *json.UnmarshalTypeError
		if !errors.As(err, &typeErr) {
			http.Error(w, "bad payload", http.StatusBadRequest)
			return
		}
		if s.typeWarns.Add(1) <= 3 {
			s.log.Warn("unexpected GSI field type; continuing without it", "field", typeErr.Field, "value", typeErr.Value)
		}
	}
	if extras := st.Extras(); len(extras) > 0 {
		if seen, _ := s.extras.Load().(string); seen != strings.Join(extras, ",") {
			s.extras.Store(strings.Join(extras, ","))
			s.log.Info("Dota also sends these game-state blocks", "blocks", extras)
		}
	}
	cfg := s.cfg.Get()
	if st.Auth == nil || subtle.ConstantTimeCompare([]byte(st.Auth.Token), []byte(cfg.Token)) != 1 {
		if s.authWarns.Add(1) <= 3 {
			s.log.Warn("GSI post with wrong token; re-run `gourdian install` and restart Dota")
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	s.firstGSI.Do(func() {
		if s.onFirstGSI != nil {
			s.onFirstGSI()
		}
	})
	if st.Player != nil {
		s.accountID.Store(st.Player.AccountID)
		s.rememberAccount(st.Player.AccountID)
		s.data.RankTier(st.Player.AccountID)
	}
	if st.InMatch() {
		cfg.Settings = s.applyHeroRole(st.Hero.ID, cfg.Settings)
	}
	res := s.engine.Update(&st, cfg.Settings)
	matchID := res.MatchID
	if matchID == "" && st.Map != nil {
		matchID = st.Map.MatchID
	}
	cfg.Settings = s.applyDetectedRole(res, matchID, cfg.Settings)
	if st.InMatch() && st.Map.ClockTime < 0 && !strings.HasPrefix(matchID, "sim-") {
		s.briefMatch(matchID, cfg.Settings)
	}
	if res.NewMatch {
		s.hub.publish("tips", []coach.Tip{})
		s.tiltReminder(matchID, cfg.Settings)
		if cfg.Settings.Recording.Auto && !strings.HasPrefix(matchID, "sim-") {
			s.startAutoRecording(matchID)
		}
	}
	if err := s.record(body); err != nil {
		s.log.Warn("recording failed", "err", err)
	}
	s.deliver(matchID, res.Tips, cfg.Settings)
	if err := s.stats.AppendSamples(res.Samples); err != nil {
		s.log.Error("save timeline", "err", err)
	}
	if m := res.Finished; m != nil {
		s.recordMatch(m, cfg.Settings)
	}
	s.maybeAskAI(&st, matchID, res, cfg.Settings)
	s.dirty.Store(true)
	w.WriteHeader(http.StatusOK)
}

const matchIdleTimeout = 3 * time.Minute

func (s *Server) recordMatch(m *model.MatchSummary, set config.Settings) {
	s.stopAutoRecording(set.Recording.Keep)
	if acct, _ := s.accountID.Load().(string); acct != "" {
		m.RankTier = s.data.RankTier(acct)
	}
	if err := s.stats.AppendMatch(*m); errors.Is(err, stats.ErrDuplicate) {
		s.log.Info("match already recorded; not saving it again", "match", m.MatchID)
		return
	} else if err != nil {
		s.log.Error("save match summary", "err", err)
	}
	if err := s.stats.AppendItems(m.Items); err != nil {
		s.log.Error("save item timings", "err", err)
	}
	s.drillResult(*m, set)
	s.askForMMR(m)
	s.goalFeedback(*m, set)
	s.tiltCheck(*m, set)
	s.hub.publish("match", m)
	s.log.Info("match recorded", "match", m.MatchID, "result", m.Result, "csv", s.stats.Dir())
	if !m.Simulated {
		s.rememberHeroRole(m.HeroID, m.Role)
	}
	s.afterMatch(*m, set)
}

func (s *Server) rememberHeroRole(heroID int, role string) {
	key := strconv.Itoa(heroID)
	if heroID == 0 || s.cfg.Settings().HeroRoles[key] == role {
		return
	}
	if _, err := s.cfg.Update(func(set *config.Settings) error {
		if set.HeroRoles == nil {
			set.HeroRoles = map[string]string{}
		}
		set.HeroRoles[key] = role
		return nil
	}); err != nil {
		s.log.Error("remember hero role", "err", err)
	}
}

// applyHeroRole picks the role when a match starts on a different hero. The pre-game
// role line tells the player where the choice came from.
func (s *Server) applyHeroRole(heroID int, set config.Settings) config.Settings {
	// The lock covers the choice and its settings write, so a post about an older hero can't
	// write its role over a newer hero's. The dashboard update runs without it.
	s.roleMu.Lock()
	defer s.roleMu.Unlock()
	if s.roleHero == heroID {
		return set
	}
	s.roleHero = heroID
	role, note := s.roleFor(heroID, set)
	s.engine.SetRoleNote(note)
	defer func() { s.applyFocus(s.cfg.Settings().Role, heroID) }()
	if role == "" {
		return set
	}
	// Compared with the settings now, not set: another post may have changed them since.
	changed := false
	set, err := s.cfg.Update(func(cur *config.Settings) error {
		changed = cur.Role != role
		cur.Role = role
		return nil
	})
	if err != nil {
		s.log.Error("apply hero role", "err", err)
		return set
	}
	if !changed {
		return set
	}
	s.publishSettingsLater()
	s.log.Info("role set for hero", "role", role, "why", note)
	return set
}

// publishSettingsLater sends the dashboard the new settings without holding up the
// game-state post, since settingsResponse asks Windows about autostart and voices.
func (s *Server) publishSettingsLater() { s.spawn(func(context.Context) { s.publishSettings() }) }

// roleFor tries the role last played on the hero, then the player's most common role on it
// (imported matches count), then the hero's own roles from OpenDota.
func (s *Server) roleFor(heroID int, set config.Settings) (role, note string) {
	name := fmt.Sprintf("hero %d", heroID)
	if s.data != nil {
		name = s.data.HeroName(heroID)
	}
	if r, ok := set.HeroRoles[strconv.Itoa(heroID)]; ok {
		return r, roleSay(set.Language, "what you played on %s last time", name)
	}
	if r := s.usualRole(heroID); r != "" {
		return r, roleSay(set.Language, "your usual role on %s", name)
	}
	if s.data != nil {
		if info, ok := s.data.Hero(heroID); ok {
			if r := roleFromHeroRoles(info.Roles); r != "" {
				return r, roleSay(set.Language, "a guess for %s", name)
			}
		}
	}
	return "", ""
}

func (s *Server) usualRole(heroID int) string {
	role, err := s.stats.UsualRole(heroID, dota.Roles)
	if err != nil {
		s.log.Warn("read the usual role", "hero", heroID, "err", err)
	}
	return role
}

// roleFromHeroRoles reads OpenDota's hero roles, which list the main ones first.
func roleFromHeroRoles(roles []string) string {
	support, carry := slices.Index(roles, "Support"), slices.Index(roles, "Carry")
	switch {
	case support >= 0 && (carry < 0 || support < carry):
		return dota.SoftSupport
	case carry >= 0:
		return dota.Carry
	}
	return ""
}

func speakable(tip coach.Tip, level string) bool {
	switch level {
	case config.SpeakUrgent:
		return tip.Severity == coach.Urgent
	case config.SpeakImportant:
		return tip.Severity != coach.Info || tip.Category == "ai" || tip.Category == "focus"
	default:
		return true
	}
}

// emitTips puts tips made outside the rules (AI, briefing, drill, goals…) in the feed and
// delivers them like the rules' own.
func (s *Server) emitTips(matchID string, tips []coach.Tip, set config.Settings) {
	s.engine.AddTips(tips)
	s.deliver(matchID, tips, set)
}

// deliver sends tips to the dashboard, the voice and tips.csv.
func (s *Server) deliver(matchID string, tips []coach.Tip, set config.Settings) {
	records := make([]model.TipRecord, 0, len(tips))
	for _, tip := range tips {
		s.hub.publish("tip", tip)
		if !tip.Quiet && set.Voice == config.VoiceSystem && s.speaker != nil && speakable(tip, set.VoiceLevel) {
			s.speaker.SayIn(set.Language, tip.Speech, tip.SpeechEN, tip.Severity == coach.Urgent)
		}
		s.log.Info("tip", "clock", tip.Clock, "rule", tip.Rule, "text", tip.Text)
		records = append(records, model.TipRecord{At: tip.At, MatchID: matchID, Clock: tip.Clock, Rule: tip.Rule,
			Category: tip.Category, Severity: string(tip.Severity), Habit: tip.Habit, Text: tip.Text})
	}
	if err := s.stats.AppendTips(records); err != nil {
		s.log.Error("save tips", "err", err)
	}
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch := s.hub.subscribe()
	defer s.hub.unsubscribe(ch)

	w.Write(sse("snapshot", s.snapshot(s.cfg.Settings())))
	w.Write(sse("tips", s.engine.RecentTips()))
	w.Write(sse("hud", s.hudPayload()))
	flusher.Flush()

	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			w.Write(msg)
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
		}
		flusher.Flush()
	}
}

type settingsResponse struct {
	Settings    config.Settings `json:"settings"`
	Rules       []coach.Rule    `json:"rules"`
	Roles       []string        `json:"roles"`
	SystemVoice bool            `json:"system_voice"`
	ConfigPath  string          `json:"config_path"`
	StatsDir    string          `json:"stats_dir"`
	Recording   string          `json:"recording"`
	DataDir     string          `json:"data_dir"`
	Version     string          `json:"version"`
	// Build changes whenever the dashboard's files change, so an open window reloads itself
	// after an update instead of keeping the copy its service worker cached.
	Build string `json:"build"`
	// Speech names what reads tips aloud on this machine, empty when nothing can.
	Speech         string            `json:"speech"`
	AIReady        bool              `json:"ai_ready"`
	AIEfforts      []string          `json:"ai_efforts"`
	HeroNames      map[string]string `json:"hero_names"`
	Autostart      bool              `json:"autostart"`
	CanAutostart   bool              `json:"can_autostart"`
	HotkeyProblems map[string]string `json:"hotkey_problems"`
	// VoiceLangs are the languages Windows has voices for, empty when that isn't known.
	VoiceLangs []string `json:"voice_langs,omitempty"`
	// NaturalVoice is Piper's state on a Linux desktop.
	NaturalVoice *naturalVoice `json:"natural_voice,omitempty"`
}

func (s *Server) settingsResponse() settingsResponse {
	return settingsResponse{
		Settings:       s.cfg.Settings(),
		Rules:          s.engine.Rules(),
		Roles:          dota.Roles,
		SystemVoice:    s.speaker != nil,
		ConfigPath:     s.cfg.Path(),
		StatsDir:       s.stats.Dir(),
		Recording:      s.recordingPath(),
		DataDir:        s.workDir,
		Version:        buildinfo.Version,
		Build:          webBuild(),
		Speech:         s.speechName(),
		AIReady:        s.aiReady(),
		AIEfforts:      config.AIEfforts,
		HeroNames:      s.heroNames(),
		Autostart:      autostart.Enabled(),
		CanAutostart:   runtime.GOOS == "windows" || platform.LinuxDesktop(),
		HotkeyProblems: s.hotkeyProblemsCopy(),
		VoiceLangs:     s.voiceLangs(),
		NaturalVoice:   s.naturalVoiceStatus(),
	}
}

// publishSettings sends every open dashboard the settings as they are now.
func (s *Server) publishSettings() { s.hub.publish("settings", s.settingsResponse()) }

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.settingsResponse())
}

// errNoSystemVoice refuses system speech on a machine that has none.
var errNoSystemVoice = errors.New("Windows speech isn't available on this machine; use browser voice")

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	var keys map[string]json.RawMessage
	if err == nil {
		err = json.Unmarshal(body, &keys)
	}
	if err != nil {
		http.Error(w, "bad settings: "+err.Error(), http.StatusBadRequest)
		return
	}
	// apply decodes the request onto settings: only the fields it sends change.
	apply := func(set *config.Settings) error {
		if _, ok := keys["hero_roles"]; ok {
			set.HeroRoles = nil // decoding merges into a map, so a sent map must replace it to drop heroes
		}
		return json.Unmarshal(body, set)
	}
	check := func(set config.Settings) error {
		if set.Voice == config.VoiceSystem && s.speaker == nil {
			return errNoSystemVoice
		}
		return s.validAIChoices(set.AI)
	}
	// Model checks ask the providers, which takes a while, so they run on a copy first and the
	// settings are only locked to apply the request.
	prev := s.cfg.Settings()
	picked := prev.Clone()
	if err := apply(&picked); err != nil {
		http.Error(w, "bad settings: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := check(picked); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.markTyped(r.Context(), &prev.AI, &picked.AI)
	s.fillModels(r.Context(), &picked.AI)
	next, err := s.cfg.Update(func(cur *config.Settings) error {
		if err := apply(cur); err != nil {
			return err
		}
		now := aiJobs(&picked.AI)
		for i, c := range aiJobs(&cur.AI) {
			if c.Provider == now[i].Provider {
				c.Model, c.Typed = now[i].Model, now[i].Typed
			}
		}
		return check(*cur)
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if snap := s.engine.Snapshot(next); snap.InMatch && snap.Hero != nil && !strings.HasPrefix(snap.MatchID, "sim-") {
		s.rememberHeroRole(snap.Hero.ID, next.Role)
		if next.Role != prev.Role {
			s.lockRole(snap.MatchID)
			s.engine.SetRoleNote(roleSay(next.Language, "your pick"))
		}
	}
	if s.speaker != nil {
		s.speaker.SetRate(next.VoiceRate)
		s.speaker.SetLanguage(next.Language)
	}
	if next.Language != prev.Language {
		s.engine.SetLanguage(next.Language)
	}
	if next.Language != prev.Language || next.Voice != prev.Voice || !maps.Equal(next.PiperVoices, prev.PiperVoices) {
		s.piper.mu.Lock()
		s.piper.failed = ""
		s.piper.mu.Unlock()
		s.ensurePiper()
		s.usePiper()
	}

	resp := s.settingsResponse()
	s.hub.publish("settings", resp)
	s.dirty.Store(true)
	writeJSON(w, resp)
}

func (s *Server) voiceLangs() []string {
	if s.speaker == nil {
		return nil
	}
	return s.speaker.Languages()
}

func (s *Server) speechName() string {
	if s.speaker == nil {
		return ""
	}
	return s.speaker.Name()
}

// OnQuit sets what Quit in the tray menu does.
func (s *Server) OnQuit(quit func()) { s.quit = quit }

// OnFirstGSI runs f once, when Dota sends its first update; set it before serving.
func (s *Server) OnFirstGSI(f func()) { s.onFirstGSI = f }

func (s *Server) handleQuit(w http.ResponseWriter, r *http.Request) {
	if s.quit == nil {
		http.Error(w, "quitting isn't available", http.StatusNotImplemented)
		return
	}
	writeJSON(w, map[string]string{"status": "quitting"})
	s.log.Info("quit requested")
	go s.quit()
}

func (s *Server) handleVoiceTest(w http.ResponseWriter, r *http.Request) {
	set := s.cfg.Settings()
	if set.Voice == config.VoiceSystem && s.speaker != nil {
		english := "Gourdian voice check. Power rune in 15 seconds."
		if set.Language == "ru" {
			s.speaker.SayIn("ru", "Проверка голоса. Руна силы через 15 секунд.", english, true)
		} else {
			s.speaker.Say(english, true)
		}
	}
	writeJSON(w, map[string]string{"voice": set.Voice})
}

// readJSON decodes a request body of at most limit bytes into v. An empty body leaves v as it
// is and isn't an error, since some requests have only optional fields.
func readJSON(w http.ResponseWriter, r *http.Request, limit int64, v any) error {
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit)).Decode(v)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func writeJSON(w http.ResponseWriter, v any) { writeJSONStatus(w, http.StatusOK, v) }

// writeJSONStatus answers with v and a status other than 200. Headers must be set before the
// status is written, or they're dropped.
func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func sse(event string, v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		data = []byte("null")
	}
	return fmt.Appendf(nil, "event: %s\ndata: %s\n\n", event, data)
}

type hub struct {
	mu  sync.Mutex
	log *slog.Logger
	// clients maps each dashboard's buffer to whether it has had to drop an event.
	clients map[chan []byte]bool
}

func newHub(log *slog.Logger) hub {
	return hub{log: log, clients: map[chan []byte]bool{}}
}

// hubBuffer is how many events a dashboard can fall behind by, a minute or so of a match,
// before events are dropped for it.
const hubBuffer = 256

func (h *hub) subscribe() chan []byte {
	ch := make(chan []byte, hubBuffer)
	h.mu.Lock()
	h.clients[ch] = false
	h.mu.Unlock()
	return ch
}

func (h *hub) unsubscribe(ch chan []byte) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
}

func (h *hub) publish(event string, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		data = []byte("null")
	}
	h.publishRaw(event, data)
}

// publishRaw sends an event whose JSON is already encoded. A client that has fallen a whole
// buffer behind misses the event, which is logged once for it: the game-state post can't
// wait for it. Disconnecting it instead would lose the event just the same, and blank the
// dashboard while it reconnects.
func (h *hub) publishRaw(event string, data []byte) {
	msg := fmt.Appendf(nil, "event: %s\ndata: %s\n\n", event, data)
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch, dropped := range h.clients {
		select {
		case ch <- msg:
		default:
			if !dropped {
				h.clients[ch] = true
				h.log.Warn("a dashboard fell behind; dropping events for it", "event", event, "buffer", cap(ch))
			}
		}
	}
}
