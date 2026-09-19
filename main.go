package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	iofs "io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gourdian/internal/coach"
	"gourdian/internal/config"
	"gourdian/internal/dotadata"
	"gourdian/internal/install"
	"gourdian/internal/matchdata"
	"gourdian/internal/overlay"
	"gourdian/internal/server"
	"gourdian/internal/sim"
	"gourdian/internal/speech"
	"gourdian/internal/stats"
)

const usage = `gourdian: live Dota 2 coaching from Game State Integration

Usage:
  gourdian [run] [-open] [-overlay]      start the trainer and dashboard (-record saves the raw game data)
  gourdian setup                         prepare the installed app folder and Dota config (the installer runs this)
  gourdian quit                          ask a running trainer to quit
  gourdian version                       print the version
  gourdian doctor                        check the whole setup and say what to fix
  gourdian install [-dota DIR]           write the GSI config into Dota 2
  gourdian uninstall [-dota DIR]         remove the GSI config
  gourdian overlay [-corner top-right]   show the in-game overlay (Windows)
  gourdian simulate [-speed N]           play a scripted fake match into a running trainer
  gourdian replay FILE [-speed N]        play a recording made with "run -record" into a running trainer
  gourdian stats                         summarize your recorded matches and show where the CSVs are
  gourdian mmr 2450 [note]               log your current MMR for the trend charts
  gourdian import [-n 50] [-account ID]  add your recent matches from OpenDota to the statistics
`

func main() {
	cmd, args := "run", os.Args[1:]
	if runtime.GOOS == "windows" && (len(args) == 0 || len(args) == 1 && args[0] == "--background") {
		runApp(len(args) == 1)
		return
	}
	attachConsole()
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "run":
		err = run(args)
	case "install":
		err = installCmd(args, false)
	case "uninstall":
		err = installCmd(args, true)
	case "simulate":
		err = simulate(args)
	case "overlay":
		err = overlayCmd(args)
	case "stats":
		err = statsCmd()
	case "doctor":
		err = doctor()
	case "setup":
		err = setupCmd()
	case "quit":
		stopRunningTrainer()
	case "version":
		err = versionCmd()
	case "replay":
		err = replay(args)
	case "mmr":
		err = mmrCmd(args)
	case "import":
		err = importCmd(args)
	case "help":
		fmt.Print(usage)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func openStore() (*config.Store, string, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, "", err
	}
	store, err := config.Open(dir)
	return store, dir, err
}

func run(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	listen := fs.String("listen", "", "address to listen on (default from config.json)")
	open := fs.Bool("open", false, "open the dashboard in your browser")
	openFirst := fs.Bool("open-first", false, "open the dashboard only on the very first start")
	withOverlay := fs.Bool("overlay", nativeLinux(), "also show the in-game overlay (on by default on Linux)")
	withTray := fs.Bool("tray", false, "show a tray icon (Windows)")
	background := fs.Bool("background", false, "started at sign-in: stay quiet until Dota sends data")
	record := fs.Bool("record", false, "save every game-state update to a replayable file")
	fs.Parse(args)

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
	cfg := store.Get()
	addr := cmp.Or(*listen, cfg.Listen)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cacheDir := filepath.Join(dir, "cache")
	data := dotadata.New(cacheDir, log)
	if !*background {
		data.Start(ctx)
	}

	var speaker *speech.Speaker
	// On a Linux desktop Piper's natural voice needs only something to play sound with.
	if exe, ok := speech.Available(); ok || nativeLinux() && speech.Player() != nil {
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
	if *background {
		srv.OnFirstGSI(func() { data.Start(ctx) })
	}
	if *record {
		if _, err := srv.StartRecording(); err != nil {
			return err
		}
	}
	go srv.Run(ctx)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// Launching again from the menu while it runs should just show the dashboard.
		url := "http://" + config.DashboardHost(addr)
		if *open && trainerAnswers(url) {
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

	ensureInstalled(cfg, log)
	url := "http://" + config.DashboardHost(ln.Addr().String())
	log.Info("trainer running", "dashboard", url, "data", dir)
	log.Info("in game: Ctrl+Shift+F10 moves and resizes the HUD, Ctrl+Shift+F11 opens the dashboard, Ctrl+Shift+F9 hides the HUD")
	if *open || *openFirst && firstStart {
		openBrowser(url)
	}
	if *withOverlay {
		o := overlay.Options{URL: url, Corner: "top-right", Scale: 1, Tray: *withTray, Quiet: *background, AppDir: dir, Log: log}
		startOverlay(ctx, o, stop, log)
	}

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutdownCtx)
}

// ensureInstalled writes or repairs the GSI config so a fresh install works without extra steps.
func ensureInstalled(cfg config.Config, log *slog.Logger) {
	if config.HomeOverride() != "" {
		// Test profiles must not point Dota at themselves; `gourdian install` still can.
		return
	}
	dirs := install.FindDota()
	if len(dirs) == 0 {
		log.Warn("Dota 2 install not found; if it's in a custom location run `gourdian install -dota DIR`")
		return
	}
	want := install.Render(config.GSIURI(cfg.Listen), cfg.Token)
	for _, d := range dirs {
		if got, err := os.ReadFile(install.CfgPath(d)); err == nil && string(got) == want {
			continue
		}
		if _, err := install.Write(d, config.GSIURI(cfg.Listen), cfg.Token); err != nil {
			log.Error("couldn't install the GSI config; run `gourdian install`", "dota", d, "err", err)
			continue
		}
		log.Warn("installed the Dota 2 game-state config; restart Dota 2 if it's already running", "dota", d)
	}
}

// newLogger writes to the console and to trainer.log in the data folder, keeping the previous run's log.
func newLogger(dir string) (*slog.Logger, func()) {
	console := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	logs := filepath.Join(dir, "logs")
	os.MkdirAll(logs, 0o755)
	path := filepath.Join(logs, "trainer.log")
	os.Rename(path, filepath.Join(logs, "trainer.prev.log"))
	f, err := os.Create(path)
	if err != nil {
		return slog.New(console), func() {}
	}
	// The file comes first: without a console, writes to stderr fail and MultiWriter stops there.
	w := io.MultiWriter(f, os.Stderr)
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo})), func() { f.Close() }
}

