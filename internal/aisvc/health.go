package aisvc

import (
	"context"
	"runtime"
	"slices"
	"time"

	"gourdian/internal/ai"
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
	LoggedOut = "auth"
	Limited   = "limit"
)

// Health is what's wrong with a provider, if anything; the dashboard shows it as a banner.
type Health struct {
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

// HealthOf returns a provider's current problem, clearing usage-limit pauses that have run out.
func (s *Service) HealthOf(id string) Health {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.health[id]
	if h.Problem == Limited && time.Now().After(h.Until) {
		delete(s.health, id)
		return Health{}
	}
	return h
}

// Pick returns the provider to use for a choice: the chosen one if it works, otherwise the
// fallback if that works.
func (s *Service) Pick(choice config.AIChoice, set config.AISettings) (ai.Provider, config.AIChoice, bool) {
	for _, c := range []config.AIChoice{choice, set.Fallback} {
		p, ok := s.byID[c.Provider]
		if ok && s.HealthOf(c.Provider).Problem == "" {
			return p, c, true
		}
	}
	return nil, choice, false
}

// Ready reports whether live tips can be requested right now.
func (s *Service) Ready() bool {
	set := s.host.Settings.Settings().AI
	_, _, ok := s.Pick(set.Live, set)
	return ok
}

// Failed pauses a provider on login, key and usage-limit errors and tells the player once;
// other errors only show on the dashboard.
func (s *Service) Failed(p ai.Provider, err error) {
	info := p.Info()
	switch ai.KindOf(err) {
	case ai.ErrAuth:
		msg := info.Name + " is logged out, so the AI coach is paused. Log in again on the AI coach page."
		if info.Kind == "api" {
			msg = info.Name + " rejected the API key, so the AI coach is paused. Check the key on the AI coach page."
		}
		_, canLogin := p.(ai.Loginer)
		if s.setProblem(Health{Problem: LoggedOut, Provider: info.ID, Message: s.withFallback(msg, info.ID), CanLogin: canLogin && runtime.GOOS == "windows"}) {
			s.host.Spawn(func(context.Context) { s.WatchLogin(info.ID, authRecheck, 0) })
		}
	case ai.ErrLimit:
		s.setProblem(Health{Problem: Limited, Provider: info.ID, Until: time.Now().Add(limitBackoff),
			Message: s.withFallback(info.Name+" hit its usage limit, so the AI coach is paused for 10 minutes.", info.ID)})
	default:
		s.host.Publish("ai_error", info.Name+": "+err.Error())
	}
}

func (s *Service) withFallback(msg, failed string) string {
	fb := s.host.Settings.Settings().AI.Fallback.Provider
	if p, ok := s.byID[fb]; ok && fb != failed && s.HealthOf(fb).Problem == "" {
		return msg + " " + p.Info().Name + " answers until then."
	}
	return msg
}

// setProblem returns false when that problem was already known, so the player hears about it once.
func (s *Service) setProblem(h Health) bool {
	s.mu.Lock()
	if s.health[h.Provider].Problem == h.Problem {
		s.mu.Unlock()
		return false
	}
	h.Since = time.Now()
	s.health[h.Provider] = h
	s.mu.Unlock()

	s.host.Log.Warn("AI provider paused", "provider", h.Provider, "problem", h.Problem, "message", h.Message)
	if !s.inUse(h.Provider) {
		return true
	}
	s.host.Publish("ai_health", s.Banner())
	s.host.Paused(h)
	return true
}

func (s *Service) inUse(id string) bool {
	set := s.host.Settings.Settings().AI
	return set.Enabled && set.Live.Provider == id || set.Review && set.Reviews.Provider == id || set.Fallback.Provider == id
}

// ClearProblem forgets a provider's problem, and picks up the work that waited for it.
func (s *Service) ClearProblem(id string) {
	s.mu.Lock()
	had := s.health[id].Problem
	delete(s.health, id)
	s.mu.Unlock()
	if had == "" {
		return
	}
	s.host.Log.Info("AI provider resumed", "provider", id, "was", had)
	s.host.Publish("ai_health", s.Banner())
	s.host.Spawn(func(context.Context) { s.host.Resumed() })
}

// Banner is the problem the dashboard shows: the first one among the providers in use.
func (s *Service) Banner() Health {
	set := s.host.Settings.Settings().AI
	var ids []string
	if set.Enabled {
		ids = append(ids, set.Live.Provider)
	}
	if set.Review {
		ids = append(ids, set.Reviews.Provider)
	}
	for _, id := range ids {
		if h := s.HealthOf(id); h.Problem != "" {
			return h
		}
	}
	return Health{}
}

// Check asks a provider for its status, caches it, and pauses or resumes it to match.
func (s *Service) Check(ctx context.Context, id string) ai.Status {
	pr, ok := s.byID[id]
	if !ok {
		return ai.Status{State: ai.StateError, Detail: "unknown provider"}
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	st := pr.Status(ctx)
	s.mu.Lock()
	was := s.statuses[id].State
	s.statuses[id] = cachedStatus{Status: st, at: time.Now()}
	s.mu.Unlock()
	switch st.State {
	case ai.StateReady:
		if s.HealthOf(id).Problem == LoggedOut {
			s.ClearProblem(id)
		}
		if was != ai.StateReady {
			// Newly connected: read its real models and pick from them.
			s.host.Spawn(func(ctx context.Context) { s.RefreshModels(ctx, id) })
		}
	case ai.StateLogin, ai.StateKey, ai.StateMissing:
		if s.inUse(id) {
			s.Failed(pr, &ai.Error{Kind: ai.ErrAuth, Msg: st.Detail})
		}
	}
	return st
}

// CachedStatus returns the last known status without starting a check.
func (s *Service) CachedStatus(id string) (ai.Status, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.statuses[id]
	return c.Status, ok && time.Since(c.at) < statusFresh
}

// ForgetStatus drops the cached status of a provider, so the next check really asks it.
func (s *Service) ForgetStatus(id string) {
	s.mu.Lock()
	delete(s.statuses, id)
	s.mu.Unlock()
}

// WatchNewLogin re-checks a provider for a few minutes after the player started logging in,
// so the dashboard updates when it's done.
func (s *Service) WatchNewLogin(id string) {
	s.host.Spawn(func(context.Context) { s.WatchLogin(id, 5*time.Second, loginWatchFor) })
}

// WatchLogin re-checks a provider every interval until it works, or until `until` passes when set.
// A provider has at most one open-ended watch (until 0), marked in watching; the short watch
// after the player starts a login runs beside it and leaves the mark alone.
func (s *Service) WatchLogin(id string, interval, until time.Duration) {
	if until == 0 {
		s.mu.Lock()
		if s.watching[id] {
			s.mu.Unlock()
			return
		}
		s.watching[id] = true
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			delete(s.watching, id)
			s.mu.Unlock()
		}()
	}
	deadline := time.Now().Add(until)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-s.host.Ctx.Done():
			return
		case <-t.C:
		}
		if s.HealthOf(id).Problem != LoggedOut {
			return
		}
		if st := s.Check(s.host.Ctx, id); st.State == ai.StateReady || until > 0 && time.Now().After(deadline) {
			return
		}
	}
}

// StartupCheck finds a logged-out provider before the first match instead of during it.
func (s *Service) StartupCheck() {
	set := s.host.Settings.Settings().AI
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
		st := s.Check(s.host.Ctx, id)
		if st.State == ai.StateError {
			s.host.Log.Warn("couldn't check an AI provider", "provider", id, "detail", st.Detail)
		}
	}
}
