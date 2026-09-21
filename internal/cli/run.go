package cli

import (
	"flag"

	"gourdian/internal/app"
	"gourdian/internal/sys/platform"
)

// runCmd parses the flags of the `run` command and hands the rest to the app.
func runCmd(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	listen := fs.String("listen", "", "address to listen on (default from config.json)")
	open := fs.Bool("open", false, "open the dashboard in your browser")
	openFirst := fs.Bool("open-first", false, "open the dashboard only on the very first start")
	withOverlay := fs.Bool("overlay", platform.LinuxDesktop(), "also show the in-game overlay (on by default on Linux)")
	withTray := fs.Bool("tray", false, "show a tray icon (Windows)")
	background := fs.Bool("background", false, "started at sign-in: stay quiet until Dota sends data")
	record := fs.Bool("record", false, "save every game-state update to a replayable file")
	fs.Parse(args)

	return app.Run(app.Options{
		Listen:     *listen,
		Open:       *open,
		OpenFirst:  *openFirst,
		Overlay:    *withOverlay,
		Tray:       *withTray,
		Background: *background,
		Record:     *record,
	})
}