func installCmd(args []string, remove bool) error {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	dota := fs.String("dota", "", `path to the "dota 2 beta" folder (auto-detected by default)`)
	quiet := fs.Bool("quiet", false, "print nothing (used by the uninstaller)")
	fs.Parse(args)
	out := io.Writer(os.Stdout)
	if *quiet {
		out = io.Discard
	}

	store, _, err := openStore()
	if err != nil {
		return err
	}
	cfg := store.Get()
	dirs := install.FindDota()
	if *dota != "" {
		dirs = []string{install.WSLPath(*dota)}
	}
	if len(dirs) == 0 {
		return errors.New(`Dota 2 not found; pass -dota "<Steam library>/steamapps/common/dota 2 beta"`)
	}
	for _, d := range dirs {
		if remove {
			path, err := install.Remove(d)
			if err != nil {
				return err
			}
			fmt.Fprintln(out, "removed", path)
			continue
		}
		path, err := install.Write(d, config.GSIURI(cfg.Listen), cfg.Token)
		if err != nil {
			return err
		}
		fmt.Fprintln(out, "wrote", path)
	}
	if !remove {
		fmt.Fprintln(out, "\nRestart Dota 2 if it's running. Its launch options must include -gamestateintegration.")
	}
	return nil
}

func simulate(args []string) error {
	fs := flag.NewFlagSet("simulate", flag.ExitOnError)
	speed := fs.Float64("speed", 10, "game seconds per real second")
	from := fs.Int("from", -60, "starting game clock in seconds")
	to := fs.Int("to", 1590, "ending game clock in seconds")
	url := fs.String("url", "", "trainer GSI endpoint (default from config.json)")
	random := fs.Bool("random", false, "vary hero, farm, deaths and result instead of playing the fixed script")
	fs.Parse(args)
	if *speed <= 0 {
		return errors.New("-speed must be positive")
	}
	var seed uint64
	if *random {
		seed = uint64(time.Now().UnixNano())
	}

	store, _, err := openStore()
	if err != nil {
		return err
	}
	cfg := store.Get()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opts := sim.Options{URL: cmp.Or(*url, config.GSIURI(cfg.Listen)), Token: cfg.Token, Speed: *speed, From: *from, To: *to, Seed: seed}
	fmt.Printf("simulating a match into %s at %gx speed (Ctrl+C to stop)\n", opts.URL, *speed)
	err = sim.Run(ctx, opts, func(clock, status int) {
		if clock%60 == 0 {
			fmt.Printf("  clock %d:00\n", clock/60)
		}
	})
	if errors.Is(err, context.Canceled) {
		return nil
	}
	if err == nil {
		fmt.Println("match finished; check the dashboard's recent matches")
	}
	return err
}

