package server

import (
	"context"
	"encoding/json"
	"net/http"
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

// loginPoll is how often the trainer re-checks while a login window is open; tests shorten it.
var loginPoll = 5 * time.Second

// setupState is what the AI page shows while the trainer installs and connects a CLI.
type setupState struct {
	Running bool     `json:"running"`
	Step    string   `json:"step"`
	Current string   `json:"current"`
	Log     []string `json:"log"`
	Done    []string `json:"done"`
	Error   string   `json:"error"`
}

type aiSetup struct {
	mu     sync.Mutex
	state  setupState
	cancel context.CancelFunc
}

func (s *Server) setupSnapshot() setupState {
	set := &s.ai.setup
	set.mu.Lock()
	defer set.mu.Unlock()
	st := set.state
	st.Log = slices.Clone(st.Log)
	st.Done = slices.Clone(st.Done)
	return st
}

// setupEdit changes the state and tells the dashboard, so progress shows as it happens.
func (s *Server) setupEdit(f func(*setupState)) {
	set := &s.ai.setup
	set.mu.Lock()
	f(&set.state)
	if n := len(set.state.Log); n > setupLogLines {
		set.state.Log = set.state.Log[n-setupLogLines:]
	}
	st := set.state
	st.Log, st.Done = slices.Clone(st.Log), slices.Clone(st.Done)
	set.mu.Unlock()
	s.hub.publish("ai_setup", st)
}

func (s *Server) setupStep(id, text string) {
	s.log.Info("AI setup", "provider", id, "step", text)
	s.setupEdit(func(st *setupState) {
		st.Current, st.Step = id, text
		st.Log = append(st.Log, text)
	})
}

func (s *Server) setupLine(line string) {
	s.setupEdit(func(st *setupState) { st.Log = append(st.Log, line) })
}

// installable are the CLIs the trainer can set up by itself, in the order it does them.
func (s *Server) installable() []string {
	var ids []string
	for _, p := range s.ai.providers.list {
		if info := p.Info(); info.CanInstall {
			ids = append(ids, info.ID)
		}
	}
	return ids
}

func (s *Server) handleAISetup(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.setupSnapshot())
}

func (s *Server) handleAISetupStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body)
	if len(body.IDs) == 0 {
		body.IDs = s.installable()
	}
	st, err := s.startSetup(body.IDs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, st)
}

func (s *Server) handleAISetupStop(w http.ResponseWriter, r *http.Request) {
	set := &s.ai.setup
	set.mu.Lock()
	cancel := set.cancel
	set.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	writeJSON(w, s.setupSnapshot())
}

func (s *Server) startSetup(ids []string) (setupState, error) {
	var names []string
	for _, id := range ids {
		p, ok := s.ai.providers.byID[id]
		if !ok {
			return setupState{}, &ai.Error{Kind: ai.ErrOther, Msg: "unknown AI provider " + id}
		}
		names = append(names, p.Info().Name)
	}
	set := &s.ai.setup
	set.mu.Lock()
	if set.state.Running {
		set.mu.Unlock()
		return setupState{}, &ai.Error{Kind: ai.ErrOther, Msg: "a setup is already running"}
	}
	ctx, cancel := context.WithTimeout(s.baseCtx, setupTimeout)
	set.cancel = cancel
	set.state = setupState{Running: true, Step: "Setting up " + strings.Join(names, " and ") + "…"}
	st := set.state
	set.mu.Unlock()
	s.hub.publish("ai_setup", st)
	s.spawn(func(context.Context) {
		defer cancel()
		s.runSetup(ctx, ids)
	})
	return st, nil
}

// runSetup installs each CLI that's missing, then opens its login and waits for it.
func (s *Server) runSetup(ctx context.Context, ids []string) {
	var failed string
	for _, id := range ids {
		p := s.ai.providers.byID[id]
		name := p.Info().Name
		status := s.checkProvider(ctx, id)
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
			status = s.checkProvider(ctx, id)
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
			s.setupEdit(func(st *setupState) { st.Done = append(st.Done, id) })
		default:
			failed = name + ": " + status.Detail
			s.setupLine(failed)
		}
	}
	step := "Done."
	if ctx.Err() != nil {
		step, failed = "Stopped.", ""
	}
	s.setupEdit(func(st *setupState) {
		st.Running, st.Current, st.Step, st.Error = false, "", step, failed
	})
	s.hub.publish("ai_health", s.aiBanner())
}

// waitReady polls a provider until its login finishes, or gives up so the page isn't stuck.
func (s *Server) waitReady(ctx context.Context, id string) ai.Status {
	deadline := time.Now().Add(loginWait)
	t := time.NewTicker(loginPoll)
	defer t.Stop()
	if st := s.checkProvider(ctx, id); st.State == ai.StateReady {
		return st
	}
	for {
		select {
		case <-ctx.Done():
			return s.checkProvider(s.baseCtx, id)
		case <-t.C:
		}
		if st := s.checkProvider(ctx, id); st.State == ai.StateReady || time.Now().After(deadline) {
			return st
		}
	}
}
