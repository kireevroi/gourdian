package rules

import (
	"strings"
	"testing"

	"dotatrainer/internal/coach"
	"dotatrainer/internal/stats"
)

func TestTemplatesAreValid(t *testing.T) {
	for _, tpl := range Templates {
		if err := tpl.Validate(); err != nil {
			t.Errorf("%s: %v", tpl.Name, err)
		}
	}
}

func TestCustomRulesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, _ := openTest(t, dir)
	spec := Templates[0]
	spec.Enabled = true
	saved, err := s.SaveCustom(spec)
	if err != nil || !strings.HasPrefix(saved.ID, "custom-save-for-bkb-") {
		t.Fatalf("saved %q, %v", saved.ID, err)
	}
	saved.Name = "Save for BKB early"
	if _, err := s.SaveCustom(saved); err != nil {
		t.Fatal(err)
	}
	reopened, _ := openTest(t, dir)
	if f := reopened.Get(); len(f.Custom) != 1 || f.Custom[0].Name != "Save for BKB early" {
		t.Fatalf("custom = %+v", f.Custom)
	}
	bad := spec
	bad.Then.Text = ""
	if _, err := s.SaveCustom(bad); err == nil {
		t.Fatal("invalid rules must not be saved")
	}
	if err := s.DeleteCustom(saved.ID); err != nil || len(s.Get().Custom) != 0 {
		t.Fatalf("delete: %v", err)
	}
}

func TestOverridesAreChecked(t *testing.T) {
	s, _ := openTest(t, t.TempDir())
	if err := s.SetOverride("item_timing", coach.RuleOverride{Severity: "urgent", Voice: "silent"}); err != nil {
		t.Fatal(err)
	}
	for name, o := range map[string]coach.RuleOverride{
		"severity": {Severity: "loud"},
		"voice":    {Voice: "loud"},
		"role":     {Roles: []string{"jungler"}},
	} {
		if err := s.SetOverride("item_timing", o); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := s.SetOverride("no_such_rule", coach.RuleOverride{}); err == nil {
		t.Error("unknown rule accepted")
	}
	s.ResetOverride("item_timing")
	if len(s.Get().Overrides) != 0 {
		t.Fatal("reset should remove the override")
	}
}

func TestImportSkipsInvalidRules(t *testing.T) {
	s, _ := openTest(t, t.TempDir())
	good, bad := Templates[1], Templates[2]
	bad.Name = ""
	n, err := s.Import(File{Custom: []coach.RuleSpec{good, bad}})
	if n != 1 || err == nil || len(s.Get().Custom) != 1 {
		t.Fatalf("imported %d, %v", n, err)
	}
}

func TestEditingABuiltinRuleStoresItsCards(t *testing.T) {
	s, err := openTest(t, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	spec, _ := coach.DefaultSpec("no_tp")
	spec.When.For = 25
	if err := s.SetOverride(spec.ID, coach.RuleOverride{Severity: "urgent", Spec: &spec}); err != nil {
		t.Fatal(err)
	}
	got := s.Get().Overrides[spec.ID]
	if got.Spec == nil || got.Spec.When.For != 25 {
		t.Fatalf("stored %+v", got)
	}
	if got.Severity != "" {
		t.Fatal("an edited rule carries its own severity, so the old override should be dropped")
	}
	bad := spec
	bad.Then.Text = ""
	if err := s.SetOverride(spec.ID, coach.RuleOverride{Spec: &bad}); err == nil {
		t.Fatal("an empty alert text was accepted")
	}
	if err := s.SetOverride("no_such_rule", coach.RuleOverride{Spec: &spec}); err == nil {
		t.Fatal("cards for an unknown rule were accepted")
	}
}

func openTest(t *testing.T, dir string) (*Store, error) {
	t.Helper()
	db, err := stats.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return Open(dir, db)
}
