package server

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"dotatrainer/internal/ai"
	"dotatrainer/internal/aicoach"
	"dotatrainer/internal/coach"
	"dotatrainer/internal/config"
	"dotatrainer/internal/gsi"
	"dotatrainer/internal/matchdata"
	"dotatrainer/internal/stats"
)

const (
	aiTimeout      = 90 * time.Second
	reviewTimeout  = 3 * time.Minute
	aiFirstAsk     = 180
	aiDeathSpacing = 45
)

type aiState struct {
	busy atomic.Bool
	// noticePending is set when a problem hasn't been announced in a match yet.
	noticePending atomic.Bool
	providers     *providers
	setup         aiSetup
	models        modelCache

	mu      sync.Mutex
	matchID string
	asked   bool
	lastAsk int
}

func (s *Server) maybeAskAI(st *gsi.State, matchID string, res coach.Result, set config.Settings) {
	if !set.AI.Enabled || !st.InMatch() || st.Map.Paused {
		return
	}
	if s.ai.noticePending.Load() {
		s.noticeAIProblem()
	}
	clock := st.Map.ClockTime
	died := slices.ContainsFunc(res.Tips, func(t coach.Tip) bool { return t.Rule == "death" })

	ready := s.aiReady()
	a := &s.ai
	a.mu.Lock()
	if a.matchID != matchID {
		a.matchID, a.asked, a.lastAsk = matchID, false, 0
	}
	var reason string
	switch {
	case !ready || a.busy.Load():
	case died && (!a.asked || clock-a.lastAsk >= aiDeathSpacing):
		reason = "the player just died, so they have time to listen and shop"
	case clock >= aiFirstAsk && (!a.asked || clock-a.lastAsk >= set.AI.Interval):
		reason = "regular check-in"
	}
	if reason != "" {
		a.asked, a.lastAsk = true, clock
	}
	a.mu.Unlock()
	if reason != "" {
		s.askAI(reason, matchID, set, true)
	}
}

// askAI drops live answers that arrive after the match ended; answers the player asked for are always shown.
func (s *Server) askAI(reason, matchID string, set config.Settings, live bool) bool {
	provider, choice, ok := s.pick(set.AI.Live, set.AI)
	if !ok || !s.ai.busy.CompareAndSwap(false, true) {
		return false
	}
	prompt := aicoach.Prompt(s.aiInput(reason, matchID, set))
	s.hub.publish("ai_status", "thinking")
	go func() {
		defer s.ai.busy.Store(false)
		defer s.hub.publish("ai_status", "idle")
		ctx, cancel := context.WithTimeout(context.Background(), aiTimeout)
		defer cancel()
		started := time.Now()
		suggestions, err := aicoach.Suggest(ctx, provider, choice, set.AI, set.Language, prompt)
		if err != nil {
			s.log.Warn("AI coach failed", "provider", provider.Info().ID, "err", err)
			s.aiFailed(provider, err)
			return
		}
		snap := s.engine.Snapshot(set)
		if snap.MatchID != matchID || live && !snap.InMatch {
			return
		}
		if len(suggestions) == 0 {
			s.log.Info("AI coach had nothing new to say", "provider", provider.Info().ID)
			return
		}
		tips := make([]coach.Tip, 0, len(suggestions))
		for _, text := range suggestions {
			tips = append(tips, coach.Tip{Rule: "ai", Category: "ai", Severity: coach.Info,
				Text: text, Speech: text, Clock: snap.Clock, At: time.Now()})
		}
		s.engine.AddTips(tips)
		s.deliver(matchID, tips, s.cfg.Settings())
		s.log.Info("AI coach answered", "provider", provider.Info().ID, "took", time.Since(started).Round(100*time.Millisecond))
	}()
	return true
}