func statsCmd() error {
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	st, err := stats.Open(dir)
	if err != nil {
		return err
	}
	defer st.Close()
	matches, err := st.Matches()
	if err != nil {
		return err
	}
	fmt.Println("CSV files:", st.Dir())
	var real []stats.MatchSummary
	for _, m := range matches {
		if m.Real() {
			real = append(real, m)
		}
	}
	fmt.Printf("%d matches recorded (%d simulated or practice)\n", len(matches), len(matches)-len(real))
	if len(real) == 0 && len(matches) > 0 {
		fmt.Println("No real matches yet, so these numbers come from simulated and practice ones:")
		real = matches
	}
	for _, window := range []struct {
		label    string
		from, to int
	}{{"last 10", len(real) - 10, len(real)}, {"10 before", len(real) - 20, len(real) - 10}} {
		part := real[max(window.from, 0):max(window.to, 0)]
		if len(part) == 0 {
			continue
		}
		var wins, deaths, gpm, lh10, lh10n int
		for _, m := range part {
			if m.Result == "win" {
				wins++
			}
			deaths += m.Deaths
			gpm += m.GPM
			if lh, ok := m.LastHitsAt["10:00"]; ok {
				lh10 += lh
				lh10n++
			}
		}
		n := len(part)
		fmt.Printf("  %-9s  %2d games  win %3.0f%%  deaths %4.1f  GPM %4d  LH@10 %s\n", window.label, n,
			100*float64(wins)/float64(n), float64(deaths)/float64(n), gpm/n, avgOrDash(lh10, lh10n))
	}
	if mmr, err := st.MMR(); err == nil && len(mmr) > 0 {
		first, last := mmr[0], mmr[len(mmr)-1]
		fmt.Printf("MMR: %d on %s -> %d on %s (%+d)\n", first.MMR, first.Date.Format("2006-01-02"), last.MMR,
			last.Date.Format("2006-01-02"), last.MMR-first.MMR)
	}
	if reviews, err := st.Reviews(); err == nil && len(reviews) > 0 {
		fmt.Println("Current focus:", reviews[len(reviews)-1].NextGameFocus)
	}
	fmt.Println("Charts: open the dashboard and click Stats (http://127.0.0.1:4570/stats.html)")
	return nil
}

func avgOrDash(sum, n int) string {
	if n == 0 {
		return "-"
	}
	return fmt.Sprint(sum / n)
}

func importCmd(args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	n := fs.Int("n", 50, "how many recent matches to look at (up to 100)")
	account := fs.String("account", "", "Steam account id (default: the one Dota reported)")
	fs.Parse(args)

	store, dir, err := openStore()
	if err != nil {
		return err
	}
	accountID := cmp.Or(*account, store.Settings().AccountID)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	data := dotadata.New(filepath.Join(dir, "cache"), log)
	data.Start(ctx)
	st, err := stats.Open(dir)
	if err != nil {
		return err
	}
	defer st.Close()
	svc := matchdata.Service{Data: data, Stats: st, Log: log}
	fmt.Printf("importing up to %d matches for account %s (about one per second)\n", min(max(*n, 1), 100), accountID)
	added, err := svc.Import(ctx, accountID, min(max(*n, 1), 100), func(p matchdata.ImportProgress) {
		fmt.Printf("\r  %d/%d checked, %d added", p.Done, p.Total, p.Added)
	})
	fmt.Println()
	if err != nil {
		return err
	}
	fmt.Printf("added %d matches to %s\n", added, filepath.Join(dir, "stats"))
	return nil
}

func mmrCmd(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: gourdian mmr 2450 [note]")
	}
	mmr, err := strconv.Atoi(args[0])
	if err != nil || mmr <= 0 || mmr > 20000 {
		return fmt.Errorf("%q is not an MMR value", args[0])
	}
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	st, err := stats.Open(dir)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.AppendMMR(stats.MMREntry{Date: time.Now(), MMR: mmr, Note: strings.Join(args[1:], " ")}); err != nil {
		return err
	}
	fmt.Println("logged MMR", mmr, "in", st.Dir())
	return nil
}

