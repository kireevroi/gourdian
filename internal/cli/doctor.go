package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gourdian/internal/ai"
	"gourdian/internal/config"
	"gourdian/internal/dotadata"
	"gourdian/internal/hidewin"
	"gourdian/internal/install"
	"gourdian/internal/screen"
	"gourdian/internal/secrets"
	"gourdian/internal/server"
	"gourdian/internal/speech"
)

type checker struct{ problems int }

func (c *checker) ok(format string, args ...any) { fmt.Printf("  ok    "+format+"\n", args...) }

func (c *checker) warn(format string, args ...any) { fmt.Printf("  warn  "+format+"\n", args...) }

func (c *checker) fail(format string, args ...any) {
	c.problems++
	fmt.Printf("  FAIL  "+format+"\n", args...)
}

func doctor() error {
	var c checker
	store, _, err := config.OpenDefault()
	if err != nil {
		return err
	}
	cfg := store.Get()
	set := cfg.Settings
	fmt.Println("Setup")
	c.ok("config: %s", store.Path())

	dirs := install.FindDota()
	if len(dirs) == 0 {
		c.fail("Dota 2 not found. Run `gourdian install -dota \"<library>/steamapps/common/dota 2 beta\"`")
	}
	want := install.Render(config.GSIURI(cfg.Listen), cfg.Token)
	for _, d := range dirs {
		got, err := os.ReadFile(install.CfgPath(d))
		switch {
		case err != nil:
			c.fail("GSI config missing in %s. Run `gourdian install`, then restart Dota", d)
		case string(got) != want:
			c.fail("GSI config in %s is out of date. Run `gourdian install`, then restart Dota", d)
		default:
			c.ok("GSI config installed: %s", install.CfgPath(d))
		}
		switch mode, err := install.VideoMode(d); {
		case err != nil:
			c.warn("couldn't read Dota's display mode: %v", err)
		case mode == install.Exclusive:
			c.warn("Dota runs in exclusive fullscreen, which hides the overlay. Switch to borderless window in Dota's video settings")
		default:
			c.ok("Dota display mode: %s (overlay can draw on top)", mode)
		}
	}
	// Valve sends the draft only to spectators, so pick advice is built without it. The
	// trainer still watches for one, and says so here if a real install ever gets it.
	if at, err := os.ReadFile(filepath.Join(filepath.Dir(store.Path()), server.DraftSeenFile)); err == nil {
		c.ok("Dota has sent a draft board (first seen %s); counter-pick advice can use it", strings.TrimSpace(string(at)))
	} else {
		c.ok("no draft board from Dota, as expected: Valve sends picks to spectators only")
	}

	// Reading the enemy picks means reading the screen, so say plainly whether that is on and
	// whether this machine can do it at all.
	switch {
	case !set.Screen.Draft:
		c.ok("not reading the screen (Settings > Pick help turns it on for the enemy picks)")
	case screen.Wayland():
		c.fail("reading the screen is on, but this is a Wayland session where a program can't; log in with X11")
	default:
		if size, err := screen.Size(); err != nil {
			c.fail("reading the screen is on, but the screen can't be read: %v", err)
		} else {
			bar := screen.Predict(size)
			c.ok("reading the screen: %dx%d, expecting the portraits at %d,%d and %d,%d",
				size.Dx(), size.Dy(), bar.Left.X, bar.Y(), bar.Right.X, bar.Y())
		}
	}

	if opts := install.LaunchOptions(); len(opts) > 0 {
		for _, o := range opts {
			if strings.Contains(o, "-gamestateintegration") {
				c.ok("launch options include -gamestateintegration (%q)", o)
			} else {
				c.warn("launch options %q lack -gamestateintegration; add it in Steam > Dota 2 > Properties if no data arrives", o)
			}
		}
	}

	fmt.Println("Trainer")
	base := "http://" + config.DashboardHost(cfg.Listen)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(base + "/api/state")
	switch {
	case err == nil:
		resp.Body.Close()
		c.ok("trainer answering at %s", base)
		if curl, err := exec.LookPath("curl.exe"); err == nil && runtime.GOOS != "windows" {
			out, err := hidewin.WindowsCommand(curl, "-s", "-o", "NUL", "-w", "%{http_code}", base+"/api/state").Output()
			if err == nil && strings.TrimSpace(string(out)) == "200" {
				c.ok("Windows (where Dota runs) can reach the trainer")
			} else {
				c.fail("Windows can't reach %s. Check that WSL localhost forwarding is on (localhostForwarding=true in .wslconfig)", base)
			}
		}
	case trainerCall("GET", base+"/api/state"):
		c.ok("Gourdian app is running on Windows at %s", base)
	default:
		c.warn("trainer isn't running. Start it from the Gourdian shortcut")
	}

	fmt.Println("Voice and overlay")
	if _, ok := speech.Available(); ok {
		c.ok("Windows speech available")
	} else if set.Voice == config.VoiceSystem {
		c.fail("voice is set to Windows speech but powershell.exe isn't reachable; switch voice to browser")
	} else {
		c.warn("Windows speech unavailable; the dashboard's browser voice still works")
	}
	if runtime.GOOS == "windows" {
		c.ok("overlay runs in-process on Windows")
	} else if self, err := os.Executable(); err == nil {
		if _, err := os.Stat(filepath.Join(filepath.Dir(self), "gourdian.exe")); err == nil {
			c.ok("overlay binary gourdian.exe found next to this one")
		} else {
			c.warn("gourdian.exe not next to this binary, so -overlay won't start. Run `make` and use bin/gourdian")
		}
	}

	fmt.Println("AI coach")
	if body, ok := trainerFetch("GET", base+"/api/ai/status"); ok {
		// The running app is the one that calls the AI, so its view is what matters.
		var st struct {
			Problem string `json:"problem"`
			Message string `json:"message"`
			Enabled bool   `json:"enabled"`
		}
		switch err := json.Unmarshal(body, &st); {
		case err != nil:
			c.warn("couldn't read the trainer's AI status: %v", err)
		case !st.Enabled:
			c.warn("AI coach is turned off")
		case st.Problem != "":
			c.fail("%s", st.Message)
		default:
			c.ok("the trainer's AI coach is ready (%s for live tips, %s for reviews)", set.AI.Live.Provider, set.AI.Reviews.Provider)
		}
		return c.finishOpenDota(set)
	}
	if !set.AI.Enabled && !set.AI.Review {
		c.warn("AI coach is turned off")
		return c.finishOpenDota(set)
	}
	_, dir, _ := config.OpenDefault()
	keys := secrets.Open(dir)
	env := ai.Env{WorkDir: dir, CLIPath: func(id string) string { return set.AI.CLIPaths[id] },
		Key: func(id string) string { k, _ := keys.Get(id); return k }, CustomURL: func() string { return set.AI.CustomURL }}
	for _, p := range ai.NewProviders(env) {
		info := p.Info()
		if info.ID != set.AI.Live.Provider && info.ID != set.AI.Reviews.Provider {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		st := p.Status(ctx)
		cancel()
		if st.State == ai.StateReady {
			c.ok("%s: %s", info.Name, st.Detail)
		} else {
			c.fail("%s: %s. Fix it on the dashboard's AI coach page", info.Name, st.Detail)
		}
	}
	return c.finishOpenDota(set)
}

// finishOpenDota runs the OpenDota checks and prints the summary.
func (c *checker) finishOpenDota(set config.Settings) error {
	fmt.Println("OpenDota")
	if set.AccountID == "" {
		c.warn("Steam account id not known yet. It's saved during your first match with the trainer running")
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		data := dotadata.New(os.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
		switch public, err := data.HasPublicMatches(ctx, set.AccountID); {
		case err != nil:
			c.warn("couldn't reach OpenDota: %v", err)
		case !public:
			c.warn("OpenDota lists no matches for account %s. Turn on Expose Public Match Data in Dota's settings (Options > Social), or reviews and imports miss details", set.AccountID)
		default:
			c.ok("account %s has public match data", set.AccountID)
		}
	}

	fmt.Println()
	if c.problems > 0 {
		return fmt.Errorf("%d problem(s) found", c.problems)
	}
	fmt.Println("Everything needed is in place.")
	return nil
}
