package aisvc

import (
	"context"
	"sync"
	"time"

	"gourdian/internal/ai"
	"gourdian/internal/config"
)

// modelsFresh is how long a provider's own model list is trusted before asking again. The
// trainer's suggestions stand in for a minute at most, in case listing failed.
const modelsFresh = 6 * time.Hour

type modelCache struct {
	mu   sync.Mutex
	byID map[string]cachedModels
}

type cachedModels struct {
	models []ai.Model
	live   bool // the provider listed these itself, rather than the trainer's suggestions
	at     time.Time
}

// Models is what a provider offers. Providers that can list their models are asked, once they
// work; the others offer what the trainer knows about them.
func (s *Service) Models(ctx context.Context, id string) ([]ai.Model, bool) {
	p, ok := s.byID[id]
	if !ok {
		return nil, false
	}
	c := &s.models
	c.mu.Lock()
	if hit, ok := c.byID[id]; ok && (time.Since(hit.at) < modelsFresh && hit.live || time.Since(hit.at) < time.Minute) {
		c.mu.Unlock()
		return hit.models, hit.live
	}
	c.mu.Unlock()
	models, live := p.Info().Models, false
	if l, ok := p.(ai.ModelLister); ok {
		if st, ok := s.CachedStatus(id); ok && st.State == ai.StateReady {
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			listed, err := l.ListModels(ctx)
			cancel()
			if err == nil && len(listed) > 0 {
				models, live = listed, true
			}
		}
	}
	c.mu.Lock()
	if c.byID == nil {
		c.byID = map[string]cachedModels{}
	}
	c.byID[id] = cachedModels{models: models, live: live, at: time.Now()}
	c.mu.Unlock()
	return models, live
}

// Recommended are the models the trainer would pick for live tips and reviews, from what's
// known of the provider's models without asking it; nil when nothing is known yet.
func (s *Service) Recommended(id string) map[string]string {
	s.models.mu.Lock()
	defer s.models.mu.Unlock()
	hit, ok := s.models.byID[id]
	if !ok {
		return nil
	}
	return map[string]string{ai.JobLive: ai.Recommend(hit.models, ai.JobLive), ai.JobReview: ai.Recommend(hit.models, ai.JobReview)}
}

// ForgetModels drops a provider's models, so they're asked for again.
func (s *Service) ForgetModels(id string) {
	c := &s.models
	c.mu.Lock()
	delete(c.byID, id)
	c.mu.Unlock()
}

// FillModels picks a model for jobs with none, or with one their provider no longer lists.
// A model the provider didn't list itself stays: it may be a full name the provider accepts.
func (s *Service) FillModels(ctx context.Context, set *config.AISettings) bool {
	changed := false
	for _, job := range []struct {
		choice *config.AIChoice
		kind   string
	}{{&set.Live, ai.JobLive}, {&set.Reviews, ai.JobReview}, {&set.Fallback, ai.JobLive}} {
		c := job.choice
		if c.Provider == "" {
			continue
		}
		models, live := s.Models(ctx, c.Provider)
		switch {
		case len(models) == 0:
		case c.Model == "" || live && !c.Typed && !ai.Offers(models, c.Model):
			if pick := ai.Recommend(models, job.kind); pick != "" && pick != c.Model {
				s.host.Log.Info("AI model picked", "provider", c.Provider, "job", job.kind, "was", c.Model, "now", pick)
				c.Model, changed = pick, true
			}
		}
	}
	return changed
}

// MarkTyped notes which models the player just named themselves, off the provider's list.
// Whether a model was typed is the trainer's call, not the page's.
func (s *Service) MarkTyped(ctx context.Context, prev, next *config.AISettings) {
	for _, job := range [][2]*config.AIChoice{{&prev.Live, &next.Live}, {&prev.Reviews, &next.Reviews}, {&prev.Fallback, &next.Fallback}} {
		was, c := job[0], job[1]
		switch {
		case c.Provider != was.Provider || c.Model == "":
			c.Typed = false
		case c.Model != was.Model:
			models, _ := s.Models(ctx, c.Provider)
			c.Typed = !ai.Offers(models, c.Model)
		default:
			c.Typed = was.Typed
		}
	}
}

// RefreshModels re-reads a provider's models once it works, and moves any job using it onto a
// model it really offers.
func (s *Service) RefreshModels(ctx context.Context, id string) {
	s.ForgetModels(id)
	set := s.host.Settings.Settings()
	if set.AI.Live.Provider != id && set.AI.Reviews.Provider != id && set.AI.Fallback.Provider != id {
		return
	}
	// Asking the provider takes a while, so the models are picked on a copy, and only jobs
	// still on the same provider and model take the pick.
	picked := set.AI
	if !s.FillModels(ctx, &picked) {
		return
	}
	if _, err := s.host.Settings.Update(func(cur *config.Settings) error {
		was, now := Jobs(&set.AI), Jobs(&picked)
		for i, c := range Jobs(&cur.AI) {
			if c.Provider == now[i].Provider && c.Model == was[i].Model {
				c.Model, c.Typed = now[i].Model, now[i].Typed
			}
		}
		return nil
	}); err != nil {
		s.host.Log.Warn("save the picked AI models", "err", err)
		return
	}
	s.host.PublishSettings()
}
