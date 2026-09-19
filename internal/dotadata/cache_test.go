package dotadata

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(t *testing.T, h http.HandlerFunc) *Client {
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.SetBaseURL(srv.URL)
	return c
}

// A 429 means "not so fast": the client waits as long as OpenDota asks and tries again.
func TestTooManyRequestsIsWaitedOut(t *testing.T) {
	var calls atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`{"ok": true}`))
	})
	start := time.Now()
	data, err := c.fetch(context.Background(), "/anything")
	if err != nil || string(data) != `{"ok": true}` {
		t.Fatalf("%q, %v", data, err)
	}
	if waited := time.Since(start); waited < time.Second || calls.Load() != 2 {
		t.Fatalf("%d calls in %v; want a retry after the second OpenDota asked for", calls.Load(), waited)
	}
}

// OpenDota's free tier allows 60 calls a minute: bursts pass, then one call a second.
func TestLimiterAllowsABurstThenSlowsDown(t *testing.T) {
	l := &limiter{rate: 20, burst: 3, tokens: 3, last: time.Now()}
	start := time.Now()
	for range 3 {
		l.wait(context.Background())
	}
	if burst := time.Since(start); burst > 20*time.Millisecond {
		t.Fatalf("the burst took %v", burst)
	}
	l.wait(context.Background())
	if slowed := time.Since(start); slowed < 30*time.Millisecond {
		t.Fatalf("the fourth call came after %v; want about 50 ms at 20 a second", slowed)
	}
}

// When OpenDota is down, an old cached answer is better than none.
func TestCachedFallsBackToAStaleCopy(t *testing.T) {
	c := New(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	fresh := func() (int, error) { return 7, nil }
	down := func() (int, error) { return 0, errors.New("OpenDota is down") }
	if v, err := cached(c, "n.json", 0, fresh, nil); err != nil || v != 7 {
		t.Fatalf("%d, %v", v, err)
	}
	if v, err := cached(c, "n.json", 0, down, nil); err != nil || v != 7 { // maxAge 0: always stale
		t.Fatalf("got %d, %v; want the stale 7", v, err)
	}
	if _, err := cached(c, "other.json", 0, down, nil); err == nil {
		t.Fatal("no cache and no OpenDota, yet no error")
	}
}
