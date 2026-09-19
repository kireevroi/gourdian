// Package rules keeps the player's own rules and their changes to built-in ones. They live
// in the trainer's data file; built-in defaults come with the app, so updates improve them.
package rules

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"dotatrainer/internal/coach"
	"dotatrainer/internal/config"
	"dotatrainer/internal/stats"
)

const fileName = "rules.json"

type File struct {
	Overrides map[string]coach.RuleOverride `json:"overrides"`
	Custom    []coach.RuleSpec              `json:"custom"`
}

type Store struct {
	db   *stats.Store
	path string // the rules.json of an older version, read once
	mu   sync.Mutex
	file File
}

func Open(dir string, db *stats.Store) (*Store, error) {
	s := &Store{db: db, path: filepath.Join(dir, fileName), file: File{Overrides: map[string]coach.RuleOverride{}}}
	rows, err := db.RuleRows()
	if err != nil {
		return s, err
	}
	for _, r := range rows {
		switch r.Kind {
		case "custom":
			var spec coach.RuleSpec
			if json.Unmarshal([]byte(r.Spec), &spec) == nil {
				s.file.Custom = append(s.file.Custom, spec)
			}
		case "override":
			var o coach.RuleOverride
			if json.Unmarshal([]byte(r.Spec), &o) == nil {
				s.file.Overrides[r.ID] = o
			}
		}
	}
	if db.RulesImported() {
		return s, nil
	}
	return s, s.importFile()
}

// importFile takes over the rules.json of an older version, once.
func (s *Store) importFile() error {
	if err := s.db.MarkRulesImported(); err != nil {
		return err
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var old File
	if err := json.Unmarshal(data, &old); err != nil {
		return fmt.Errorf("parse %s: %w", s.path, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.file.Custom = append(s.file.Custom, old.Custom...)
	for id, o := range old.Overrides {
		s.file.Overrides[id] = o
	}
	return s.save()
}

// Get returns a copy the caller may change.
func (s *Store) Get() File {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.file)
}

func clone(f File) File {
	data, _ := json.Marshal(f)
	var out File
	json.Unmarshal(data, &out)
	if out.Overrides == nil {
		out.Overrides = map[string]coach.RuleOverride{}
	}
	return out
}

func (s *Store) save() error {
	var rows []stats.RuleRow
	for _, spec := range s.file.Custom {
		data, err := json.Marshal(spec)
		if err != nil {
			return err
		}
		rows = append(rows, stats.RuleRow{ID: spec.ID, Kind: "custom", Spec: string(data)})
	}
	for _, id := range slices.Sorted(maps.Keys(s.file.Overrides)) {
		data, err := json.Marshal(s.file.Overrides[id])
		if err != nil {
			return err
		}
		rows = append(rows, stats.RuleRow{ID: id, Kind: "override", Spec: string(data)})
	}
	return s.db.ReplaceRules(rows)
}

var nonWord = regexp.MustCompile(`[^a-z0-9]+`)

func newID(name string) string {
	slug := strings.Trim(nonWord.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(slug) > 24 {
		slug = slug[:24]
	}
	b := make([]byte, 3)
	rand.Read(b)
	return "custom-" + slug + "-" + hex.EncodeToString(b)
}

// SaveCustom adds or replaces a custom rule and returns it with its id.
func (s *Store) SaveCustom(spec coach.RuleSpec) (coach.RuleSpec, error) {
	if err := spec.Validate(); err != nil {
		return spec, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i := slices.IndexFunc(s.file.Custom, func(r coach.RuleSpec) bool { return r.ID == spec.ID })
	switch {
	case spec.ID == "" || !strings.HasPrefix(spec.ID, "custom-"):
		spec.ID = newID(spec.Name)
		s.file.Custom = append(s.file.Custom, spec)
	case i < 0:
		s.file.Custom = append(s.file.Custom, spec)
	default:
		s.file.Custom[i] = spec
	}
	return spec, s.save()
}

func (s *Store) DeleteCustom(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.file.Custom)
	s.file.Custom = slices.DeleteFunc(s.file.Custom, func(r coach.RuleSpec) bool { return r.ID == id })
	if len(s.file.Custom) == n {
		return fmt.Errorf("no rule %q", id)
	}
	return s.save()
}

// SetOverride stores changes to a built-in rule after checking them against its parameters.
func (s *Store) SetOverride(id string, o coach.RuleOverride) error {
	builtins := coach.BuiltinRules("en")
	i := slices.IndexFunc(builtins, func(r coach.Rule) bool { return r.ID == id })
	if i < 0 {
		return fmt.Errorf("no built-in rule %q", id)
	}
	if o.Severity != "" && !slices.Contains([]string{"info", "warn", "urgent"}, o.Severity) {
		return fmt.Errorf("unknown severity %q", o.Severity)
	}
	if o.Voice != "" && o.Voice != "speak" && o.Voice != "silent" {
		return fmt.Errorf("unknown voice setting %q", o.Voice)
	}
	for _, role := range o.Roles {
		if !slices.Contains(config.Roles, role) {
			return fmt.Errorf("unknown position %q", role)
		}
	}
	if o.Spec != nil {
		if builtins[i].Spec == nil {
			return fmt.Errorf("%s is worked out in code, so its cards can't be changed", builtins[i].Label)
		}
		spec := *o.Spec
		spec.ID, spec.Enabled = id, true
		if err := spec.Validate(); err != nil {
			return err
		}
		// An edited rule says everything itself, so the older per-field changes go.
		o = coach.RuleOverride{Spec: &spec}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.file.Overrides[id] = o
	return s.save()
}

func (s *Store) ResetOverride(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.file.Overrides, id)
	return s.save()
}

// Import adds the custom rules from an exported file, replacing rules with the same id, and
// returns how many were added. Invalid rules are skipped.
func (s *Store) Import(f File) (int, error) {
	added := 0
	var errs []error
	for _, spec := range f.Custom {
		if _, err := s.SaveCustom(spec); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", spec.Name, err))
			continue
		}
		added++
	}
	for id, o := range f.Overrides {
		if err := s.SetOverride(id, o); err != nil {
			errs = append(errs, err)
		}
	}
	return added, errors.Join(errs...)
}
