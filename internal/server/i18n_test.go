package server

import (
	"encoding/json"
	"testing"

	"gourdian/internal/coach"
)

func TestRuleBuilderIsInRussian(t *testing.T) {
	raw, err := webFS.ReadFile("web/i18n/ru.json")
	if err != nil {
		t.Fatal(err)
	}
	var dict struct {
		Strings map[string]string `json:"strings"`
	}
	if err := json.Unmarshal(raw, &dict); err != nil {
		t.Fatal(err)
	}
	for _, f := range coach.Fields {
		if dict.Strings[f.Label] == "" {
			t.Errorf("field %s: no Russian for %q", f.ID, f.Label)
		}
	}
	for _, e := range coach.Events {
		if dict.Strings[e.Label] == "" {
			t.Errorf("event %s: no Russian for %q", e.ID, e.Label)
		}
	}
}
