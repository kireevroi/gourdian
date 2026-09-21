package server

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"gourdian/internal/game/gsi"
)

// DraftSeenFile appears the first time Dota sends a draft with heroes in it. Valve gives drafts to
// spectators only, so it is there to tell us from a real install the day that changes.
const DraftSeenFile = "draft-seen"

// feed is what the trainer has learned about Dota's game-state feed so far.
type feed struct {
	authWarns atomic.Int32
	typeWarns atomic.Int32
	extras    atomic.Value
	draftSeen atomic.Bool
	first     sync.Once
	onFirst   func()
}

func firstFew(n *atomic.Int32) bool { return n.Add(1) <= 3 }

func (s *Server) noteDraft(st *gsi.State) {
	ours, theirs, bans, ok := st.DraftBoard()
	if !ok || !s.feed.draftSeen.CompareAndSwap(false, true) {
		return
	}
	s.log.Info("Dota sent a draft board", "ours", len(ours), "theirs", len(theirs), "bans", len(bans))
	path := filepath.Join(s.workDir, DraftSeenFile)
	if err := os.WriteFile(path, []byte(time.Now().Format(time.RFC3339)+"\n"), 0o644); err != nil {
		s.log.Warn("couldn't record that a draft board arrived", "file", path, "err", err)
	}
}
