package cli

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"gourdian/internal/sys/config"
	"gourdian/internal/ui/overlay"
)

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
		store, _, err := config.OpenDefault()
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
