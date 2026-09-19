package dotadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// forever is a maxAge for cached answers that never change, like a parsed match.
const forever = time.Duration(-1)

// cachedBytes answers from the disk cache while the file is younger than maxAge and decode
// takes it. Otherwise it fetches a fresh answer, and saves it when decode takes it and keep
// says so; if fetching or decoding fails, an older cached copy that decodes is better than
// nothing. The cache holds the bytes as they came, so a field read by a later version is
// there for answers cached before it.
func (c *Client) cachedBytes(name string, maxAge time.Duration, fetch func() ([]byte, error), decode func([]byte) error, keep func() bool) error {
	path := filepath.Join(c.cacheDir, name)
	if fi, err := os.Stat(path); err == nil && (maxAge == forever || time.Since(fi.ModTime()) < maxAge) {
		if data, err := os.ReadFile(path); err == nil && decode(data) == nil {
			return nil
		}
	}
	data, err := fetch()
	if err == nil {
		if err = decode(data); err != nil {
			err = fmt.Errorf("decode %s: %w", name, err)
		}
	}
	if err != nil {
		if old, readErr := os.ReadFile(path); readErr == nil && decode(old) == nil {
			c.log.Warn("using stale cache", "file", name, "err", err)
			return nil
		}
		return err
	}
	if keep == nil || keep() {
		c.save(name, data)
	}
	return nil
}

// cached is cachedBytes for answers that decode as a T; keep, if set, decides whether a fresh
// one is saved.
func cached[T any](c *Client, name string, maxAge time.Duration, fetch func() ([]byte, error), keep func(T) bool) (T, error) {
	var v T
	err := c.cachedBytes(name, maxAge, fetch, func(data []byte) error {
		var fresh T // decoded apart, so a copy that fails leaves nothing behind in v
		if err := json.Unmarshal(data, &fresh); err != nil {
			return err
		}
		v = fresh
		return nil
	}, func() bool { return keep == nil || keep(v) })
	return v, err
}

// save writes data to the cache through a temporary file of its own, so a crash never leaves
// half a file and two saves of the same file can't trip over each other.
func (c *Client) save(name string, data []byte) {
	path := filepath.Join(c.cacheDir, name)
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	var tmp *os.File
	if err == nil {
		tmp, err = os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	}
	if err == nil {
		_, err = tmp.Write(data)
		if closeErr := tmp.Close(); err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Rename(tmp.Name(), path)
		}
		if err != nil {
			os.Remove(tmp.Name())
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
