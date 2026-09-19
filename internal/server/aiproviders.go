package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"dotatrainer/internal/ai"
	"dotatrainer/internal/aicoach"
	"dotatrainer/internal/coach"
	"dotatrainer/internal/config"
	"dotatrainer/internal/secrets"
)

// aiEnv connects providers to the trainer's settings and stored keys.
func (s *Server) aiEnv(keys *secrets.Store) ai.Env {
	return ai.Env{
		WorkDir: s.workDir,
		CLIPath: func(id string) string { return s.cfg.Settings().AI.CLIPaths[id] },
		Key: func(id string) string {
			key, err := keys.Get(id)
			if err != nil {
				s.log.Warn("couldn't read a stored API key", "provider", id, "err", err)
			}
			return key
		},
		CustomURL: func() string { return s.cfg.Settings().AI.CustomURL },
	}
}

type aiStatus struct {
	aiHealth
	Enabled bool `json:"enabled"`
}

func (s *Server) handleAIStatus(w http.ResponseWriter, r *http.Request) {
	set := s.cfg.Settings().AI
	writeJSON(w, aiStatus{aiHealth: s.aiBanner(), Enabled: set.Enabled || set.Review})
}

// handleAICheck re-checks the providers in use, for the dashboard's "Check again".
func (s *Server) handleAICheck(w http.ResponseWriter, r *http.Request) {
	set := s.cfg.Settings().AI
	for _, id := range []string{set.Live.Provider, set.Reviews.Provider} {
		s.checkProvider(r.Context(), id)
	}
	s.handleAIStatus(w, r)
}

// handleAILogin opens the login of the provider the banner is about.
func (s *Server) handleAILogin(w http.ResponseWriter, r *http.Request) {
	id := s.aiBanner().Provider
	if id == "" {
		id = s.cfg.Settings().AI.Live.Provider
	}
	r.SetPathValue("id", id)
	s.handleProviderLogin(w, r)
}

type providerView struct {
	ai.Info
	Status    ai.Status `json:"status"`
	Checked   bool      `json:"checked"`
	KeyMasked string    `json:"key_masked,omitempty"`
	Problem   aiHealth  `json:"problem"`
	// Recommended are the models the trainer picks for live tips and reviews, once known.
	Recommended map[string]string `json:"recommended,omitempty"`
}

func (s *Server) providerView(p ai.Provider, st ai.Status, checked bool) providerView {
	info := p.Info()
	v := providerView{Info: info, Status: st, Checked: checked, Problem: s.healthOf(info.ID)}
	s.ai.models.mu.Lock()
	if hit, ok := s.ai.models.byID[info.ID]; ok {
		v.Recommended = map[string]string{ai.JobLive: ai.Recommend(hit.models, ai.JobLive), ai.JobReview: ai.Recommend(hit.models, ai.JobReview)}
	}
	s.ai.models.mu.Unlock()
	if info.Kind == "api" {
		if key, _ := s.keys.Get(info.ID); key != "" {
			v.KeyMasked = secrets.Mask(key)
		}
	}
	return v
}

// handleProviders lists every provider; with ?fresh=1 it checks them all first.
func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	list := s.ai.providers.list
	views := make([]providerView, len(list))
	fresh := r.URL.Query().Get("fresh") == "1"
	var wg sync.WaitGroup
	for i, p := range list {
		id := p.Info().ID
		if st, ok := s.cachedStatusOf(id); ok && !fresh {
			views[i] = s.providerView(p, st, true)
			continue
		}
		if !fresh {
			views[i] = s.providerView(p, ai.Status{}, false)
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			views[i] = s.providerView(p, s.checkProvider(r.Context(), id), true)
		}()
	}
	wg.Wait()
	writeJSON(w, views)
}

func (s *Server) provider(w http.ResponseWriter, r *http.Request) (ai.Provider, bool) {
	p, ok := s.ai.providers.byID[r.PathValue("id")]
	if !ok {
		http.Error(w, "unknown AI provider", http.StatusNotFound)
	}
	return p, ok
}

// handleProviderCheck re-checks one provider. A usage limit or a logout from before is
// dropped first: the player is asking because something changed, such as another account.
func (s *Server) handleProviderCheck(w http.ResponseWriter, r *http.Request) {
	p, ok := s.provider(w, r)
	if !ok {
		return
	}
	id := p.Info().ID
	s.clearAIProblem(id)
	s.forgetStatus(id)
	writeJSON(w, s.providerView(p, s.checkProvider(r.Context(), id), true))
}

