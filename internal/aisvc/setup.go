package aisvc

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"gourdian/internal/ai"
)

const (
	setupLogLines = 60
	setupTimeout  = 20 * time.Minute
	loginWait     = 4 * time.Minute
)

// loginPoll is how often the trainer re-checks while a login window is open.
var loginPoll = 5 * time.Second

// SetupState is what the AI page shows while the trainer installs and connects a CLI.
type SetupState struct {
	Running bool     `json:"running"`
	Step    string   `json:"step"`
	Current string   `json:"current"`
	Log     []string `json:"log"`
	Done    []string `json:"done"`
	Error   string   `json:"error"`
}

type setupJob struct {
	mu     sync.Mutex
	state  SetupState
	cancel context.CancelFunc
}

// Setup is where the setup job is now.
func (s *Service) Setup() SetupState {
	set := &s.setup
	set.mu.Lock()
	defer set.mu.Unlock()
	st := set.state
	st.Log = slices.Clone(st.Log)
	st.Done = slices.Clone(st.Done)
	return st
}

// setupEdit changes the state and tells the dashboard, so progress shows as it happens.
func (s *Service) setupEdit(f func(*SetupState)) {
	set := &s.setup
	set.mu.Lock()
	f(&set.state)
	if n := len(set.state.Log); n > setupLogLines {
		set.state.Log = set.state.Log[n-setupLogLines:]
	}
	st := set.state
	st.Log, st.Done = slices.Clone(st.Log), slices.Clone(st.Done)
	set.mu.Unlock()
	s.host.Publish("ai_setup", st)
}

func (s *Service) setupStep(id, text string) {
	s.host.Log.Info("AI setup", "provider", id, "step", text)
	s.setupEdit(func(st *SetupState) {
		st.Current, st.Step = id, text
		st.Log = append(st.Log, text)
	})
}

func (s *Service) setupLine(line string) {
	s.setupEdit(func(st *SetupState) { st.Log = append(st.Log, line) })
}

// Installable are the CLIs the trainer can set up by itself, in the order it does them.
func (s *Service) Installable() []string {
	var ids []string
	for _, p := range s.list {
		if info := p.Info(); info.CanInstall {
			ids = append(ids, info.ID)
		}
	}
	return ids
}

// StopSetup cancels a running setup.
func (s *Service) StopSetup() {
	set := &s.setup
	set.mu.Lock()
	cancel := set.cancel
	set.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// StartSetup installs and logs in the providers with these ids, in the background.
func (s *Service) StartSetup(ids []string) (SetupState, error) {
	var names []string
	for _, id := range ids {
		p, ok := s.byID[id]
		if !ok {
			return SetupState{}, &ai.Error{Kind: ai.ErrOther, Msg: "unknown AI provider " + id}
		}
		names = append(names, p.Info().Name)
	}
	set := &s.setup
	set.mu.Lock()
	if set.state.Running {
		set.mu.Unlock()
		return SetupState{}, &ai.Error{Kind: ai.ErrOther, Msg: "a setup is already running"}
	}
	ctx, cancel := context.WithTimeout(s.host.Ctx, setupTimeout)
	set.cancel = cancel
	set.state = SetupState{Running: true, Step: "Setting up " + strings.Join(names, " and ") + "…"}
	st := set.state
	set.mu.Unlock()
	s.host.Publish("ai_setup", st)
	s.host.Spawn(func(context.Context) {
		defer cancel()
		s.runSetup(ctx, ids)
	})
	return st, nil
}

// runSetup installs each CLI that's missing, then opens its login and waits for it.
func (s *Service) runSetup(ctx context.Context, ids []string) {
	var failed string
	for _, id := range ids {
		p := s.byID[id]
		name := p.Info().Name
		status := s.Check(ctx, id)
		if status.State == ai.StateMissing {
			inst, ok := p.(ai.Installer)
			if !ok {
				failed = name + " has to be installed by hand"
				continue
			}
			s.setupStep(id, "Installing "+name+"…")
			if err := inst.Install(ctx, s.setupLine); err != nil {
				failed = name + ": " + err.Error()
				s.setupLine(failed)
				continue
			}
			status = s.Check(ctx, id)
		}
		if status.State == ai.StateLogin {
			l, ok := p.(ai.Loginer)
			if !ok {
				failed = name + " needs a login the trainer can't open"
				continue
			}
			s.setupStep(id, "Log in to "+name+" in the window that opened. The trainer waits here.")
			if err := l.Login(); err != nil {
				failed = name + ": " + err.Error()
				s.setupLine(failed)
				continue
			}
			status = s.waitReady(ctx, id)
		}
		switch status.State {
		case ai.StateReady:
			s.setupLine(name + " is ready.")
			s.setupEdit(func(st *SetupState) { st.Done = append(st.Done, id) })
		default:
			failed = name + ": " + status.Detail
			s.setupLine(failed)
		}
	}
	step := "Done."
	if ctx.Err() != nil {
		step, failed = "Stopped.", ""
	}
	s.setupEdit(func(st *SetupState) {
		st.Running, st.Current, st.Step, st.Error = false, "", step, failed
	})
	s.host.Publish("ai_health", s.Banner())
}

// waitReady polls a provider until its login finishes, or gives up so the page isn't stuck.
func (s *Service) waitReady(ctx context.Context, id string) ai.Status {
	deadline := time.Now().Add(loginWait)
	t := time.NewTicker(loginPoll)
	defer t.Stop()
	if st := s.Check(ctx, id); st.State == ai.StateReady {
		return st
	}
	for {
		select {
		case <-ctx.Done():
			return s.Check(s.host.Ctx, id)
		case <-t.C:
		}
		if st := s.Check(ctx, id); st.State == ai.StateReady || time.Now().After(deadline) {
			return st
		}
	}
}
