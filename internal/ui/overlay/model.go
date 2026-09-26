// Package overlay draws a click-through, always-on-top HUD over Dota 2 (borderless window
// mode) from the trainer's event stream. It never touches the game process.
package overlay

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"gourdian/internal/coaching/coach"
	"gourdian/internal/sys/config"
	"gourdian/internal/ui/hud"
)

type Options struct {
	URL      string
	Corner   string
	OffsetX  int
	OffsetY  int
	Scale    float64
	Tray     bool
	Quiet    bool
	AppDir   string
	Log      *slog.Logger
	Snapshot string
	// SnapshotEditing draws the snapshot as the HUD looks while its layout is edited.
	SnapshotEditing bool
}

func (o Options) logger() *slog.Logger {
	if o.Log != nil {
		return o.Log
	}
	return slog.Default()
}

// View is what the HUD draws: the startup banner, then the trainer's HUD view.
type View struct {
	Banner string
	hud.View
}

func (v View) Empty() bool { return v.Banner == "" && v.View.Empty() }

const bannerFor = 8 * time.Second

type model struct {
	mu        sync.Mutex
	hotkeys   config.HotkeySettings
	started   time.Time
	quiet     bool
	announced bool
	online    bool
	inMatch   bool
	clock     int
	team      string // "radiant" or "dire", for telling the two runs of portraits apart
	hud       hud.Payload
}

// newModel shows the hotkey banner at start, or when quiet (started at sign-in) only once a match begins.
func newModel(now time.Time, quiet bool) *model {
	m := &model{quiet: quiet, hotkeys: config.Default().Settings.Hotkeys}
	if !quiet {
		m.started = now
	}
	return m
}

func (m *model) apply(event string, data []byte, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch event {
	case "snapshot":
		var s coach.Snapshot
		if json.Unmarshal(data, &s) != nil {
			return false
		}
		m.online, m.inMatch, m.clock, m.team = true, s.InMatch, s.Clock, s.Team
		if m.quiet && !m.announced && s.InMatch {
			m.started, m.announced = now, true
		}
	case "hud":
		var p hud.Payload
		if json.Unmarshal(data, &p) != nil {
			return false
		}
		m.hud = p
	default:
		return false
	}
	return true
}

// draft reports whether the trainer says the player is choosing a hero, which is the only
// time the screen is read.
func (m *model) draft() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.online && m.hud.Draft
}

// keepFrames reports whether the player asked for the draft frames to be saved.
func (m *model) keepFrames() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hud.KeepFrames
}

// dire reports which side the player is on, so a run of portraits can be called theirs.
func (m *model) dire() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.team == "dire"
}

func (m *model) setHotkeys(hk config.HotkeySettings) {
	m.mu.Lock()
	m.hotkeys = hk
	m.mu.Unlock()
}

func (m *model) setOnline(online bool) {
	m.mu.Lock()
	m.online = online
	m.mu.Unlock()
}

// view returns what to draw. While the layout is edited outside a match, a sample with every
// enabled widget shows instead of nothing.
func (m *model) view(now time.Time, editing bool) View {
	m.mu.Lock()
	defer m.mu.Unlock()
	var v View
	if now.Sub(m.started) < bannerFor {
		v.Banner = "Gourdian · " + m.hotkeys.HUDEdit + " move HUD · " + m.hotkeys.Dashboard + " dashboard"
	}
	switch {
	case !m.online && v.Banner != "":
		v.Rows = []hud.Line{{Text: "Trainer not reachable. Start Gourdian from the Start menu", Kind: hud.KindMuted}}
	case !m.online:
	case editing && m.hud.Live.Empty():
		v.View = m.hud.Sample
	default:
		v.View = m.hud.Live
	}
	return v
}

// askingPosition reports whether the position hotkeys should be active: while the player is
// choosing a hero, and then in the match until 2:30.
//
// The draft half matters as much as the match half. Pick advice is worked out for a position,
// and until the player says which they are playing the trainer uses whatever they played
// last, so the one moment they most need to correct it was the one moment the keys did
// nothing.
func (m *model) askingPosition() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.online && (m.hud.Choosing || m.inMatch && m.clock < hud.PositionUntil)
}

// stream follows the trainer's SSE feed, reconnecting until ctx ends. Events the model doesn't
// use, such as settings changes from the dashboard, go to other, if set.
func stream(ctx context.Context, base string, m *model, changed func(), other func(event string, data []byte)) {
	for ctx.Err() == nil {
		streamOnce(ctx, base, m, changed, other)
		if ctx.Err() != nil {
			return
		}
		m.setOnline(false)
		changed()
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
}

func streamOnce(ctx context.Context, base string, m *model, changed func(), other func(event string, data []byte)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/events", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("events: %s", resp.Status)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	var event string
	var data []byte
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if event != "" && m.apply(event, data, time.Now()) {
				changed()
			} else if event != "" && other != nil {
				other(event, data)
			}
			event, data = "", nil
		case strings.HasPrefix(line, "event: "):
			event = line[len("event: "):]
		case strings.HasPrefix(line, "data: "):
			data = append(data, line[len("data: "):]...)
		}
	}
	return sc.Err()
}
