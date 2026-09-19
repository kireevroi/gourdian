package server

import (
	"context"
	"runtime"
	"slices"
	"sync"
	"time"

	"gourdian/internal/ai"
	"gourdian/internal/coach"
	"gourdian/internal/config"
)

const (
	authRecheck   = 5 * time.Minute
	limitBackoff  = 10 * time.Minute
	loginWatchFor = 5 * time.Minute
	statusFresh   = time.Minute
)

// Problems that pause a provider until they're fixed.
const (
	aiLoggedOut = "auth"
	aiLimited   = "limit"
)

type aiHealth struct {
	Problem  string    `json:"problem,omitempty"`
	Message  string    `json:"message,omitempty"`
	Provider string    `json:"provider,omitempty"`
	Since    time.Time `json:"since,omitzero"`
	Until    time.Time `json:"until,omitzero"`
	CanLogin bool      `json:"can_login"`
}

type cachedStatus struct {
	ai.Status
	at time.Time
}

// providers holds the AI providers and what the trainer knows about each one's health.
type providers struct {
	list []ai.Provider
	byID map[string]ai.Provider

	mu       sync.Mutex
	health   map[string]aiHealth
	statuses map[string]cachedStatus
	watching map[string]bool
}

func newProviders(list []ai.Provider) *providers {
	p := &providers{list: list, byID: map[string]ai.Provider{}, health: map[string]aiHealth{},
		statuses: map[string]cachedStatus{}, watching: map[string]bool{}}
	for _, pr := range list {
		p.byID[pr.Info().ID] = pr
	}
	return p
}

// healthOf returns a provider's current problem, clearing usage-limit pauses that have run out.
func (s *Server) healthOf(id string) aiHealth {
	p := s.ai.providers
	p.mu.Lock()
	defer p.mu.Unlock()
	h := p.health[id]
	if h.Problem == aiLimited && time.Now().After(h.Until) {
		delete(p.health, id)
		return aiHealth{}
	}
	return h
}

// pick returns the provider to use for a choice: the chosen one if it works, otherwise the
// fallback if that works.
func (s *Server) pick(choice config.AIChoice, set config.AISettings) (ai.Provider, config.AIChoice, bool) {
	for _, c := range []config.AIChoice{choice, set.Fallback} {
		p, ok := s.ai.providers.byID[c.Provider]
		if ok && s.healthOf(c.Provider).Problem == "" {
			return p, c, true
		}
	}
	return nil, choice, false
}

// aiReady reports whether live tips can be requested right now.
func (s *Server) aiReady() bool {
	set := s.cfg.Settings().AI
	_, _, ok := s.pick(set.Live, set)
	return ok
}

// aiFailed pauses a provider on login, key and usage-limit errors and tells the player once;
// other errors only show on the dashboard.
func (s *Server) aiFailed(p ai.Provider, err error) {
	info := p.Info()
	switch ai.KindOf(err) {
	case ai.ErrAuth:
		msg := info.Name + " is logged out, so the AI coach is paused. Log in again on the AI coach page."
		if info.Kind == "api" {
			msg = info.Name + " rejected the API key, so the AI coach is paused. Check the key on the AI coach page."
		}
		_, canLogin := p.(ai.Loginer)
		if s.setAIProblem(aiHealth{Problem: aiLoggedOut, Provider: info.ID, Message: s.withFallback(msg, info.ID), CanLogin: canLogin && runtime.GOOS == "windows"}) {
			s.spawn(func(context.Context) { s.watchLogin(info.ID, authRecheck, 0) })
		}
	case ai.ErrLimit:
		s.setAIProblem(aiHealth{Problem: aiLimited, Provider: info.ID, Until: time.Now().Add(limitBackoff),
			Message: s.withFallback(info.Name+" hit its usage limit, so the AI coach is paused for 10 minutes.", info.ID)})
	default:
		s.hub.publish("ai_error", info.Name+": "+err.Error())
	}
}

func (s *Server) withFallback(msg, failed string) string {
	fb := s.cfg.Settings().AI.Fallback.Provider
	if p, ok := s.ai.providers.byID[fb]; ok && fb != failed && s.healthOf(fb).Problem == "" {
		return msg + " " + p.Info().Name + " answers until then."
	}
	return msg
}

// setAIProblem returns false when that problem was already known, so the player hears about it once.
func (s *Server) setAIProblem(h aiHealth) bool {
	p := s.ai.providers
	p.mu.Lock()
	if p.health[h.Provider].Problem == h.Problem {
		p.mu.Unlock()
		return false
	}
	h.Since = time.Now()
	p.health[h.Provider] = h
	p.mu.Unlock()

	s.log.Warn("AI provider paused", "provider", h.Provider, "problem", h.Problem, "message", h.Message)
	if !s.providerInUse(h.Provider) {
		return true
	}
	s.hub.publish("ai_health", s.aiBanner())
	s.ai.noticePending.Store(true)
	s.noticeAIProblem()
	return true
}

func (s *Server) providerInUse(id string) bool {
	set := s.cfg.Settings().AI
	return set.Enabled && set.Live.Provider == id || set.Review && set.Reviews.Provider == id || set.Fallback.Provider == id
}

