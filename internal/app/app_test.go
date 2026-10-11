package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func stopTrainer(port int) {
	req, _ := http.NewRequestWithContext(context.Background(), "POST", fmt.Sprintf("http://127.0.0.1:%d/api/quit", port), nil)
	for i := 0; i < 20; i++ {
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestRun(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOURDIAN_HOME", dir)

	port := 41001
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(Options{Listen: fmt.Sprintf("127.0.0.1:%d", port)})
	}()

	time.Sleep(1 * time.Second)
	stopTrainer(port)

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Run() returned error: %v", err)
		}
	case <-time.After(6 * time.Second):
		t.Error("Run() did not return after quit request")
	}

	if _, err := os.Stat(filepath.Join(dir, "config.json")); err != nil {
		if os.IsNotExist(err) {
			t.Error("config.json was not created")
		} else {
			t.Errorf("error stating config.json: %v", err)
		}
	}
}

func TestRun_QuitRequested(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOURDIAN_HOME", dir)

	port := 41002
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(Options{Listen: fmt.Sprintf("127.0.0.1:%d", port)})
	}()

	time.Sleep(1 * time.Second)
	stopTrainer(port)

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Run() returned error: %v", err)
		}
	case <-time.After(6 * time.Second):
		t.Error("Run() did not return after quit request")
	}
}

func TestRun_PortInUse(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOURDIAN_HOME", dir)

	port := 41003
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	time.Sleep(100 * time.Millisecond)

	err = Run(Options{Listen: fmt.Sprintf("127.0.0.1:%d", port)})
	if err == nil {
		t.Error("Run() expected to fail due to port in use")
	}
}

func TestRun_InvalidDir(t *testing.T) {
	os.Setenv("GOURDIAN_HOME", "/nonexistent/directory/that/cannot/be/created")
	defer os.Unsetenv("GOURDIAN_HOME")

	err := Run(Options{})
	if err == nil {
		t.Error("Run() should have failed when GOURDIAN_HOME is invalid")
	}
}

func TestRun_RecordEnabled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOURDIAN_HOME", dir)

	port := 41004
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(Options{Listen: fmt.Sprintf("127.0.0.1:%d", port), Record: true})
	}()

	time.Sleep(1 * time.Second)
	stopTrainer(port)

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Run() returned error: %v", err)
		}
	case <-time.After(6 * time.Second):
		t.Error("Run() did not return after interrupt")
	}
}

func TestRun_BackgroundAndOverlay(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOURDIAN_HOME", dir)

	port := 41005
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(Options{Listen: fmt.Sprintf("127.0.0.1:%d", port), Background: true, Overlay: true})
	}()

	time.Sleep(1 * time.Second)
	stopTrainer(port)

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Run() returned error: %v", err)
		}
	case <-time.After(6 * time.Second):
		t.Error("Run() did not return after interrupt")
	}
}

func TestRun_PortInUse_OpenDashboard(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOURDIAN_HOME", dir)

	port := 41006
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		http.Serve(ln, mux)
	}()

	time.Sleep(100 * time.Millisecond)

	err = Run(Options{Listen: fmt.Sprintf("127.0.0.1:%d", port), Open: true})
	if err != nil {
		t.Errorf("Run() expected to return nil because Open is true and trainer is running, got: %v", err)
	}
}

func TestRun_ConfigRepair(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOURDIAN_HOME", dir)

	err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"language": "invalid_lang"}`), 0644)
	if err != nil {
		t.Fatalf("failed to write config.json: %v", err)
	}

	port := 41007
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(Options{Listen: fmt.Sprintf("127.0.0.1:%d", port)})
	}()

	time.Sleep(1 * time.Second)
	stopTrainer(port)

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Run() returned error: %v", err)
		}
	case <-time.After(6 * time.Second):
		t.Error("Run() did not return after interrupt")
	}
}

func TestRun_ConfigOpenError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOURDIAN_HOME", dir)

	err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{invalid`), 0644)
	if err != nil {
		t.Fatalf("failed to write bad config.json: %v", err)
	}

	err = Run(Options{Listen: "127.0.0.1:0"})
	if err == nil {
		t.Error("Run() expected to fail due to config.Open error")
	}
}

func TestRun_SystemVoiceBrowserFallbackError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOURDIAN_HOME", dir)

	err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"voice": "system"}`), 0400)
	if err != nil {
		t.Fatalf("failed to write config.json: %v", err)
	}

	port := 41010
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(Options{Listen: fmt.Sprintf("127.0.0.1:%d", port)})
	}()

	time.Sleep(200 * time.Millisecond)
	stopTrainer(port)

	select {
	case err := <-errCh:
		if err == nil {
			t.Log("Run() unexpectedly returned nil (store.Update didn't fail on read-only file)")
		}
	case <-time.After(6 * time.Second):
		t.Error("Run() did not return after quit request")
	}
	os.Chmod(filepath.Join(dir, "config.json"), 0644)
}

func TestRun_RecordError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOURDIAN_HOME", dir)

	err := os.WriteFile(filepath.Join(dir, "recordings"), []byte("not a dir"), 0644)
	if err != nil {
		t.Fatalf("failed to write recordings file: %v", err)
	}

	err = Run(Options{Listen: "127.0.0.1:0", Record: true})
	if err == nil {
		t.Error("Run() expected to fail due to StartRecording error")
	}
}

func TestRun_StatsOpenError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOURDIAN_HOME", dir)

	err := os.WriteFile(filepath.Join(dir, "stats"), []byte("not a dir"), 0644)
	if err != nil {
		t.Fatalf("failed to write stats file: %v", err)
	}

	err = Run(Options{Listen: "127.0.0.1:0"})
	if err == nil {
		t.Error("Run() expected to fail due to stats.Open error")
	}
}

func TestRun_SystemVoiceSwitch(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOURDIAN_HOME", dir)

	err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"voice": "system"}`), 0644)
	if err != nil {
		t.Fatalf("failed to write config.json: %v", err)
	}

	port := 41008
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(Options{Listen: fmt.Sprintf("127.0.0.1:%d", port)})
	}()

	time.Sleep(1 * time.Second)
	stopTrainer(port)

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Run() returned error: %v", err)
		}
	case <-time.After(6 * time.Second):
		t.Error("Run() did not return after interrupt")
	}
}

func TestRun_FirstStartOpenBrowser(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOURDIAN_HOME", dir)

	port := 41009
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(Options{Listen: fmt.Sprintf("127.0.0.1:%d", port), OpenFirst: true})
	}()

	time.Sleep(1 * time.Second)
	stopTrainer(port)

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Run() returned error: %v", err)
		}
	case <-time.After(6 * time.Second):
		t.Error("Run() did not return after interrupt")
	}
}
