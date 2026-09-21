package server

import (
	"net/http"
	"time"

	"gourdian/internal/mmr"
	"gourdian/internal/model"
)

// rankedLobby is OpenDota's lobby type for ranked matchmaking.
const rankedLobby = 7

// askForMMR offers to log the MMR after a real match.
func (s *Server) askForMMR(m *model.MatchSummary) {
	if !m.Real() || !s.cfg.Settings().MMRPrompt {
		return
	}
	p := &mmr.Prompt{MatchID: m.MatchID, Hero: m.Hero, Result: m.Result, Ranked: m.Ranked, At: time.Now()}
	if entries, err := s.stats.MMR(); err == nil {
		p.Last, _ = mmr.Before(entries, m.MatchID, time.Time{})
	}
	s.mmr.Ask(p)
	s.hub.publish("mmr_prompt", p)
}

// confirmRanked is called once it's known what kind of match it was; an unranked one takes
// the prompt away again.
func (s *Server) confirmRanked(matchID string, ranked bool) {
	if next, changed := s.mmr.ConfirmRanked(matchID, ranked); changed {
		s.hub.publish("mmr_prompt", next)
	}
}

func (s *Server) clearMMRPrompt() {
	s.mmr.Clear()
	s.hub.publish("mmr_prompt", nil)
}

// handleMatchRanked marks a match as ranked, or not, when the player says so.
func (s *Server) handleMatchRanked(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Ranked bool `json:"ranked"`
	}
	if err := readJSON(w, r, 4<<10, &body); err != nil {
		http.Error(w, `send {"ranked": true}`, http.StatusBadRequest)
		return
	}
	matchID := r.PathValue("id")
	if err := s.stats.UpdateMatch(matchID, func(m *model.MatchSummary) { m.Ranked = body.Ranked }); err != nil {
		s.matchProblem(w, "couldn't mark that match", err)
		return
	}
	m, err := s.stats.Match(matchID)
	if err != nil {
		s.matchProblem(w, "couldn't read that match", err)
		return
	}
	switch p := s.mmr.Pending(); {
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
	return mmr.For(entries, matchID)
}

// handleMatchMMR logs the MMR after one match, from the match lists.
func (s *Server) handleMatchMMR(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MMR    int `json:"mmr"`
		Change int `json:"change"`
	}
	if err := readJSON(w, r, 4<<10, &body); err != nil {
		http.Error(w, `send {"mmr": 3025} or {"change": 25}`, http.StatusBadRequest)
		return
	}
	matchID := r.PathValue("id")
	m, err := s.stats.Match(matchID)
	if err != nil {
		s.matchProblem(w, "couldn't read that match", err)
		return
	}
	entry := model.MMREntry{Date: m.EndedAt, MMR: body.MMR, Note: m.Result, MatchID: matchID}
	if !m.Ranked {
		// Logging MMR for a match says it was ranked, whatever OpenDota thought.
		if err := s.stats.UpdateMatch(matchID, func(row *model.MatchSummary) { row.Ranked = true }); err != nil {
			s.log.Warn("mark the match ranked", "err", err)
		}
	}
	if entry.Date.IsZero() {
		entry.Date = time.Now()
	}
	if body.Change != 0 {
		entries, err := s.stats.MMR()
		before, ok := mmr.Before(entries, matchID, m.EndedAt)
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
		s.failed(w, http.StatusInternalServerError, "couldn't save your MMR", err)
		return
	}
	if p := s.mmr.Pending(); p != nil && p.MatchID == matchID {
		s.clearMMRPrompt()
	}
	writeJSON(w, entry)
}

func (s *Server) handleMMRPrompt(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		s.clearMMRPrompt()
	}
	writeJSON(w, s.mmr.Pending())
}

// handleMMRChange logs a win or loss as a step from the last entry, for the prompt's buttons.
func (s *Server) handleMMRChange(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Change int    `json:"change"`
		Note   string `json:"note"`
	}
	if err := readJSON(w, r, 4<<10, &body); err != nil || body.Change == 0 {
		http.Error(w, `send {"change": 25}`, http.StatusBadRequest)
		return
	}
	p := s.mmr.Pending()
	if p == nil {
		http.Error(w, "this match's MMR is already logged", http.StatusConflict)
		return
	}
	entries, err := s.stats.MMR()
	before, ok := mmr.Before(entries, p.MatchID, time.Time{})
	if err != nil || !ok {
		http.Error(w, "log your MMR once first, then the buttons can add and subtract", http.StatusBadRequest)
		return
	}
	entry := model.MMREntry{Date: time.Now(), MMR: before + body.Change, Note: body.Note, MatchID: p.MatchID}
	if entry.MMR <= 0 {
		http.Error(w, "that would take your MMR below zero", http.StatusBadRequest)
		return
	}
	if err := s.stats.AppendMMR(entry); err != nil {
		s.failed(w, http.StatusInternalServerError, "couldn't save your MMR", err)
		return
	}
	s.clearMMRPrompt()
	writeJSON(w, entry)
}

// handleMMRList returns every MMR entry, so the match lists can show what was logged.
func (s *Server) handleMMRList(w http.ResponseWriter, r *http.Request) {
	entries, err := s.stats.MMR()
	if err != nil {
		s.failed(w, http.StatusInternalServerError, "couldn't read your MMR log", err)
		return
	}
	if entries == nil {
		entries = []model.MMREntry{}
	}
	writeJSON(w, entries)
}

func (s *Server) handleMMR(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MMR  int    `json:"mmr"`
		Note string `json:"note"`
	}
	if err := readJSON(w, r, 4<<10, &body); err != nil || body.MMR <= 0 || body.MMR > 20000 {
		http.Error(w, "send {\"mmr\": 1234}", http.StatusBadRequest)
		return
	}
	entry := model.MMREntry{Date: time.Now(), MMR: body.MMR, Note: body.Note}
	if err := s.stats.AppendMMR(entry); err != nil {
		s.failed(w, http.StatusInternalServerError, "couldn't save your MMR", err)
		return
	}
	writeJSON(w, entry)
}
