package overlay

import (
	"context"
	"fmt"
	"image/color"
	"log/slog"
	"time"

	"gourdian/internal/config"
	"gourdian/internal/dota"
	"gourdian/internal/hud"
)

// The HUD's colours; the Windows and the pure-Go renderer both draw with these.
var (
	hudPanel  = color.RGBA{14, 17, 22, 255}
	hudRow    = color.RGBA{28, 34, 48, 255}
	hudLine   = color.RGBA{38, 45, 58, 255}
	hudAccent = color.RGBA{224, 83, 61, 255}
	hudKinds  = map[string]color.RGBA{
		hud.KindText:   {230, 233, 239, 255},
		hud.KindMuted:  {139, 149, 167, 255},
		hud.KindGood:   {63, 185, 122, 255},
		hud.KindInfo:   {91, 156, 240, 255},
		hud.KindWarn:   {229, 169, 59, 255},
		hud.KindUrgent: {239, 74, 74, 255},
		hud.KindCoach:  {144, 133, 233, 255},
	}
)

func kindColor(kind string) color.RGBA {
	if c, ok := hudKinds[kind]; ok {
		return c
	}
	return hudKinds[hud.KindText]
}

// The HUD's layout in unscaled pixels; both renderers multiply these by their scale.
const (
	hudPad      = 12  // inside a block, left of the text
	hudGap      = 4   // between the lines of a block
	hudBar      = 5   // the coloured bar on a block's left edge
	hudBarInset = 6   // how far the bar stops short of the block's top and bottom
	hudSpacing  = 8   // between blocks
	hudRadius   = 10  // the blocks' rounded corners
	editBottom  = 52  // room under the blocks, while editing, for the hint and Done
	editMin     = 200 // the least height of the HUD while editing
)

// editHint is the hint shown while the layout is edited, shortened until fits says it fits.
func editHint(o config.OverlaySettings, fits func(string) bool) string {
	for _, s := range []string{
		fmt.Sprintf("Drag to move · wheel: size %d%% · Ctrl+wheel: background %d%%", o.HUDScale, o.HUDBackground),
		fmt.Sprintf("Drag · wheel: %d%% · Ctrl+wheel: %d%%", o.HUDScale, o.HUDBackground),
		fmt.Sprintf("%d%% · %d%%", o.HUDScale, o.HUDBackground),
	} {
		if fits(s) {
			return s
		}
	}
	return ""
}

// sendLayout saves a layout the player dragged or scrolled into place, in the background.
func sendLayout(a api, log *slog.Logger, o config.OverlaySettings) {
	p := patch{"overlay": patch{"hud_placed": o.HUDPlaced, "hud_x": o.HUDX, "hud_y": o.HUDY, "hud_scale": o.HUDScale, "hud_background": o.HUDBackground}}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := a.do(ctx, "PUT", "/api/settings", p, nil); err != nil {
			log.Warn("couldn't save the HUD layout", "err", err)
		}
	}()
}

// sendPosition tells the trainer the position picked with Ctrl+Shift+1 to 5 (i is 0 to 4).
func sendPosition(a api, log *slog.Logger, i int) {
	role := dota.Roles[i]
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := a.do(ctx, "POST", "/api/role", map[string]string{"role": role}, nil); err != nil {
			log.Warn("couldn't set the position", "err", err)
		}
	}()
}