func (s *Server) aiContext(set config.Settings) aicoach.Context {
	c := aicoach.Context{Profile: set.AI.Profile}
	if recent, err := s.stats.Recent(50); err == nil {
		sum := summarize(recent, s.engine.Rules())
		c.History = aicoach.History{Matches: sum.Sample, WinRate: sum.WinRate, AvgDeaths: sum.AvgDeaths,
			AvgGPM: sum.AvgGPM, AvgLH10: sum.AvgLH10}
		for _, h := range sum.Habits {
			c.History.Habits = append(c.History.Habits, fmt.Sprintf("%s %.1f", h.Label, h.PerMatch))
		}
	}
	c.MMR, _ = s.stats.MMR()
	if reviews, err := s.stats.Reviews(); err == nil && len(reviews) > 0 {
		c.Focus = reviews[len(reviews)-1].NextGameFocus
	}
	return c
}

// heroFacts is the hero's roles and the professional item build, from OpenDota, for the AI
// coach. owned lists what the player carries now; it may be empty after a match.
func (s *Server) heroFacts(heroID int, role string, owned []string) *aicoach.HeroFacts {
	if heroID == 0 {
		return nil
	}
	info, ok := s.data.Hero(heroID)
	if !ok {
		return nil
	}
	h := &aicoach.HeroFacts{Name: info.LocalizedName, Roles: info.Roles, Build: map[string][]string{}, Owned: owned}
	if b := s.data.BuildFor(heroID, role); b != nil {
		h.BuildPosition, h.BuildGames, h.BuildWon = b.Position, b.Games, b.Won
		for _, it := range b.Items {
			h.Build[it.Phase] = append(h.Build[it.Phase], it.DName)
		}
	}
	return h
}

func (s *Server) matchTimeline(matchID string) []stats.Sample {
	timeline, err := s.stats.Timeline()
	if err != nil {
		return nil
	}
	return slices.DeleteFunc(timeline, func(x stats.Sample) bool { return x.MatchID != matchID })
}

func (s *Server) aiInput(reason, matchID string, set config.Settings) aicoach.Input {
	snap := s.engine.Snapshot(set)
	ctx := s.aiContext(set)
	if snap.Hero != nil {
		var owned []string
		for _, it := range snap.Items {
			switch {
			case it.DName != "":
				owned = append(owned, it.DName)
			case it.Name != "":
				owned = append(owned, it.Name)
			}
		}
		ctx.Hero = s.heroFacts(snap.Hero.ID, set.Role, owned)
	}
	return aicoach.Input{
		Context:  ctx,
		Reason:   reason,
		Role:     set.Role,
		Snapshot: snap,
		Facts:    s.engine.Facts(set.Role),
		Tips:     s.engine.RecentTips(),
		Timeline: s.matchTimeline(matchID),
	}
}