func (s *Server) handleProviderLogin(w http.ResponseWriter, r *http.Request) {
	p, ok := s.provider(w, r)
	if !ok {
		return
	}
	l, ok := p.(ai.Loginer)
	if !ok {
		http.Error(w, p.Info().Name+" uses an API key, not a login", http.StatusBadRequest)
		return
	}
	if err := l.Login(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	go s.watchLogin(p.Info().ID, 5*time.Second, loginWatchFor)
	writeJSON(w, map[string]string{"status": "Finish logging in in the window that opened. The dashboard updates when it's done."})
}

// handleProviderInstall sets up one provider through the same job as "Set up all", so the
// dashboard sees its progress.
// handleProviderSwitch signs out of a CLI and opens its login, for using another account.
func (s *Server) handleProviderSwitch(w http.ResponseWriter, r *http.Request) {
	p, ok := s.provider(w, r)
	if !ok {
		return
	}
	sw, ok := p.(ai.Switcher)
	if !ok {
		http.Error(w, p.Info().Name+" has no account to switch", http.StatusBadRequest)
		return
	}
	// The old account's limits and logouts say nothing about the new one.
	s.clearAIProblem(p.Info().ID)
	s.forgetStatus(p.Info().ID)
	if err := sw.SwitchAccount(); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	go s.watchLogin(p.Info().ID, 5*time.Second, loginWatchFor)
	writeJSON(w, map[string]string{"status": "Sign in as the other account in the window that opened. The dashboard updates when it's done."})
}

func (s *Server) handleProviderInstall(w http.ResponseWriter, r *http.Request) {
	p, ok := s.provider(w, r)
	if !ok {
		return
	}
	st, err := s.startSetup([]string{p.Info().ID})
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, st)
}

func (s *Server) handleProviderKey(w http.ResponseWriter, r *http.Request) {
	p, ok := s.provider(w, r)
	if !ok {
		return
	}
	var body struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil || p.Info().Kind != "api" {
		http.Error(w, `send {"key": "..."} for an API provider`, http.StatusBadRequest)
		return
	}
	if err := s.keys.Set(p.Info().ID, body.Key); err != nil {
		http.Error(w, "couldn't store the key: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.log.Info("API key updated", "provider", p.Info().ID, "set", body.Key != "")
	s.forgetModels(p.Info().ID)
	st := s.checkProvider(r.Context(), p.Info().ID)
	if st.State == ai.StateReady {
		s.refreshModels(r.Context(), p.Info().ID)
	}
	writeJSON(w, s.providerView(p, st, true))
}

func (s *Server) handleProviderModels(w http.ResponseWriter, r *http.Request) {
	p, ok := s.provider(w, r)
	if !ok {
		return
	}
	models, _ := s.modelsFor(r.Context(), p.Info().ID)
	if models == nil {
		models = []ai.Model{}
	}
	writeJSON(w, models)
}

// handleProviderTest sends a short live-tip request, so the player can see a provider answer.
func (s *Server) handleProviderTest(w http.ResponseWriter, r *http.Request) {
	p, ok := s.provider(w, r)
	if !ok {
		return
	}
	var choice config.AIChoice
	json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&choice)
	choice.Provider = p.Info().ID
	set := s.cfg.Settings()
	snap := coach.Snapshot{InMatch: true, Clock: 610, Team: "radiant", Role: config.RoleMid,
		Hero: &coach.HeroView{Name: "Shadow Fiend", Level: 9, Alive: true, HealthPercent: 70, ManaPercent: 40}}
	prompt := aicoach.Prompt(aicoach.Input{Reason: "a connection test from the dashboard; answer as you would in a match", Role: config.RoleMid, Snapshot: snap})
	ctx, cancel := context.WithTimeout(r.Context(), aiTimeout)
	defer cancel()
	started := time.Now()
	tips, err := aicoach.Suggest(ctx, p, choice, set.AI, set.Language, prompt)
	if err != nil {
		if k := ai.KindOf(err); k == ai.ErrAuth || k == ai.ErrLimit {
			s.checkProvider(context.Background(), choice.Provider)
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if s.healthOf(choice.Provider).Problem != "" {
		s.clearAIProblem(choice.Provider)
	}
	writeJSON(w, map[string]any{"tips": tips, "took_ms": time.Since(started).Milliseconds()})
}

// validAIChoices rejects settings naming providers that don't exist.
func (s *Server) validAIChoices(set config.AISettings) error {
	for _, c := range []config.AIChoice{set.Live, set.Reviews, set.Fallback} {
		if _, ok := s.ai.providers.byID[c.Provider]; !ok && !(c == set.Fallback && c.Provider == "") {
			return &ai.Error{Kind: ai.ErrOther, Msg: "unknown AI provider " + c.Provider}
		}
	}
	return nil
}
