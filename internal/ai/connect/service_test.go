package connect

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"gourdian/internal/ai"
	"gourdian/internal/ai/secrets"
	"gourdian/internal/sys/config"
)

// testService is a service with a fresh settings file and nothing listening to it.
func testService(t *testing.T, providers ...ai.Provider) *Service {
	t.Helper()
	dir := t.TempDir()
	store, err := config.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := New(Host{Settings: store, Keys: secrets.Open(dir), WorkDir: dir, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Ctx: t.Context(), Spawn: func(work func(context.Context)) { go work(t.Context()) },
		Publish: func(string, any) {}, PublishSettings: func() {}, Paused: func(Health) {}, Resumed: func() {}})
	if len(providers) > 0 {
		s.UseProviders(providers)
	}
	return s
}

// A usage limit pauses the provider, and the pause ends by itself.
func TestUsageLimitBacksOff(t *testing.T) {
	srv := testService(t)
	claude, _ := srv.Get("claude")
	srv.Failed(claude, errors.New("claude (success): Claude AI usage limit reached|1789000000"))
	if srv.Ready() {
		t.Fatal("limit should pause the coach")
	}
	srv.mu.Lock()
	h := srv.health["claude"]
	h.Until = time.Now().Add(-time.Second)
	srv.health["claude"] = h
	srv.mu.Unlock()
	if !srv.Ready() {
		t.Fatal("coach should resume after the back-off")
	}
}

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

func waitSetup(t *testing.T, s *Service) SetupState {
	t.Helper()
	for range 200 {
		if st := s.Setup(); !st.Running {
			return st
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the setup never finished")
	return SetupState{}
}

func TestSetupInstallsThenLogsIn(t *testing.T) {
	f := &fakeCLI{}
	srv := testService(t, f)
	if _, err := srv.StartSetup([]string{"fake"}); err != nil {
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
	srv := testService(t, &fakeCLI{installErr: errors.New("no network")})
	if _, err := srv.StartSetup([]string{"fake"}); err != nil {
		t.Fatal(err)
	}
	st := waitSetup(t, srv)
	if st.Error == "" || st.Running {
		t.Fatalf("state = %+v", st)
	}
}

func TestOnlyOneSetupAtATime(t *testing.T) {
	srv := testService(t, &fakeCLI{})
	srv.setup.state = SetupState{Running: true}
	if _, err := srv.StartSetup([]string{"fake"}); err == nil {
		t.Fatal("a second setup started while one was running")
	}
	srv.setup.state = SetupState{}
}

// A short watch after a login must not clear the mark of the open-ended watch still running
// for the same provider, or the next logout starts a second one.
func TestLoginWatchLeavesTheOpenEndedWatchMarked(t *testing.T) {
	srv := testService(t, &fakeCLI{})
	srv.setProblem(Health{Problem: LoggedOut, Provider: "fake"})
	watching := func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return srv.watching["fake"]
	}
	go srv.WatchLogin("fake", time.Hour, 0) // waits for a logout to clear, for as long as it takes
	for deadline := time.Now().Add(5 * time.Second); !watching(); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the open-ended watch never started")
		}
	}
	srv.WatchLogin("fake", time.Millisecond, 5*time.Millisecond) // the short watch after a login
	if !watching() {
		t.Fatal("the short watch cleared the open-ended watch's mark")
	}
}
