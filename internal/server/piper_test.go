package server

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"dotatrainer/internal/config"
	"dotatrainer/internal/speech"
)

// linuxDesktop makes the trainer think it runs on a Linux desktop whose only sound player is a
// stand-in, so nothing is played.
func linuxDesktop(t *testing.T) {
	t.Setenv("WSL_DISTRO_NAME", "")
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "pw-play"), []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", bin)
}

func TestNaturalVoiceOnLinux(t *testing.T) {
	linuxDesktop(t)
	srv, _, dir := newTestServer(t, func(s *config.Settings) { s.Voice = config.VoiceSystem; s.Language = "ru" })
	srv.speaker = speech.New("", 0, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer srv.speaker.Close()
	// Pretend a failed download already happened, so the test doesn't reach the internet.
	srv.piper.failed = "offline"

	nv := srv.naturalVoiceStatus()
	if nv == nil || nv.State != "failed" || nv.Chosen["ru"] != "ru_RU-irina-medium" || nv.Player != "pw-play" {
		t.Fatalf("status = %+v", nv)
	}
	if checks := srv.piperChecks(srv.cfg.Settings()); len(checks) != 1 || checks[0].Fix != "install_voice" {
		t.Fatalf("checks = %+v", checks)
	}

	home := filepath.Join(dir, "piper")
	os.MkdirAll(filepath.Join(home, "bin", "piper"), 0o755)
	os.WriteFile(filepath.Join(home, "bin", "piper", "piper"), []byte("#!/bin/sh\n"), 0o755)
	os.MkdirAll(filepath.Join(home, "voices"), 0o755)
	model := filepath.Join(home, "voices", "ru_RU-irina-medium.onnx")
	f, _ := os.Create(model)
	f.Truncate(63201294)
	f.Close()
	os.WriteFile(model+".json", []byte("{}"), 0o644)

	srv.ensurePiper()
	if nv := srv.naturalVoiceStatus(); nv.State != "ready" {
		t.Fatalf("with Piper and the voice in place: %+v", nv)
	}
	if srv.piper.applied == "" {
		t.Fatal("the downloaded voice wasn't given to the speaker")
	}
	if checks := srv.piperChecks(srv.cfg.Settings()); len(checks) != 1 || !checks[0].OK || checks[0].Detail != "Ирина" {
		t.Fatalf("checks = %+v", checks)
	}
}
