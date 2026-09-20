package overlay

import (
	"context"
	"fmt"
	"image"
	"log/slog"
	"time"

	"gourdian/internal/screen"
)

// readEvery is how often the portraits are read while the draft is open. Twice a draft is
// enough to settle a hero, so a couple of seconds apart costs nothing and leaves the game
// alone the rest of the time.
const readEvery = 2 * time.Second

// watchDraft reads the hero portraits off the screen while the player is choosing, and tells
// the trainer what it saw.
//
// This lives in the overlay because the overlay is the part of the trainer running where the
// screen is: under WSL the rest of it runs on the Linux side, which cannot see the Windows
// desktop at all. Nothing is read outside a draft, and nothing at all unless the player has
// turned it on.
func watchDraft(ctx context.Context, m *model, a *api, heroes map[string]int, log *slog.Logger) {
	if screen.Wayland() {
		log.Info("not reading the draft: a program can't read a Wayland desktop")
		return
	}
	watchWith(ctx, m, a, screen.TableFor(heroes), eyes{screen.Size, screen.Grab}, readEvery, log)
}

// eyes is where the pictures come from, so the loop can be run against a made-up screen.
type eyes struct {
	size func() (image.Rectangle, error)
	grab func(image.Rectangle) (image.Image, error)
}

func watchWith(ctx context.Context, m *model, a *api, table screen.Table, look eyes, every time.Duration, log *slog.Logger) {
	if len(table) == 0 {
		log.Warn("not reading the draft: no hero portraits to compare against")
		return
	}
	var seen screen.Reading
	var bar screen.Bar
	var size string
	drafting, searched := false, false
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		if !m.draft() {
			// The draft is over, or hasn't started. Start the next one from nothing.
			if drafting {
				seen.Forget()
				searched, drafting = false, false
			}
			continue
		}
		drafting = true
		where, err := look.size()
		if err != nil {
			log.Warn("couldn't measure the screen", "err", err)
			continue
		}
		if next := fmt.Sprintf("%dx%d", where.Dx(), where.Dy()); next != size {
			size, bar, searched = next, screen.Predict(where), false
		}
		shot, err := look.grab(where)
		if err != nil {
			log.Warn("couldn't read the screen", "err", err)
			continue
		}
		read, _ := seen.Add(shot, bar, table)
		// Dota's interface can be scaled by hand, which moves the portraits away from where
		// the screen's height says they are. Look for them properly, once per draft.
		if read == 0 && !searched {
			searched = true
			if found, n, ok := screen.Locate(shot, table); ok {
				log.Info("found the hero portraits", "screen", size, "heroes", n, "at", found.Left)
				bar = found
			}
		}
		// Tell the trainer every time round, not only when a hero has just settled. It drops
		// a reading that stops being renewed, so a quiet stretch of the draft -- which is
		// most of it, once the first few are in -- would take the other side off the board
		// and undo the advice that rests on them.
		if seen.Settled() == 0 {
			continue
		}
		dire := m.dire()
		body := map[string]any{"ours": seen.Ours(dire), "theirs": seen.Theirs(dire), "screen": size, "bar": bar}
		if err := a.do(ctx, "POST", "/api/draft", body, nil); err != nil {
			log.Warn("couldn't tell the trainer what the draft looks like", "err", err)
		}
	}
}

// startDraftReader waits for the trainer to have the hero numbers and then watches the draft.
// It is started whatever the setting says, and asks the model each tick, so turning the
// reading on from the dashboard takes effect without restarting the overlay.
func startDraftReader(ctx context.Context, m *model, a *api, log *slog.Logger) {
	var heroes map[string]int
	for len(heroes) == 0 {
		if err := a.do(ctx, "GET", "/api/heroes", nil, &heroes); err != nil && ctx.Err() == nil {
			log.Debug("waiting for the hero list before reading the draft", "err", err)
		}
		if len(heroes) > 0 {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
	watchDraft(ctx, m, a, heroes, log)
}
