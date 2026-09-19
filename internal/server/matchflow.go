package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"gourdian/internal/config"
	"gourdian/internal/matchdata"
	"gourdian/internal/stats"
)

const (
	// parseWait is how long the trainer keeps waiting for OpenDota to parse a replay. Parsing
	// is usually done in a few minutes, but the queue can be much slower.
	parseWait     = 3 * time.Hour
	resumeWithin  = 24 * time.Hour
	importTimeout = 30 * time.Minute
)

func isOpenDotaMatch(m stats.MatchSummary) bool {
	_, err := strconv.ParseInt(m.MatchID, 10, 64)
	return err == nil && m.Real() && m.MatchID != "0"
}

// afterMatch waits for OpenDota to parse a real match, stores the parsed data and then
// writes the review with it. Waiting happens in the background; the trainer keeps coaching.
// reviewStatus tells the dashboard what the review is doing; Waiting means the replay parse
// hasn't arrived yet, so the dashboard offers to review with live data instead.
type reviewStatus struct {
	Text    string `json:"text"`
	MatchID string `json:"match_id,omitempty"`
	Waiting bool   `json:"waiting,omitempty"`
}

// checkRanked asks OpenDota what kind of match it was, so the MMR prompt only stays for
// ranked games. The summary is there within a couple of minutes, long before the replay parse.
func (s *Server) checkRanked(matchID string) {
	ctx, cancel := context.WithTimeout(s.baseCtx, 10*time.Minute)
	defer cancel()
	for wait := 30 * time.Second; ; wait *= 2 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		match, err := s.data.Match(ctx, matchID)
		if err != nil {
			continue
		}
		s.saveRanked(matchID, match.LobbyType)
		return
	}
}

// saveRanked records OpenDota's lobby type. It can only mark a match ranked: one the player
// already marked stays ranked.
func (s *Server) saveRanked(matchID string, lobbyType int) {
	ranked := lobbyType == rankedLobby
	if err := s.stats.UpdateMatch(matchID, func(row *stats.MatchSummary) {
		row.Ranked = row.Ranked || ranked
		ranked = row.Ranked
	}); err != nil {
		s.log.Warn("save the match's lobby type", "err", err)
	}
	s.confirmRanked(matchID, ranked)
}

func (s *Server) afterMatch(m stats.MatchSummary, set config.Settings) {
	if !isOpenDotaMatch(m) {
		s.reviewMatch(m, set, false, nil)
		return
	}
	reviewing := set.AI.Review
	if _, _, ok := s.pick(set.AI.Reviews, set.AI); !ok {
		reviewing = false
	}
	status := func(waited time.Duration) {
		if !reviewing {
			return
		}
		text := "Waiting for OpenDota to parse the replay, then the review is written. This is usually a few minutes, but can take longer."
		if waited >= time.Minute {
			text = fmt.Sprintf("Waiting for OpenDota to parse the replay: %d minutes so far. The review follows as soon as it lands.", int(waited.Minutes()))
		}
		s.hub.publish("review_status", reviewStatus{Text: text, MatchID: m.MatchID, Waiting: true})
	}
	status(0)
	go s.checkRanked(m.MatchID)
	go func() {
		ctx, cancel := context.WithTimeout(s.baseCtx, parseWait)
		defer cancel()
		detail, err := s.matches.Enrich(ctx, m, s.cfg.Settings().AccountID, status)
		switch {
		case s.baseCtx.Err() != nil:
			return
		case err != nil:
			s.log.Warn("OpenDota match data unavailable", "match", m.MatchID, "err", err)
		case detail.Parsed:
			s.targets.reset()
			s.log.Info("parsed match data saved", "match", m.MatchID)
			s.hub.publish("match", m)
		default:
			s.log.Warn("OpenDota didn't parse the replay in time; reviewing with live data", "match", m.MatchID)
		}
		s.reviewMatch(m, s.cfg.Settings(), false, detail)
	}()
}

// resumePending picks up matches from the last day whose parse or review was cut short by
// quitting the trainer or by the AI coach being paused.
func (s *Server) resumePending() {
	matches, err := s.stats.Matches()
	if err != nil {
		return
	}
	reviewed := map[string]bool{}
	if reviews, err := s.stats.Reviews(); err == nil {
		for _, r := range reviews {
			reviewed[r.MatchID] = true
		}
	}
	for _, m := range matches {
		if m.Source == stats.SourceLive && !reviewed[m.MatchID] && time.Since(m.EndedAt) < resumeWithin && isOpenDotaMatch(m) {
			s.log.Info("resuming match data and review", "match", m.MatchID)
			s.afterMatch(m, s.cfg.Settings())
		}
	}
}

// rememberAccount stores the Steam account id the first time Dota reports it.
func (s *Server) rememberAccount(accountID string) {
	if accountID == "" || accountID == "0" {
		return
	}
	cur := s.cfg.Settings()
	if cur.AccountID != "" {
		return
	}
	cur.AccountID = accountID
	if err := s.cfg.UpdateSettings(cur); err != nil {
		s.log.Error("save account id", "err", err)
		return
	}
	s.log.Info("learned Steam account id", "account", accountID)
}

type importStatus struct {
	Running bool   `json:"running"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Added   int    `json:"added"`
	Error   string `json:"error,omitempty"`
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Count int `json:"count"`
	}
	json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body)
	count := min(max(body.Count, 1), 100)
	if body.Count == 0 {
		count = 50
	}
	account := s.cfg.Settings().AccountID
	if account == "" {
		http.Error(w, "No Steam account id yet. Play a match with the trainer running, or enter it in Settings.", http.StatusBadRequest)
		return
	}
	if !s.importing.CompareAndSwap(false, true) {
		http.Error(w, "an import is already running", http.StatusConflict)
		return
	}
	go func() {
		defer s.importing.Store(false)
		ctx, cancel := context.WithTimeout(s.baseCtx, importTimeout)
		defer cancel()
		s.hub.publish("import_status", importStatus{Running: true})
		added, err := s.matches.Import(ctx, account, count, func(p matchdata.ImportProgress) {
			s.hub.publish("import_status", importStatus{Running: true, Done: p.Done, Total: p.Total, Added: p.Added})
		})
		s.targets.reset()
		final := importStatus{Added: added}
		if err != nil {
			final.Error = err.Error()
		}
		s.hub.publish("import_status", final)
		s.hub.publish("match", nil)
		s.log.Info("import finished", "added", added, "err", err)
	}()
	writeJSON(w, importStatus{Running: true})
}
