package dotadata

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// forever is a maxAge for cached answers that never change, like a parsed match.
const forever = time.Duration(-1)

// cached answers from the disk cache while the file is younger than maxAge. Otherwise it fetches
// a fresh value and saves it when keep says so (nil keeps everything); if the fetch fails, an
// older cached copy is better than nothing.
func cached[T any](c *Client, name string, maxAge time.Duration, fetch func() (T, error), keep func(T) bool) (T, error) {
	path := filepath.Join(c.cacheDir, name)
	var v T
	if fi, err := os.Stat(path); err == nil && (maxAge == forever || time.Since(fi.ModTime()) < maxAge) {
		if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &v) == nil {
			return v, nil
		}
	}
	fresh, err := fetch()
	if err != nil {
		var stale T
		if data, readErr := os.ReadFile(path); readErr == nil && json.Unmarshal(data, &stale) == nil {
			c.log.Warn("using stale cache", "file", name, "err", err)
			return stale, nil
		}
		return v, err
	}
	if keep == nil || keep(fresh) {
		c.save(name, fresh)
	}
	return fresh, nil
}

// save writes v to the cache through a temporary file, so a crash never leaves half a file.
func (c *Client) save(name string, v any) {
	path := filepath.Join(c.cacheDir, name)
	data, err := json.Marshal(v)
	if err == nil {
		err = os.MkdirAll(filepath.Dir(path), 0o755)
	}
	if err == nil {
		tmp := path + ".tmp"
		if err = os.WriteFile(tmp, data, 0o644); err == nil {
			err = os.Rename(tmp, path)
		}
	}
	if err != nil {
		c.log.Warn("couldn't cache OpenDota data", "file", name, "err", err)
	}
}

// limiter keeps a client inside OpenDota's free tier of 60 calls a minute: a burst of up to
// burst calls, then rate a second. A 429 answer pauses it for as long as OpenDota asks.
type limiter struct {
	mu     sync.Mutex
	rate   float64 // calls a second; 0 means no limit
	burst  float64
	tokens float64
	last   time.Time
	until  time.Time // paused until then
}

func newLimiter() *limiter {
	return &limiter{rate: 1, burst: 10, tokens: 10, last: time.Now()}
}

func (l *limiter) wait(ctx context.Context) error {
	for {
		l.mu.Lock()
		now := time.Now()
		l.tokens = min(l.burst, l.tokens+now.Sub(l.last).Seconds()*l.rate)
		l.last = now
		var wait time.Duration
		switch {
		case now.Before(l.until): // OpenDota asked for a pause
			wait = l.until.Sub(now)
		case l.rate == 0:
			l.mu.Unlock()
			return nil
		case l.tokens >= 1:
			l.tokens--
			l.mu.Unlock()
			return nil
		default:
			wait = time.Duration((1 - l.tokens) / l.rate * float64(time.Second))
		}
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

func (l *limiter) pause(d time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if until := time.Now().Add(d); until.After(l.until) {
		l.until = until
	}
}

// retryAfter is how long a 429 answer asks to wait, within reason.
func retryAfter(resp *http.Response) time.Duration {
	if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
		return min(time.Duration(s)*time.Second, time.Minute)
	}
	return 5 * time.Second
}
