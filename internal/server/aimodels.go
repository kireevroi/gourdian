package server

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

// modelsFor is what a provider offers. Providers that can list their models are asked, once
// they work; the others offer what the trainer knows about them.
func (s *Server) modelsFor(ctx context.Context, id string) ([]ai.Model, bool) {
	p, ok := s.ai.providers.byID[id]
	if !ok {
		return nil, false
	}
	c := &s.ai.models
	c.mu.Lock()
	if hit, ok := c.byID[id]; ok && (time.Since(hit.at) < modelsFresh && hit.live || time.Since(hit.at) < time.Minute) {
		c.mu.Unlock()
		return hit.models, hit.live
	}
	c.mu.Unlock()
	models, live := p.Info().Models, false
	if l, ok := p.(ai.ModelLister); ok {
		if st, ok := s.cachedStatusOf(id); ok && st.State == ai.StateReady {
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

func (s *Server) forgetModels(id string) {
	c := &s.ai.models
	c.mu.Lock()
	delete(c.byID, id)
	c.mu.Unlock()
}

// fillModels picks a model for jobs with none, or with one their provider no longer lists.
// A model the provider didn't list itself stays: it may be a full name the provider accepts.
func (s *Server) fillModels(ctx context.Context, set *config.AISettings) bool {
	changed := false
	for _, job := range []struct {
		choice *config.AIChoice
		kind   string
	}{{&set.Live, ai.JobLive}, {&set.Reviews, ai.JobReview}, {&set.Fallback, ai.JobLive}} {
		c := job.choice
		if c.Provider == "" {
			continue
		}
		models, live := s.modelsFor(ctx, c.Provider)
		switch {
		case len(models) == 0:
		case c.Model == "" || live && !c.Typed && !ai.Offers(models, c.Model):
			if pick := ai.Recommend(models, job.kind); pick != "" && pick != c.Model {
				s.log.Info("AI model picked", "provider", c.Provider, "job", job.kind, "was", c.Model, "now", pick)
				c.Model, changed = pick, true
			}
		}
	}
	return changed
}

// markTyped notes which models the player just named themselves, off the provider's list.
// Whether a model was typed is the trainer's call, not the page's.
func (s *Server) markTyped(ctx context.Context, prev, next *config.AISettings) {
	for _, job := range [][2]*config.AIChoice{{&prev.Live, &next.Live}, {&prev.Reviews, &next.Reviews}, {&prev.Fallback, &next.Fallback}} {
		was, c := job[0], job[1]
		switch {
		case c.Provider != was.Provider || c.Model == "":
			c.Typed = false
		case c.Model != was.Model:
			models, _ := s.modelsFor(ctx, c.Provider)
			c.Typed = !ai.Offers(models, c.Model)
		default:
			c.Typed = was.Typed
		}
	}
}

// refreshModels re-reads a provider's models once it works, and moves any job using it onto
// a model it really offers.
func (s *Server) refreshModels(ctx context.Context, id string) {
	s.forgetModels(id)
	set := s.cfg.Settings()
	if set.AI.Live.Provider != id && set.AI.Reviews.Provider != id && set.AI.Fallback.Provider != id {
		return
	}
	if !s.fillModels(ctx, &set.AI) {
		return
	}
	if err := s.cfg.UpdateSettings(set); err != nil {
		s.log.Warn("save the picked AI models", "err", err)
		return
	}
	s.hub.publish("settings", s.settingsResponse())
}

// recommendations are the models the trainer would pick for each job, for the AI page.
func (s *Server) recommendations(ctx context.Context, id string) map[string]string {
	models, _ := s.modelsFor(ctx, id)
	return map[string]string{ai.JobLive: ai.Recommend(models, ai.JobLive), ai.JobReview: ai.Recommend(models, ai.JobReview)}
}
