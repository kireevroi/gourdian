package sim

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"gourdian/internal/coaching/coach"
)

// ManualPrefix marks recordings started by hand, which are never pruned. Automatic ones are named
// <timestamp>_<match id>, so sorting by name sorts them by age.
const ManualPrefix = "manual_"

const flushEvery = 5 * time.Second

// Recorder saves raw game-state posts to gzipped JSONL in Dir, in the format ReadRecording reads.
type Recorder struct {
	Dir string
	Log *slog.Logger

	mu  sync.Mutex
	cur *recording
}

type recording struct {
	mu      sync.Mutex
	path    string
	auto    bool
	f       *os.File
	gz      *gzip.Writer
	start   time.Time
	flushed time.Time
	token   []byte
}

type recorded struct {
	Millis  int64           `json:"t"`
	Payload json.RawMessage `json:"payload"`
}

// StartManual records until Stop, keeping a manual recording that is already running.
func (r *Recorder) StartManual(token string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cur != nil && !r.cur.auto {
		return r.cur.path, nil
	}
	if r.cur != nil {
		r.cur.close()
	}
	return r.open(ManualPrefix+time.Now().Format("2006-01-02_15-04-05"), false, token)
}

// StartAuto records a match unless the player is already recording by hand.
func (r *Recorder) StartAuto(matchID, token string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cur != nil && !r.cur.auto {
		return
	}
	if r.cur != nil {
		r.cur.close()
	}
	suffix := matchID
	if strings.HasPrefix(matchID, coach.LocalMatchPrefix) {
		suffix = "practice"
	}
	if _, err := r.open(time.Now().Format("2006-01-02_15-04-05")+"_"+suffix, true, token); err != nil {
		r.Log.Warn("couldn't start recording", "err", err)
	}
}

// StopAuto ends an automatic recording and deletes the oldest ones beyond keep.
func (r *Recorder) StopAuto(keep int) {
	r.mu.Lock()
	cur := r.cur
	if cur == nil || !cur.auto {
		r.mu.Unlock()
		return
	}
	r.cur = nil
	r.mu.Unlock()
	cur.close()
	r.Log.Info("match recording saved", "file", cur.path)
	r.prune(keep)
}

func (r *Recorder) Stop() error {
	r.mu.Lock()
	cur := r.cur
	r.cur = nil
	r.mu.Unlock()
	if cur == nil {
		return nil
	}
	r.Log.Info("recording saved", "file", cur.path)
	return cur.close()
}

func (r *Recorder) Path() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cur == nil {
		return ""
	}
	return r.cur.path
}

func (r *Recorder) Write(body []byte) error {
	r.mu.Lock()
	cur := r.cur
	r.mu.Unlock()
	if cur == nil {
		return nil
	}
	return cur.write(body)
}

func (r *Recorder) open(name string, auto bool, token string) (string, error) {
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(r.Dir, name+".jsonl.gz")
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	r.cur = &recording{path: path, auto: auto, f: f, gz: gzip.NewWriter(f), start: time.Now(), token: []byte(token)}
	r.Log.Info("recording game data", "file", path)
	return path, nil
}

func (r *Recorder) prune(keep int) {
	matches, _ := filepath.Glob(filepath.Join(r.Dir, "*.jsonl.gz"))
	matches = slices.DeleteFunc(matches, func(p string) bool { return strings.HasPrefix(filepath.Base(p), ManualPrefix) })
	slices.Sort(matches)
	for _, old := range matches[:max(0, len(matches)-keep)] {
		if err := os.Remove(old); err == nil {
			r.Log.Info("deleted old recording", "file", old)
		}
	}
}

func (c *recording) write(body []byte) error {
	line, err := json.Marshal(recorded{
		Millis:  time.Since(c.start).Milliseconds(),
		Payload: bytes.ReplaceAll(bytes.TrimSpace(body), c.token, []byte("redacted")),
	})
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gz == nil {
		return nil
	}
	if _, err := c.gz.Write(append(line, '\n')); err != nil {
		return err
	}
	// Flush now and then, so a recording still in progress, or cut off by a crash, is readable.
	if time.Since(c.flushed) >= flushEvery {
		c.flushed = time.Now()
		return c.gz.Flush()
	}
	return nil
}

func (c *recording) close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gz == nil {
		return nil
	}
	err := errors.Join(c.gz.Close(), c.f.Close())
	c.gz = nil
	return err
}
