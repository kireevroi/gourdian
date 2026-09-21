package server

import (
	"cmp"
	"context"
	"encoding/json"
	"testing"

	"gourdian/internal/ai"
)

// listingProvider is an API provider whose models come from its own list.
type listingProvider struct {
	id     string
	models []ai.Model
}

func (l *listingProvider) Info() ai.Info {
	return ai.Info{ID: cmp.Or(l.id, "listing"), Name: "Listing API", Kind: "api"}
}
func (l *listingProvider) Status(context.Context) ai.Status { return ai.Status{State: ai.StateReady} }
func (l *listingProvider) Complete(context.Context, ai.Request) (json.RawMessage, error) {
	return nil, nil
}
func (l *listingProvider) ListModels(context.Context) ([]ai.Model, error) { return l.models, nil }

func TestConnectingAProviderPicksItsModels(t *testing.T) {
	srv, _, _ := newTestServer(t, nil)
	p := &listingProvider{models: []ai.Model{{ID: "gemini-2.5-flash"}, {ID: "gemini-3.8-flash"}, {ID: "gemini-3.7-pro"}, {ID: "lyria-3-pro"}}}
	srv.providers.UseProviders([]ai.Provider{p})
	set := srv.cfg.Settings()
	set.AI.Live.Provider, set.AI.Live.Model = "listing", ""
	set.AI.Reviews.Provider, set.AI.Reviews.Model = "listing", "gemini-1.0-pro" // retired
	set.AI.Fallback.Provider = ""
	if err := srv.cfg.UpdateSettings(set); err != nil {
		t.Fatal(err)
	}
	srv.providers.Check(t.Context(), "listing")
	srv.providers.RefreshModels(t.Context(), "listing")
	got := srv.cfg.Settings().AI
	if got.Live.Model != "gemini-3.8-flash" || got.Reviews.Model != "gemini-3.7-pro" {
		t.Fatalf("live %q, reviews %q", got.Live.Model, got.Reviews.Model)
	}
}

func TestAModelTypedInIsKept(t *testing.T) {
	srv, h, _ := newTestServer(t, nil)
	p := &listingProvider{models: []ai.Model{{ID: "gemini-3.8-flash"}, {ID: "gemini-3.7-pro"}}}
	claude := &listingProvider{id: srv.cfg.Settings().AI.Reviews.Provider, models: []ai.Model{{ID: "sonnet"}}}
	srv.providers.UseProviders([]ai.Provider{p, claude})
	srv.providers.Check(t.Context(), "listing")
	for _, body := range []string{
		`{"ai":{"live":{"provider":"listing","model":""}}}`,
		`{"ai":{"live":{"provider":"listing","model":"gemini-3.9-flash-preview"}}}`,
		`{"ai":{"reviews":{"provider":"listing","model":"sonnet"}}}`,
	} {
		if code := putJSON(t, h, "/api/settings", body); code != 200 {
			t.Fatalf("%s: status %d", body, code)
		}
	}
	srv.providers.RefreshModels(t.Context(), "listing")
	got := srv.cfg.Settings().AI
	if got.Live.Model != "gemini-3.9-flash-preview" || !got.Live.Typed {
		t.Fatalf("the model the player typed was replaced: %+v", got.Live)
	}
	if got.Reviews.Model != "gemini-3.7-pro" {
		t.Fatalf("a model left over from another provider should be replaced, got %q", got.Reviews.Model)
	}
	if code := putJSON(t, h, "/api/settings", `{"ai":{"live":{"provider":"listing","model":"gemini-3.8-flash","typed":true}}}`); code != 200 {
		t.Fatal(code)
	}
	if srv.cfg.Settings().AI.Live.Typed {
		t.Fatal("a model from the list isn't a typed one, whatever the page says")
	}
}
