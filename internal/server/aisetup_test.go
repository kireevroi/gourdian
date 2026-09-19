package server

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"dotatrainer/internal/ai"
)

// fakeCLI is a provider that needs installing and logging in, like the real CLIs.
type fakeCLI struct {
	installed, loggedIn bool
	installErr          error
}

func (f *fakeCLI) Info() ai.Info {
	return ai.Info{ID: "fake", Name: "Fake CLI", Kind: "cli", CanInstall: true}
}

func (f *fakeCLI) Status(context.Context) ai.Status {
	switch {
	case !f.installed:
		return ai.Status{State: ai.StateMissing, Detail: "not installed"}
	case !f.loggedIn:
		return ai.Status{State: ai.StateLogin, Detail: "not logged in"}
	}
	return ai.Status{State: ai.StateReady}
}

func (f *fakeCLI) Complete(context.Context, ai.Request) (json.RawMessage, error) { return nil, nil }

func (f *fakeCLI) Install(ctx context.Context, progress func(string)) error {
	if f.installErr != nil {
		return f.installErr
	}
	progress("downloading…")
	f.installed = true
	return nil
}

func (f *fakeCLI) Login() error {
	f.loggedIn = true
	return nil
}

func waitSetup(t *testing.T, s *Server) setupState {
	t.Helper()
	for range 200 {
		if st := s.setupSnapshot(); !st.Running {
			return st
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the setup never finished")
	return setupState{}
}

func TestSetupInstallsThenLogsIn(t *testing.T) {
	srv, _, _ := newTestServer(t, nil)
	f := &fakeCLI{}
	srv.ai.providers = newProviders([]ai.Provider{f})
	if _, err := srv.startSetup([]string{"fake"}); err != nil {
		t.Fatal(err)
	}
	st := waitSetup(t, srv)
	if !f.installed || !f.loggedIn {
		t.Fatalf("installed=%v loggedIn=%v", f.installed, f.loggedIn)
	}
	if st.Error != "" || len(st.Done) != 1 || st.Done[0] != "fake" {
		t.Fatalf("state = %+v", st)
	}
}

func TestSetupReportsAFailedInstall(t *testing.T) {
	srv, _, _ := newTestServer(t, nil)
	srv.ai.providers = newProviders([]ai.Provider{&fakeCLI{installErr: errors.New("no network")}})
	if _, err := srv.startSetup([]string{"fake"}); err != nil {
		t.Fatal(err)
	}
	st := waitSetup(t, srv)
	if st.Error == "" || st.Running {
		t.Fatalf("state = %+v", st)
	}
}

func TestOnlyOneSetupAtATime(t *testing.T) {
	srv, _, _ := newTestServer(t, nil)
	srv.ai.providers = newProviders([]ai.Provider{&fakeCLI{}})
	srv.ai.setup.state = setupState{Running: true}
	if _, err := srv.startSetup([]string{"fake"}); err == nil {
		t.Fatal("a second setup started while one was running")
	}
	srv.ai.setup.state = setupState{}
}