// noticeAIProblem tells the player about a paused coach on the HUD and by voice, but only in a
// match: outside one, such as when the app starts with Windows, the dashboard banner is enough.
func (s *Server) noticeAIProblem() {
	set := s.cfg.Settings()
	snap := s.engine.Snapshot(set)
	h := s.aiBanner()
	if !snap.InMatch || h.Problem == "" || !s.ai.noticePending.CompareAndSwap(true, false) {
		return
	}
	tip := coach.Tip{Rule: "ai_problem", Category: "system", Severity: coach.Warn, Text: h.Message, Clock: snap.Clock, At: time.Now(),
		Speech: "The AI coach is paused. Check the dashboard."}
	s.emitTips(snap.MatchID, []coach.Tip{tip}, set)
}

func (s *Server) clearAIProblem(id string) {
	p := s.ai.providers
	p.mu.Lock()
	had := p.health[id].Problem
	delete(p.health, id)
	p.mu.Unlock()
	if had == "" {
		return
	}
	s.log.Info("AI provider resumed", "provider", id, "was", had)
	s.hub.publish("ai_health", s.aiBanner())
	s.spawn(func(context.Context) { s.resumePending() })
}

// aiBanner is the problem the dashboard shows: the first one among the providers in use.
func (s *Server) aiBanner() aiHealth {
	set := s.cfg.Settings().AI
	var ids []string
	if set.Enabled {
		ids = append(ids, set.Live.Provider)
	}
	if set.Review {
		ids = append(ids, set.Reviews.Provider)
	}
	for _, id := range ids {
		if h := s.healthOf(id); h.Problem != "" {
			return h
		}
	}
	return aiHealth{}
}

// checkProvider asks a provider for its status, caches it, and pauses or resumes it to match.
func (s *Server) checkProvider(ctx context.Context, id string) ai.Status {
	pr, ok := s.ai.providers.byID[id]
	if !ok {
		return ai.Status{State: ai.StateError, Detail: "unknown provider"}
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	st := pr.Status(ctx)
	p := s.ai.providers
	p.mu.Lock()
	was := p.statuses[id].State
	p.statuses[id] = cachedStatus{Status: st, at: time.Now()}
	p.mu.Unlock()
	switch st.State {
	case ai.StateReady:
		if s.healthOf(id).Problem == aiLoggedOut {
			s.clearAIProblem(id)
		}
		if was != ai.StateReady {
			// Newly connected: read its real models and pick from them.
			s.spawn(func(ctx context.Context) { s.refreshModels(ctx, id) })
		}
	case ai.StateLogin, ai.StateKey, ai.StateMissing:
		if s.providerInUse(id) {
			s.aiFailed(pr, &ai.Error{Kind: ai.ErrAuth, Msg: st.Detail})
		}
	}
	return st
}

// cachedStatusOf returns the last known status without starting a check.
func (s *Server) cachedStatusOf(id string) (ai.Status, bool) {
	p := s.ai.providers
	p.mu.Lock()
	defer p.mu.Unlock()
	c, ok := p.statuses[id]
	return c.Status, ok && time.Since(c.at) < statusFresh
}

// forgetStatus drops the cached status of a provider, so the next check really asks it.
func (s *Server) forgetStatus(id string) {
	p := s.ai.providers
	p.mu.Lock()
	delete(p.statuses, id)
	p.mu.Unlock()
}

// watchLogin re-checks a provider every interval until it works, or until `until` passes when set.
// A provider has at most one open-ended watch (until 0), marked in watching; the short watch
// after the player starts a login runs beside it and leaves the mark alone.
func (s *Server) watchLogin(id string, interval, until time.Duration) {
	if until == 0 {
		p := s.ai.providers
		p.mu.Lock()
		if p.watching[id] {
			p.mu.Unlock()
			return
		}
		p.watching[id] = true
		p.mu.Unlock()
		defer func() {
			p.mu.Lock()
			delete(p.watching, id)
			p.mu.Unlock()
		}()
	}
	deadline := time.Now().Add(until)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-s.baseCtx.Done():
			return
		case <-t.C:
		}
		if s.healthOf(id).Problem != aiLoggedOut {
			return
		}
		if st := s.checkProvider(s.baseCtx, id); st.State == ai.StateReady || until > 0 && time.Now().After(deadline) {
			return
		}
	}
}

// startupAICheck finds a logged-out provider before the first match instead of during it.
func (s *Server) startupAICheck() {
	set := s.cfg.Settings().AI
	var ids []string
	for _, c := range []config.AIChoice{set.Live, set.Reviews, set.Fallback} {
		if c.Provider != "" && !slices.Contains(ids, c.Provider) {
			ids = append(ids, c.Provider)
		}
	}
	if !set.Enabled && !set.Review {
		return
	}
	for _, id := range ids {
		st := s.checkProvider(s.baseCtx, id)
		if st.State == ai.StateError {
			s.log.Warn("couldn't check an AI provider", "provider", id, "detail", st.Detail)
		}
	}
}
