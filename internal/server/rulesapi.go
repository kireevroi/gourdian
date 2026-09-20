package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"gourdian/internal/coach"
	"gourdian/internal/gsi"
	"gourdian/internal/rules"
	"gourdian/internal/sim"
)

// applyRules hands the stored rules to the engine.
func (s *Server) applyRules() {
	f := s.rules.Get()
	s.engine.Configure(s.cfg.Settings().Language, f.Custom, f.Overrides)
}

type rulesResponse struct {
	Builtin    []coach.Rule                  `json:"builtin"`
	Overrides  map[string]coach.RuleOverride `json:"overrides"`
	Custom     []coach.RuleSpec              `json:"custom"`
	Fields     []coach.Field                 `json:"fields"`
	Events     []coach.Event                 `json:"events"`
	Categories []string                      `json:"categories"`
	Templates  []coach.RuleSpec              `json:"templates"`
	// Fired counts each rule's tips over the last 10 real matches.
	Fired   map[string]int `json:"fired"`
	Matches int            `json:"matches"`
}

func (s *Server) rulesResponse() rulesResponse {
	f := s.rules.Get()
	if f.Custom == nil {
		f.Custom = []coach.RuleSpec{}
	}
	resp := rulesResponse{Builtin: coach.BuiltinRules(s.cfg.Settings().Language), Overrides: f.Overrides, Custom: f.Custom, Fields: coach.Fields,
		Events: coach.Events, Categories: coach.Categories, Templates: rules.Templates, Fired: map[string]int{}}
	if recent, err := s.stats.Recent(50); err == nil {
		ids := map[string]bool{}
		for _, m := range recent {
			if m.Real() && m.Coached() && len(ids) < 10 {
				ids[m.MatchID] = true
			}
		}
		resp.Matches = len(ids)
		if counts, err := s.stats.RuleCounts(ids); err == nil {
			resp.Fired = counts
		}
	}
	return resp
}

func (s *Server) rulesChanged(w http.ResponseWriter) {
	s.applyRules()
	resp := s.rulesResponse()
	s.hub.publish("rules", true)
	s.publishSettings()
	writeJSON(w, resp)
}

func (s *Server) handleRules(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.rulesResponse()) }

func decodeSpec(w http.ResponseWriter, r *http.Request) (coach.RuleSpec, bool) {
	var spec coach.RuleSpec
	if err := readJSON(w, r, 64<<10, &spec); err != nil {
		http.Error(w, "bad rule: "+err.Error(), http.StatusBadRequest)
		return spec, false
	}
	return spec, true
}

func (s *Server) handleSaveRule(w http.ResponseWriter, r *http.Request) {
	spec, ok := decodeSpec(w, r)
	if !ok {
		return
	}
	if _, err := s.rules.SaveCustom(spec); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.rulesChanged(w)
}

func (s *Server) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	if err := s.rules.DeleteCustom(r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	s.rulesChanged(w)
}

func (s *Server) handleOverride(w http.ResponseWriter, r *http.Request) {
	var o coach.RuleOverride
	if err := readJSON(w, r, 16<<10, &o); err != nil {
		http.Error(w, "bad change: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.rules.SetOverride(r.PathValue("id"), o); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.rulesChanged(w)
}

func (s *Server) handleResetOverride(w http.ResponseWriter, r *http.Request) {
	if err := s.rules.ResetOverride(r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.rulesChanged(w)
}

type checkResponse struct {
	Conditions []coach.CondResult `json:"conditions"`
	Match      bool               `json:"match"`
	Error      string             `json:"error,omitempty"`
}

// handleCheckRule shows how a rule's conditions evaluate in the current match.
func (s *Server) handleCheckRule(w http.ResponseWriter, r *http.Request) {
	spec, ok := decodeSpec(w, r)
	if !ok {
		return
	}
	results, match, err := s.engine.CheckSpec(spec, s.cfg.Settings())
	resp := checkResponse{Conditions: results, Match: match}
	if err != nil {
		resp.Error = err.Error()
	}
	writeJSON(w, resp)
}

type recordingInfo struct {
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

type testFire struct {
	Clock    int    `json:"clock"`
	Text     string `json:"text"`
	Severity string `json:"severity"`
}

// handleTestRule replays a recorded match through one rule, without touching the live match.
func (s *Server) handleTestRule(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Rule      coach.RuleSpec `json:"rule"`
		Recording string         `json:"recording"`
	}
	if err := readJSON(w, r, 64<<10, &body); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := body.Rule.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := filepath.Base(body.Recording)
	if name != body.Recording || !strings.Contains(name, ".jsonl") {
		http.Error(w, "pick one of the recordings", http.StatusBadRequest)
		return
	}
	path := filepath.Join(s.recordingsDir(), name)
	var readErr error
	states := func(yield func(*gsi.State) bool) {
		stop := errors.New("stop")
		readErr = sim.ReadRecording(path, func(_ int64, payload map[string]json.RawMessage) error {
			data, _ := json.Marshal(payload)
			var st gsi.State
			if json.Unmarshal(data, &st) != nil {
				return nil
			}
			if !yield(&st) {
				return stop
			}
			return nil
		})
		if errors.Is(readErr, stop) {
			readErr = nil
		}
	}
	tips := coach.TestSpec(s.data, body.Rule, s.cfg.Settings(), states)
	if readErr != nil {
		http.Error(w, "couldn't read the recording: "+readErr.Error(), http.StatusBadRequest)
		return
	}
	fires := make([]testFire, 0, len(tips))
	for _, t := range tips {
		fires = append(fires, testFire{Clock: t.Clock, Text: t.Text, Severity: string(t.Severity)})
	}
	writeJSON(w, fires)
}

func (s *Server) handleExportRules(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Disposition", `attachment; filename="dota-trainer-rules.json"`)
	writeJSON(w, s.rules.Get())
}

func (s *Server) handleImportRules(w http.ResponseWriter, r *http.Request) {
	var f rules.File
	if err := readJSON(w, r, 1<<20, &f); err != nil {
		http.Error(w, "that isn't an exported rules file: "+err.Error(), http.StatusBadRequest)
		return
	}
	added, err := s.rules.Import(f)
	s.applyRules()
	s.hub.publish("rules", true)
	resp := map[string]any{"added": added}
	if err != nil {
		resp["skipped"] = err.Error()
	}
	writeJSON(w, resp)
}

type pickItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// handleCatalog lists items and heroes for the rule editor's pickers.
func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	var resp struct {
		Items  []pickItem `json:"items"`
		Heroes []pickItem `json:"heroes"`
	}
	if s.data != nil {
		for name, it := range s.data.Items() {
			if it.DName != "" && !strings.HasPrefix(name, "recipe_") {
				resp.Items = append(resp.Items, pickItem{ID: name, Name: it.DName})
			}
		}
		for _, h := range s.data.Heroes() {
			resp.Heroes = append(resp.Heroes, pickItem{ID: strconv.Itoa(h.ID), Name: h.LocalizedName})
		}
	}
	slices.SortFunc(resp.Items, func(a, b pickItem) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(resp.Heroes, func(a, b pickItem) int { return strings.Compare(a.Name, b.Name) })
	writeJSON(w, resp)
}