// reviewMatch skips simulated and practice matches unless forced, so they don't spend subscription usage.
func (s *Server) reviewMatch(m stats.MatchSummary, set config.Settings, force bool, detail *matchdata.Detail) bool {
	provider, choice, ok := s.pick(set.AI.Reviews, set.AI)
	if !ok || !force && (!set.AI.Review || !m.Real()) {
		return false
	}
	labels := map[string]string{}
	for _, r := range s.engine.Rules() {
		labels[r.ID] = cmp.Or(r.Habit, r.Label)
	}
	var warnings []string
	for rule, n := range m.TipCounts {
		if label, ok := labels[rule]; ok {
			warnings = append(warnings, fmt.Sprintf("%s ×%d", label, n))
		}
	}
	slices.Sort(warnings)
	metrics := s.goalMetrics()
	reviewCtx := s.aiContext(set)
	reviewCtx.Hero = s.heroFacts(m.HeroID, reviewRole(m, set), nil)
	prompt := aicoach.ReviewPrompt(aicoach.ReviewInput{
		Context:   reviewCtx,
		Match:     m,
		Targets:   s.targets.TargetsFor(m.HeroID, m.Role).LastHitMap(),
		Timeline:  s.matchTimeline(m.MatchID),
		Warnings:  warnings,
		Detail:    detail,
		Metrics:   metrics,
		WeekGoals: s.weekProgress(time.Now()),
		LastFocus: s.lastFocusFor(reviewRole(m, set), m.HeroID),
	})
	s.hub.publish("review_status", reviewStatus{Text: provider.Info().Name + " is writing your match review…", MatchID: m.MatchID})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()
		r, err := aicoach.RequestReview(ctx, provider, choice, set.AI, set.Language, prompt, slices.Sorted(maps.Keys(metrics)))
		if err != nil {
			s.log.Warn("match review failed", "provider", provider.Info().ID, "err", err)
			if k := ai.KindOf(err); k == ai.ErrAuth || k == ai.ErrLimit {
				s.aiFailed(provider, err)
			} else {
				s.hub.publish("ai_error", "Match review failed: "+err.Error())
			}
			return
		}
		review := stats.Review{Date: time.Now(), MatchID: m.MatchID, Hero: m.Hero, HeroID: m.HeroID,
			Role: reviewRole(m, set), Result: m.Result, Summary: r.Summary, FollowedFocus: r.FollowedFocus,
			Strengths: r.Strengths, Improve: r.Improve, NextGameFocus: r.NextGameFocus}
		if err := s.stats.AppendReview(review); err != nil {
			s.log.Error("save review", "err", err)
		}
		s.engine.SetFocus(review.NextGameFocus)
		s.saveGoals(r, m.MatchID, review.Date)
		s.hub.publish("review", review)
		if cur := s.cfg.Settings(); cur.Voice == config.VoiceSystem && s.speaker != nil {
			s.speaker.Say("Match review ready. Next game focus: "+r.NextGameFocus, false)
		}
		s.log.Info("match review saved", "match", m.MatchID, "focus", r.NextGameFocus)
	}()
	return true
}

func (s *Server) handleAIAsk(w http.ResponseWriter, r *http.Request) {
	set := s.cfg.Settings()
	snap := s.engine.Snapshot(set)
	switch _, _, ok := s.pick(set.AI.Live, set.AI); {
	case !ok:
		http.Error(w, cmp.Or(s.aiBanner().Message, "the AI coach isn't connected; set it up on the AI coach page"), http.StatusConflict)
		return
	case snap.Hero == nil:
		http.Error(w, "no hero yet: start a match first", http.StatusConflict)
		return
	}
	reason := "the player asked for advice during the match"
	if !snap.InMatch {
		reason = "the player asked for advice after the match; focus on what to do differently next game"
	}
	if !s.askAI(reason, snap.MatchID, set, false) {
		http.Error(w, "the coach is still answering the previous question", http.StatusConflict)
		return
	}
	writeJSON(w, map[string]string{"status": "asking"})
}

func (s *Server) handleReviewMatch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	matches, err := s.stats.Matches()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	i := slices.IndexFunc(matches, func(m stats.MatchSummary) bool { return m.MatchID == id })
	switch {
	case i < 0:
		http.Error(w, "match not found", http.StatusNotFound)
		return
	}
	set := s.cfg.Settings()
	if _, _, ok := s.pick(set.AI.Reviews, set.AI); !ok {
		http.Error(w, cmp.Or(s.aiBanner().Message, "the AI coach isn't connected; set it up on the AI coach page"), http.StatusConflict)
		return
	}
	m := matches[i]
	s.hub.publish("review_status", reviewStatus{Text: "Preparing the match review…", MatchID: id})
	go func() {
		var detail *matchdata.Detail
		if isOpenDotaMatch(m) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			detail, _ = s.matches.Enrich(ctx, m, s.cfg.Settings().AccountID, nil)
			cancel()
		}
		s.reviewMatch(m, s.cfg.Settings(), true, detail)
	}()
	writeJSON(w, map[string]string{"status": "writing"})
}

func (s *Server) handleReviews(w http.ResponseWriter, r *http.Request) {
	reviews, err := s.stats.Reviews()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	slices.Reverse(reviews)
	writeJSON(w, reviews[:min(len(reviews), 20)])
}
