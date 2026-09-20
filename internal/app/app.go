// Package app starts the trainer: it builds the settings store, the logger, the OpenDota client,
// the voice, the rules engine, the statistics and the HTTP server, runs them until the process is
// asked to stop, and shuts them down in order. Commands parse flags and call Run; everything about
// how the pieces fit together lives here.
package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	iofs "io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"gourdian/internal/coach"
	"gourdian/internal/config"
	"gourdian/internal/dotadata"
	"gourdian/internal/overlay"
	"gourdian/internal/platform"
	"gourdian/internal/server"
	"gourdian/internal/speech"
	"gourdian/internal/stats"
)

// Options is what the `run` command decides from its flags.
type Options struct {
	// Listen overrides the address in config.json.
	Listen string
	// Open opens the dashboard in a browser once the trainer is up; OpenFirst only does so
	// on the very first start.
	Open      bool
	OpenFirst bool
	// Overlay draws the in-game HUD, with Tray adding a tray icon on Windows.
	Overlay bool
	Tray    bool
	// Background is set when Windows starts the app at sign-in: stay quiet until Dota sends data.
	Background bool
	// Record saves every game-state update to a replayable file.
	Record bool
}

// Run starts the trainer and returns when it has stopped, either on Ctrl+C or because the
// dashboard asked it to quit.
func Run(o Options) error {
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	_, statErr := os.Stat(filepath.Join(dir, "config.json"))
	firstStart := errors.Is(statErr, iofs.ErrNotExist)
	store, err := config.Open(dir)
	if err != nil {
		return err
	}
	log, closeLog := newLogger(dir)
	defer closeLog()
	if reset := store.Repaired(); len(reset) > 0 {
		log.Warn("config.json had settings the trainer can't use; they're back to their defaults, and the file as it was is config.json.bad", "settings", reset)
	}
	cfg := store.Get()
	addr := cmp.Or(o.Listen, cfg.Listen)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cacheDir := filepath.Join(dir, "cache")
	data := dotadata.New(cacheDir, log)
	if !o.Background {
		data.Start(ctx)
	}

	var speaker *speech.Speaker
	// On a Linux desktop Piper's natural voice needs only something to play sound with.
	if exe, ok := speech.Available(); ok || platform.LinuxDesktop() && speech.Player() != nil {
		speaker = speech.New(exe, cfg.Settings.VoiceRate, log)
		speaker.SetLanguage(cfg.Settings.Language)
		defer speaker.Close()
		log.Info("speaking through " + speaker.Name())
	} else if cfg.Settings.Voice == config.VoiceSystem {
		if _, err := store.Update(func(set *config.Settings) error {
			set.Voice = config.VoiceBrowser
			return nil
		}); err != nil {
			return err
		}
		log.Warn("no speech on this machine (install PipeWire or alsa-utils for the natural voice); switched voice to the browser")
	}

	engine := coach.New(data, log)
	st, err := stats.Open(dir)
	if err != nil {
		return err
	}
	defer st.Close()
	st.OnError(func(msg string, err error) { log.Warn(msg, "err", err) })
	srv := server.New(store, engine, st, data, speaker, dir, cacheDir, log)
	defer srv.Close()
	srv.OnQuit(stop)
	if o.Background {
		srv.OnFirstGSI(func() { data.Start(ctx) })
	}
	if o.Record {
		if _, err := srv.StartRecording(); err != nil {
			return err
		}
	}
	go srv.Run(ctx)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// Launching again from the menu while it runs should just show the dashboard.
		url := "http://" + config.DashboardHost(addr)
		if o.Open && trainerAnswers(url) {
			openBrowser(url)
			return nil
		}
		return fmt.Errorf("listen on %s (is the trainer already running?): %w", addr, err)
	}
	httpSrv := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// Dashboard event streams never finish on their own; ending them on Ctrl+C lets Shutdown return.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server stopped", "err", err)
			stop()
		}
	}()

	EnsureInstalled(cfg, log)
	url := "http://" + config.DashboardHost(ln.Addr().String())
	log.Info("trainer running", "dashboard", url, "data", dir)
	log.Info("in game: Ctrl+Shift+F10 moves and resizes the HUD, Ctrl+Shift+F11 opens the dashboard, Ctrl+Shift+F9 hides the HUD")
	if o.Open || o.OpenFirst && firstStart {
		openBrowser(url)
	}
	if o.Overlay {
		opts := overlay.Options{URL: url, Corner: "top-right", Scale: 1, Tray: o.Tray, Quiet: o.Background, AppDir: dir, Log: log}
		startOverlay(ctx, opts, stop, log)
	}

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutdownCtx)
}
