package server

import (
	"maps"
	"sync"
	"time"

	"gourdian/internal/coaching/coach"
	"gourdian/internal/sys/config"
	"gourdian/internal/ui/hud"
)

// overlayReport is what the overlay last said about itself, for the dashboard.
type overlayReport struct {
	mu          sync.Mutex
	hotkeys     map[string]string
	hudErr      string
	hudReported bool
}

// set takes a report; hudErr is sent only by the Linux HUD, empty once its window is up.
func (o *overlayReport) set(hotkeys map[string]string, hudErr *string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if hotkeys != nil || hudErr == nil {
		o.hotkeys = hotkeys
	}
	if hudErr != nil {
		o.hudErr, o.hudReported = *hudErr, true
	}
}

func (o *overlayReport) hudState() (reported bool, err string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.hudReported, o.hudErr
}

func (o *overlayReport) hotkeyProblems() map[string]string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return maps.Clone(o.hotkeys)
}

// alertQueue lets alerts take turns on the HUD across its redraws.
type alertQueue struct {
	mu sync.Mutex
	q  hud.Queue
}

func (a *alertQueue) build(snap coach.Snapshot, tips []coach.Tip, set config.Settings) hud.View {
	a.mu.Lock()
	defer a.mu.Unlock()
	return hud.BuildHeld(snap, tips, set.HUDWidgets, time.Now(), &a.q, set.Language)
}
