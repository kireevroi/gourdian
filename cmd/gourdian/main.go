// Command gourdian is a live coach for Dota 2: it reads Valve's Game State Integration feed,
// speaks tips while you play, draws a HUD over the game and records every match.
package main

import (
	"os"

	"gourdian/internal/cli"
)

func main() { os.Exit(cli.Main(os.Args[1:])) }