func overlayCmd(args []string) error {
	fs := flag.NewFlagSet("overlay", flag.ExitOnError)
	url := fs.String("url", "", "trainer address (default from config.json)")
	corner := fs.String("corner", "top-right", "top-right, top-left or top-center")
	x := fs.Int("x", 0, "horizontal offset in pixels (at 100% scaling)")
	y := fs.Int("y", 0, "vertical offset in pixels (at 100% scaling)")
	scale := fs.Float64("scale", 1, "size multiplier on top of Windows display scaling")
	snapshot := fs.String("snapshot", "", "render one HUD frame to this PNG file and exit")
	snapshotEditing := fs.Bool("editing", false, "with -snapshot, draw the HUD as it looks while its layout is edited")
	fs.Parse(args)

	base := *url
	if base == "" {
		store, _, err := openStore()
		if err != nil {
			return err
		}
		base = "http://" + config.DashboardHost(store.Get().Listen)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return overlay.Run(ctx, overlay.Options{URL: base, Corner: *corner, OffsetX: *x, OffsetY: *y, Scale: *scale,
		Snapshot: *snapshot, SnapshotEditing: *snapshotEditing})
}

// nativeLinux is a Linux desktop playing Dota itself, as opposed to WSL coaching Windows.
func nativeLinux() bool { return runtime.GOOS == "linux" && os.Getenv("WSL_DISTRO_NAME") == "" }

// startOverlay runs the overlay in-process on Windows and Linux desktops; under WSL it launches
// the Windows build sitting next to this binary, since only a Windows process can draw over the game.
func startOverlay(ctx context.Context, o overlay.Options, stop func(), log *slog.Logger) {
	if nativeLinux() {
		go func() {
			// The trainer still coaches by voice and the dashboard without a HUD.
			if err := overlay.Run(ctx, o); err != nil {
				log.Error("HUD stopped", "err", err)
			}
		}()
		return
	}
	if runtime.GOOS == "windows" {
		go func() {
			if err := overlay.Run(ctx, o); err != nil {
				log.Error("overlay stopped", "err", err)
			}
			// Its windows also close when Windows or an installer asks the app to exit; exit with them.
			stop()
		}()
		return
	}
	self, err := os.Executable()
	if err != nil {
		log.Error("overlay: locate executable", "err", err)
		return
	}
	exe := filepath.Join(filepath.Dir(self), "gourdian.exe")
	if _, err := os.Stat(exe); err != nil {
		log.Warn("overlay needs the Windows build next to this binary; run `make`", "missing", exe)
		return
	}
	cmd := exec.CommandContext(ctx, exe, "overlay", "-url", o.URL, "-corner", o.Corner)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		log.Error("overlay: start", "err", err)
		return
	}
	log.Info("overlay started")
	go cmd.Wait()
}

func replay(args []string) error {
	fs := flag.NewFlagSet("replay", flag.ExitOnError)
	speed := fs.Float64("speed", 1, "playback speed multiplier")
	url := fs.String("url", "", "trainer GSI endpoint (default from config.json)")
	fs.Parse(reorderFlags(args))
	if fs.NArg() != 1 || *speed <= 0 {
		return errors.New("usage: gourdian replay [-speed N] FILE")
	}
	store, _, err := openStore()
	if err != nil {
		return err
	}
	cfg := store.Get()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	opts := sim.Options{URL: cmp.Or(*url, config.GSIURI(cfg.Listen)), Token: cfg.Token, Speed: *speed}
	fmt.Printf("replaying %s into %s at %gx speed (Ctrl+C to stop)\n", fs.Arg(0), opts.URL, *speed)
	err = sim.Replay(ctx, fs.Arg(0), opts, func(n int) {
		if n%500 == 0 {
			fmt.Printf("  %d updates sent\n", n)
		}
	})
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// reorderFlags moves flags ahead of positional arguments, since the flag package stops at
// the first positional one and `replay FILE -speed 10` is the natural way to type it.
func reorderFlags(args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			flags = append(flags, args[i])
			if !strings.Contains(args[i], "=") && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
		} else {
			positional = append(positional, args[i])
		}
	}
	return append(flags, positional...)
}

// trainerAnswers reports whether a trainer is already serving the dashboard at url.
func trainerAnswers(url string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url + "/api/state")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch {
	case runtime.GOOS == "windows":
		overlay.OpenDashboard(url, true)
		return
	case runtime.GOOS == "darwin":
		cmd = exec.Command("open", url)
	default:
		if exe, err := exec.LookPath("cmd.exe"); err == nil {
			cmd = windowsCommand(exe, "/c", "start", url)
		} else {
			cmd = exec.Command("xdg-open", url)
		}
	}
	_ = cmd.Start()
}
