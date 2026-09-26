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

	"gourdian/internal/ai"
	"gourdian/internal/ai/prompts"
	"gourdian/internal/coaching/coach"
	"gourdian/internal/data/ingest"
	"gourdian/internal/game/gsi"
	"gourdian/internal/game/model"
	"gourdian/internal/sys/config"
)

const (
	aiTimeout      = 90 * time.Second
	reviewTimeout  = 3 * time.Minute
	aiFirstAsk     = 180
	aiDeathSpacing = 45
)

// noticeAIProblem tells the player about a paused coach on the HUD and by voice, but only in a
// match: outside one, such as when the app starts with Windows, the dashboard banner is enough.
func (s *Server) noticeAIProblem() {
	set := s.cfg.Settings()
	snap := s.engine.Snapshot(set)
	h := s.providers.Banner()
	if !snap.InMatch || h.Problem == "" || !s.ai.noticePending.CompareAndSwap(true, false) {
		return
	}
	tip := coach.Tip{Rule: "ai_problem", Category: "system", Severity: coach.Warn, Text: h.Message, Clock: snap.Clock, At: time.Now(),
		Speech: "The AI coach is paused. Check the dashboard."}
	s.emitTips(snap.MatchID, []coach.Tip{tip}, set)
}

type aiState struct {
	busy atomic.Bool
	// noticePending is set when a problem hasn't been announced in a match yet.
	noticePending atomic.Bool

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

	ready := s.providers.Ready()
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
	provider, choice, ok := s.providers.Pick(set.AI.Live, set.AI)
	if !ok || !s.ai.busy.CompareAndSwap(false, true) {
		return false
	}
	prompt := prompts.Prompt(s.aiInput(reason, matchID, set))
	s.hub.publish("ai_status", "thinking")
	s.bg.Go(func(ctx context.Context) {
		defer s.ai.busy.Store(false)
		defer s.hub.publish("ai_status", "idle")
		ctx, cancel := context.WithTimeout(ctx, aiTimeout)
		defer cancel()
		started := time.Now()
		suggestions, err := prompts.Suggest(ctx, provider, choice, set.AI, set.Language, prompt)
		if err != nil {
			s.log.Warn("AI coach failed", "provider", provider.Info().ID, "err", err)
			s.providers.Failed(provider, err)
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
		s.emitTips(matchID, tips, s.cfg.Settings())
		s.log.Info("AI coach answered", "provider", provider.Info().ID, "took", time.Since(started).Round(100*time.Millisecond))
	})
	return true
}

func (s *Server) aiContext(set config.Settings) prompts.Context {
	c := prompts.Context{Profile: set.AI.Profile}
	if recent, err := s.stats.Recent(50); err == nil {
		sum := summarize(recent, s.engine.Rules())
		c.History = prompts.History{Matches: sum.Sample, WinRate: sum.WinRate, AvgDeaths: sum.AvgDeaths,
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
func (s *Server) heroFacts(heroID int, role string, owned []string) *prompts.HeroFacts {
	if heroID == 0 {
		return nil
	}
	info, ok := s.data.Hero(heroID)
	if !ok {
		return nil
	}
	h := &prompts.HeroFacts{Name: info.LocalizedName, Roles: info.Roles, Build: map[string][]string{}, Owned: owned}
	if b := s.data.BuildFor(heroID, role); b != nil {
		h.BuildPosition, h.BuildGames, h.BuildWon = b.Position, b.Games, b.Won
		for _, it := range b.Items {
			h.Build[it.Phase] = append(h.Build[it.Phase], fmt.Sprintf("%s (%dg)", it.DName, it.Cost))
		}
	}
	return h
}

func (s *Server) matchTimeline(matchID string) []model.Sample {
	timeline, err := s.stats.Timeline()
	if err != nil {
		return nil
	}
	return slices.DeleteFunc(timeline, func(x model.Sample) bool { return x.MatchID != matchID })
}

func (s *Server) aiInput(reason, matchID string, set config.Settings) prompts.Input {
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
	return prompts.Input{
		Context:  ctx,
		Reason:   reason,
		Role:     set.Role,
		Snapshot: snap,
		Facts:    s.engine.Facts(set.Role),
		Tips:     s.engine.RecentTips(),
		Timeline: s.matchTimeline(matchID),
		Timings:  set.Timings,
	}
}

// reviewMatch skips simulated and practice matches unless forced, so they don't spend subscription usage.
func (s *Server) reviewMatch(m model.MatchSummary, set config.Settings, force bool, detail *ingest.Detail) bool {
	provider, choice, ok := s.providers.Pick(set.AI.Reviews, set.AI)
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
	prompt := prompts.ReviewPrompt(prompts.ReviewInput{
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
	s.bg.Go(func(ctx context.Context) {
		ctx, cancel := context.WithTimeout(ctx, reviewTimeout)
		defer cancel()
		r, err := prompts.RequestReview(ctx, provider, choice, set.AI, set.Language, prompt, slices.Sorted(maps.Keys(metrics)))
		if err != nil {
			s.log.Warn("match review failed", "provider", provider.Info().ID, "err", err)
			if k := ai.KindOf(err); k == ai.ErrAuth || k == ai.ErrLimit {
				s.providers.Failed(provider, err)
			} else {
				s.hub.publish("ai_error", "Match review failed: "+err.Error())
			}
			s.reviewFailed(m.MatchID)
			return
		}
		review := model.Review{Date: time.Now(), MatchID: m.MatchID, Hero: m.Hero, HeroID: m.HeroID,
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
	})
	return true
}

func (s *Server) handleAIAsk(w http.ResponseWriter, r *http.Request) {
	set := s.cfg.Settings()
	snap := s.engine.Snapshot(set)
	switch _, _, ok := s.providers.Pick(set.AI.Live, set.AI); {
	case !ok:
		http.Error(w, cmp.Or(s.providers.Banner().Message, "the AI coach isn't connected; set it up on the AI coach page"), http.StatusConflict)
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
	m, err := s.stats.Match(id)
	if err != nil {
		s.matchProblem(w, "couldn't read that match", err)
		return
	}
	set := s.cfg.Settings()
	if _, _, ok := s.providers.Pick(set.AI.Reviews, set.AI); !ok {
		http.Error(w, cmp.Or(s.providers.Banner().Message, "the AI coach isn't connected; set it up on the AI coach page"), http.StatusConflict)
		return
	}
	s.hub.publish("review_status", reviewStatus{Text: "Preparing the match review…", MatchID: id})
	s.bg.Go(func(ctx context.Context) {
		var detail *ingest.Detail
		if isOpenDotaMatch(m) {
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			detail, _ = s.matches.Enrich(ctx, m, s.cfg.Settings().AccountID, nil)
			cancel()
		}
		if !s.reviewMatch(m, s.cfg.Settings(), true, detail) {
			s.reviewFailed(id)
		}
	})
	writeJSON(w, map[string]string{"status": "writing"})
}

func (s *Server) handleReviews(w http.ResponseWriter, r *http.Request) {
	reviews, err := s.stats.Reviews()
	if err != nil {
		s.failed(w, http.StatusInternalServerError, "couldn't read your match reviews", err)
		return
	}
	slices.Reverse(reviews)
	writeJSON(w, reviews[:min(len(reviews), 20)])
}

// askDraft asks the coach which hero to take. forced is the player pressing the button on the
// dashboard, which ignores the setting but still needs a provider and a board to talk about.
func (s *Server) askDraft(set config.Settings, forced bool) bool {
	if !forced && (!set.AI.Enabled || !set.AI.Draft) {
		return false
	}
	provider, choice, ok := s.providers.Pick(set.AI.Live, set.AI)
	if !ok {
		return false
	}
	snap := s.snapshot(set)
	if snap.Picks == nil || !s.ai.busy.CompareAndSwap(false, true) {
		return false
	}
	prompt := prompts.DraftPrompt(prompts.DraftInput{Context: s.aiContext(set), Role: set.Role, Board: snap.Picks})
	s.hub.publish("ai_status", "thinking")
	s.bg.Go(func(ctx context.Context) {
		// Deferred first to run after busy clears: the new position's question was refused.
		moved := false
		defer func() {
			if moved {
				s.askDraft(s.cfg.Settings(), forced)
			}
		}()
		defer s.ai.busy.Store(false)
		defer s.hub.publish("ai_status", "idle")
		ctx, cancel := context.WithTimeout(ctx, aiTimeout)
		defer cancel()
		advice, err := prompts.AskDraft(ctx, provider, choice, set.AI, set.Language, prompt, len(snap.Picks.Enemies) > 0)
		if err != nil {
			s.log.Warn("AI coach failed on the draft", "provider", provider.Info().ID, "err", err)
			s.providers.Failed(provider, err)
			return
		}
		now := s.cfg.Settings()
		if !pickMatters(s.snapshot(now)) {
			s.log.Info("AI coach answered the draft too late", "provider", provider.Info().ID)
			return
		}
		if now.Role != set.Role {
			s.log.Info("AI coach answered for a position the player left", "asked", set.Role, "now", now.Role)
			moved = true
			return
		}
		s.emitTips(snap.MatchID, []coach.Tip{{Rule: "ai", Category: "ai", Severity: coach.Info,
			Text: advice, Speech: advice, Clock: snap.Clock, At: time.Now()}}, s.cfg.Settings())
		s.log.Info("AI coach answered the draft", "provider", provider.Info().ID)
	})
	return true
}
