// Package aisvc keeps the trainer's AI providers usable: which ones work, which are logged out
// or over their usage limit, what models they offer, and the setup that installs the CLIs and
// opens their logins. The server asks it which provider to use and tells it what failed.
package connect

import (
	"context"
	"log/slog"
	"sync"

	"gourdian/internal/ai"
	"gourdian/internal/ai/secrets"
	"gourdian/internal/sys/config"
)

// Host is what the service needs from the app around it.
type Host struct {
	Settings *config.Store
	Keys     *secrets.Store
	WorkDir  string
	Log      *slog.Logger
	// Ctx ends when the app quits; Spawn runs work for as long as the app lives.
	Ctx   context.Context
	Spawn func(work func(ctx context.Context))
	// Publish sends an event to the open dashboards; PublishSettings sends them the settings
	// after the service changed them.
	Publish         func(event string, v any)
	PublishSettings func()
	// Paused is told when a provider in use gets a new problem, to let the player know.
	Paused func(Health)
	// Resumed is told when a paused provider works again, to pick up what waited for it.
	Resumed func()
}

type Service struct {
	host Host
	list []ai.Provider
	byID map[string]ai.Provider

	mu       sync.Mutex
	health   map[string]Health
	statuses map[string]cachedStatus
	watching map[string]bool

	models modelCache
	setup  setupJob
}

// New makes the providers the trainer knows, connected to its settings and stored keys.
func New(h Host) *Service {
	s := &Service{host: h}
	s.UseProviders(ai.NewProviders(s.env()))
	return s
}

// UseProviders replaces the providers, as tests do with fakes.
func (s *Service) UseProviders(list []ai.Provider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.list, s.byID = list, map[string]ai.Provider{}
	s.health, s.statuses, s.watching = map[string]Health{}, map[string]cachedStatus{}, map[string]bool{}
	for _, p := range list {
		s.byID[p.Info().ID] = p
	}
	s.models = modelCache{}
}

// List is every provider, in the order the AI page shows them.
func (s *Service) List() []ai.Provider { return s.list }

// Get is the provider with an id.
func (s *Service) Get(id string) (ai.Provider, bool) {
	p, ok := s.byID[id]
	return p, ok
}

// env connects providers to the settings and the stored keys.
func (s *Service) env() ai.Env {
	return ai.Env{
		WorkDir: s.host.WorkDir,
		CLIPath: func(id string) string { return s.host.Settings.Settings().AI.CLIPaths[id] },
		Key: func(id string) string {
			key, err := s.host.Keys.Get(id)
			if err != nil {
				s.host.Log.Warn("couldn't read a stored API key", "provider", id, "err", err)
			}
			return key
		},
		CustomURL: func() string { return s.host.Settings.Settings().AI.CustomURL },
	}
}

// ValidChoices rejects settings naming providers that don't exist.
func (s *Service) ValidChoices(set config.AISettings) error {
	for _, c := range []config.AIChoice{set.Live, set.Reviews, set.Fallback} {
		if _, ok := s.byID[c.Provider]; !ok && !(c == set.Fallback && c.Provider == "") {
			return &ai.Error{Kind: ai.ErrOther, Msg: "unknown AI provider " + c.Provider}
		}
	}
	return nil
}

// Jobs lists the jobs' provider choices, in the same order for any AI settings.
func Jobs(a *config.AISettings) [3]*config.AIChoice {
	return [3]*config.AIChoice{&a.Live, &a.Reviews, &a.Fallback}
}
