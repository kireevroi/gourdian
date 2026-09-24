package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGuardRefusesOtherSitesAndMachines(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	h := Guard(ok)
	for _, c := range []struct {
		host, remote, path string
		want               int
	}{
		{"127.0.0.1:4570", "127.0.0.1:50000", "/api/settings", http.StatusOK},
		{"localhost:4570", "[::1]:50000", "/api/settings", http.StatusOK},
		{"[::1]:4570", "[::1]:50000", "/events", http.StatusOK},
		// DNS rebinding: a page on attacker.example that now resolves to 127.0.0.1.
		{"attacker.example:4570", "127.0.0.1:50000", "/api/settings", http.StatusForbidden},
		{"attacker.example:4570", "127.0.0.1:50000", "/gsi", http.StatusForbidden},
		// Listening on every interface doesn't open the dashboard to the network.
		{"192.168.1.5:4570", "192.168.1.7:50000", "/api/settings", http.StatusForbidden},
		{"192.168.1.5:4570", "192.168.1.7:50000", "/", http.StatusForbidden},
		// Dota's posts carry the token, so they may come from elsewhere.
		{"192.168.1.5:4570", "192.168.1.7:50000", "/gsi", http.StatusOK},
	} {
		req := httptest.NewRequest(http.MethodPut, "http://"+c.host+c.path, strings.NewReader("{}"))
		req.Host, req.RemoteAddr = c.host, c.remote
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s from %s to %s: %d, want %d", c.host, c.remote, c.path, rec.Code, c.want)
		}
	}
}

// The path to a provider's CLI is run as a program, so only config.json can set it.
func TestSettingsCantPointAtAProgram(t *testing.T) {
	srv, h, _ := newTestServer(t, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/settings",
		strings.NewReader(`{"voice_rate":3,"ai":{"cli_paths":{"claude":"/tmp/evil"}}}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	set := srv.cfg.Settings()
	if set.VoiceRate != 3 || set.AI.CLIPaths["claude"] != "" {
		t.Fatalf("voice rate %d, claude path %q", set.VoiceRate, set.AI.CLIPaths["claude"])
	}
}
