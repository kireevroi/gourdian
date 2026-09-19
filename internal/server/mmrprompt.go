package server

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"gourdian/internal/stats"
)

// rankedLobby is OpenDota's lobby type for ranked matchmaking.
const rankedLobby = 7

// mmrPrompt asks for the new MMR after a match. It stays until the player answers or
// dismisses it, so closing the dashboard mid-match doesn't lose it.
type mmrPrompt struct {
	MatchID string    `json:"match_id"`
	Hero    string    `json:"hero"`
	Result  string    `json:"result"`
	Last    int       `json:"last"`   // the MMR logged before this match
	Ranked  bool      `json:"ranked"` // false until OpenDota confirms it
	At      time.Time `json:"at"`
}

type mmrState struct {
	mu     sync.Mutex
	prompt *mmrPrompt
}

func (s *Server) pendingMMR() *mmrPrompt {
	s.mmr.mu.Lock()
	defer s.mmr.mu.Unlock()
	return s.mmr.prompt
}

// askForMMR offers to log the MMR after a real match.
func (s *Server) askForMMR(m *stats.MatchSummary) {
	if !m.Real() || !s.cfg.Settings().MMRPrompt {
		return
	}
	p := &mmrPrompt{MatchID: m.MatchID, Hero: m.Hero, Result: m.Result, Ranked: m.Ranked, At: time.Now()}
	if entries, err := s.stats.MMR(); err == nil {
		p.Last, _ = mmrBefore(entries, m.MatchID, time.Time{})
	}
	s.mmr.mu.Lock()
	s.mmr.prompt = p
	s.mmr.mu.Unlock()
	s.hub.publish("mmr_prompt", p)
}

// mmrBefore is the last MMR logged before a match, not counting the match's own entry, so
// logging a match again doesn't add its win twice.
func mmrBefore(entries []stats.MMREntry, matchID string, ended time.Time) (int, bool) {
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if matchID != "" && e.MatchID == matchID || !ended.IsZero() && e.Date.After(ended) {
			continue
		}
		return e.MMR, true
	}
	return 0, false
}

// confirmRanked is called once it's known what kind of match it was; an unranked one takes
// the prompt away again.
func (s *Server) confirmRanked(matchID string, ranked bool) {
	s.mmr.mu.Lock()
	p := s.mmr.prompt
	if p == nil || p.MatchID != matchID {
		s.mmr.mu.Unlock()
		return
	}
	if ranked {
		p.Ranked = true
		s.mmr.mu.Unlock()
		s.hub.publish("mmr_prompt", p)
		return
	}
	s.mmr.prompt = nil
	s.mmr.mu.Unlock()
	s.hub.publish("mmr_prompt", nil)
}

func (s *Server) clearMMRPrompt() {
	s.mmr.mu.Lock()
	s.mmr.prompt = nil
	s.mmr.mu.Unlock()
	s.hub.publish("mmr_prompt", nil)
}

// handleMatchRanked marks a match as ranked, or not, when the player says so.
func (s *Server) handleMatchRanked(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Ranked bool `json:"ranked"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		http.Error(w, `send {"ranked": true}`, http.StatusBadRequest)
		return
	}
	matchID := r.PathValue("id")
	if err := s.stats.UpdateMatch(matchID, func(m *stats.MatchSummary) { m.Ranked = body.Ranked }); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	m, err := s.stats.Match(matchID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	switch p := s.pendingMMR(); {
	case p != nil && p.MatchID == matchID:
		s.confirmRanked(matchID, body.Ranked)
	case body.Ranked && p == nil && s.mmrFor(matchID) == 0:
		s.askForMMR(&m)
	}
	writeJSON(w, m)
}

// mmrFor is the MMR logged for a match, or 0.
func (s *Server) mmrFor(matchID string) int {
	entries, err := s.stats.MMR()
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if e.MatchID == matchID {
			return e.MMR
		}
	}
	return 0
}

// handleMatchMMR logs the MMR after one match, from the match lists.
func (s *Server) handleMatchMMR(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MMR    int `json:"mmr"`
		Change int `json:"change"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		http.Error(w, `send {"mmr": 3025} or {"change": 25}`, http.StatusBadRequest)
		return
	}
	matchID := r.PathValue("id")
	m, err := s.stats.Match(matchID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	entry := stats.MMREntry{Date: m.EndedAt, MMR: body.MMR, Note: m.Result, MatchID: matchID}
	if !m.Ranked {
		// Logging MMR for a match says it was ranked, whatever OpenDota thought.
		if err := s.stats.UpdateMatch(matchID, func(row *stats.MatchSummary) { row.Ranked = true }); err != nil {
			s.log.Warn("mark the match ranked", "err", err)
		}
	}
	if entry.Date.IsZero() {
		entry.Date = time.Now()
	}
	if body.Change != 0 {
		entries, err := s.stats.MMR()
		before, ok := mmrBefore(entries, matchID, m.EndedAt)
		if err != nil || !ok {
			http.Error(w, "there's no MMR logged before this match, so type the number instead", http.StatusBadRequest)
			return
		}
		entry.MMR = before + body.Change
	}
	if entry.MMR <= 0 || entry.MMR > 20000 {
		http.Error(w, "that MMR doesn't look right", http.StatusBadRequest)
		return
	}
	if err := s.stats.AppendMMR(entry); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if p := s.pendingMMR(); p != nil && p.MatchID == matchID {
		s.clearMMRPrompt()
	}
	writeJSON(w, entry)
}

func (s *Server) handleMMRPrompt(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		s.clearMMRPrompt()
	}
	writeJSON(w, s.pendingMMR())
}

// handleMMRChange logs a win or loss as a step from the last entry, for the prompt's buttons.
func (s *Server) handleMMRChange(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Change int    `json:"change"`
		Note   string `json:"note"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil || body.Change == 0 {
		http.Error(w, `send {"change": 25}`, http.StatusBadRequest)
		return
	}
	p := s.pendingMMR()
	if p == nil {
		http.Error(w, "this match's MMR is already logged", http.StatusConflict)
		return
	}
	entries, err := s.stats.MMR()
	before, ok := mmrBefore(entries, p.MatchID, time.Time{})
	if err != nil || !ok {
		http.Error(w, "log your MMR once first, then the buttons can add and subtract", http.StatusBadRequest)
		return
	}
	entry := stats.MMREntry{Date: time.Now(), MMR: before + body.Change, Note: body.Note, MatchID: p.MatchID}
	if entry.MMR <= 0 {
		http.Error(w, "that would take your MMR below zero", http.StatusBadRequest)
		return
	}
	if err := s.stats.AppendMMR(entry); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.clearMMRPrompt()
	writeJSON(w, entry)
}
