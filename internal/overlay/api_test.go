package overlay

import (
	"testing"

	"dotatrainer/internal/config"
)

func TestWheelLayoutStepsWithinLimits(t *testing.T) {
	o := config.Default().Settings.Overlay
	if got := wheelLayout(o, true, false); got.HUDScale != 110 || got.HUDBackground != o.HUDBackground {
		t.Fatalf("wheel up = %+v", got)
	}
	if got := wheelLayout(o, false, true); got.HUDBackground != o.HUDBackground-10 || got.HUDScale != o.HUDScale {
		t.Fatalf("ctrl+wheel down = %+v", got)
	}
	o.HUDScale, o.HUDBackground = config.MaxHUDScale, 0
	if got := wheelLayout(wheelLayout(o, true, false), false, true); got.HUDScale != config.MaxHUDScale || got.HUDBackground != 0 {
		t.Fatalf("limits not kept: %+v", got)
	}
}
