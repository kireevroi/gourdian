// Package cli is the gourdian command line. It works out which command was asked for, parses that
// command's flags and calls the work, so cmd/gourdian stays one line and every command can be
// reached from a test.
package cli

import (
	"fmt"
	"os"
	"runtime"
	"strings"
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
  gourdian draft [-for 30s]              photograph the screen during a draft and say which heroes it can make out
`

// Main runs the command named by argv and reports the process exit code: 0 when it worked,
// 1 when it failed and 2 when the command isn't one of ours.
func Main(argv []string) int {
	cmd, args := "run", argv
	// Double-clicking the exe on Windows, or Windows starting it at sign-in, means the app
	// rather than a command: tray, HUD and dashboard, with errors shown as dialogs.
	if runtime.GOOS == "windows" && (len(args) == 0 || len(args) == 1 && args[0] == "--background") {
		runApp(len(args) == 1)
		return 0
	}
	attachConsole()
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "run":
		err = runCmd(args)
	case "install":
		err = installCmd(args, false)
	case "uninstall":
		err = installCmd(args, true)
	case "simulate":
		err = simulateCmd(args)
	case "overlay":
		err = overlayCmd(args)
	case "stats":
		err = statsCmd()
	case "draft":
		err = draftCmd(args)
	case "doctor":
		err = doctor()
	case "setup":
		err = setupCmd()
	case "quit":
		stopRunningTrainer()
	case "version":
		err = versionCmd()
	case "replay":
		err = replayCmd(args)
	case "mmr":
		err = mmrCmd(args)
	case "import":
		err = importCmd(args)
	case "help":
		fmt.Print(usage)
	default:
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}
