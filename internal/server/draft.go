package server

import (
	"os"
	"path/filepath"
	"time"

	"gourdian/internal/gsi"
)

// DraftSeenFile is written in the data folder the first time Dota sends a draft board with
// heroes in it. Valve gives the draft to spectators and observers only, so a player's feed
// carries an empty one and this file never appears; the trainer watches for it anyway, so the
// day that changes we learn it from a real install instead of guessing.
const DraftSeenFile = "draft-seen"

// noteDraft records a draft board the first time one arrives with heroes in it.
func (s *Server) noteDraft(st *gsi.State) {
	ours, theirs, bans, ok := st.DraftBoard()
	if !ok || !s.draftSeen.CompareAndSwap(false, true) {
		return
	}
	s.log.Info("Dota sent a draft board", "ours", len(ours), "theirs", len(theirs), "bans", len(bans))
	path := filepath.Join(s.workDir, DraftSeenFile)
	if err := os.WriteFile(path, []byte(time.Now().Format(time.RFC3339)+"\n"), 0o644); err != nil {
		s.log.Warn("couldn't record that a draft board arrived", "file", path, "err", err)
	}
}
