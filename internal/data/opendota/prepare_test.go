package opendota

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// An app started at sign-in stays off the network until something asks for data, and then
// loads it instead of waiting for Dota.
func TestPreparedClientLoadsWhenAsked(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/constants/heroes":
			w.Write([]byte(`{"1": {"id": 1, "localized_name": "Anti-Mage"}}`))
		default:
			w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(srv.Close)
	c := New(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(c.Wait)
	c.SetBaseURL(srv.URL)
	c.Prepare(t.Context())
	time.Sleep(50 * time.Millisecond)
	if n := calls.Load(); n != 0 {
		t.Fatalf("a prepared client called OpenDota %d times before anything asked", n)
	}
	c.Hero(1)
	c.WaitReady(t.Context())
	if h, ok := c.Hero(1); !ok || h.LocalizedName != "Anti-Mage" {
		t.Fatalf("hero 1 = %+v, %v", h, ok)
	}
}

// A client nobody started never goes to the network, however it's asked.
func TestUnstartedClientStaysOffline(t *testing.T) {
	c := New(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.SetBaseURL("http://127.0.0.1:1")
	if c.Items() != nil || len(c.Heroes()) != 0 {
		t.Fatal("an unstarted client has data")
	}
	c.Wait()
}
