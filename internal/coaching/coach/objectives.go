package coach

// roshanState is the last Roshan kill the player saw, and which team made it.
type roshanState struct {
	known  bool
	deadAt int
	team   string
}

// aegisState is the Aegis since it was last picked up.
type aegisState struct {
	known   bool
	expires int
	team    string
	mine    bool
	holder  int
	// used is the Aegis having brought its holder back. The player's own shows at once; anyone
	// else's only once they die for good, as coming back prints no kill.
	used bool
}

// glyphState is your team's Glyph, cooling down since at while used.
type glyphState struct {
	used bool
	at   int
}
