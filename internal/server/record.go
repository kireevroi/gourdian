package server

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"dotatrainer/internal/coach"
)

// recorder appends raw GSI payloads to a gzipped JSONL file for later replay.
type recorder struct {
	mu      sync.Mutex
	path    string
	auto    bool
	f       *os.File
	gz      *gzip.Writer
	start   time.Time
	flushed time.Time
	token   []byte
}

type recordedPayload struct {
	Millis  int64           `json:"t"`
	Payload json.RawMessage `json:"payload"`
}

// Manual recordings carry this prefix and are never pruned; automatic ones are named
// <timestamp>_<match id> so sorting by name sorts them by age.
const manualPrefix = "manual_"

func (s *Server) recordingsDir() string { return filepath.Join(s.workDir, "recordings") }

// StartRecording saves every following GSI payload to a new file in the data folder.
func (s *Server) StartRecording() (string, error) {
	s.recMu.Lock()
	defer s.recMu.Unlock()
	if s.rec != nil && !s.rec.auto {
		return s.rec.path, nil
	}
	if s.rec != nil {
		s.rec.close()
	}
	return s.openRecording(manualPrefix+time.Now().Format("2006-01-02_15-04-05"), false)
}

// startAutoRecording records a match unless the player is already recording by hand.
func (s *Server) startAutoRecording(matchID string) {
	s.recMu.Lock()
	defer s.recMu.Unlock()
	if s.rec != nil && !s.rec.auto {
		return
	}
	if s.rec != nil {
		s.rec.close()
	}
	suffix := matchID
	if strings.HasPrefix(matchID, coach.LocalMatchPrefix) {
		suffix = "practice"
	}
	if _, err := s.openRecording(time.Now().Format("2006-01-02_15-04-05")+"_"+suffix, true); err != nil {
		s.log.Warn("couldn't start recording", "err", err)
	}
}

// stopAutoRecording ends an automatic recording and deletes the oldest ones beyond keep.
func (s *Server) stopAutoRecording(keep int) {
	s.recMu.Lock()
	r := s.rec
	if r == nil || !r.auto {
		s.recMu.Unlock()
		return
	}
	s.rec = nil
	s.recMu.Unlock()
	r.close()
	s.log.Info("match recording saved", "file", r.path)
	s.pruneRecordings(keep)
}

func (s *Server) openRecording(name string, auto bool) (string, error) {
	if err := os.MkdirAll(s.recordingsDir(), 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(s.recordingsDir(), name+".jsonl.gz")
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	s.rec = &recorder{path: path, auto: auto, f: f, gz: gzip.NewWriter(f), start: time.Now(), token: []byte(s.cfg.Get().Token)}
	s.log.Info("recording game data", "file", path)
	return path, nil
}

func (s *Server) pruneRecordings(keep int) {
	matches, _ := filepath.Glob(filepath.Join(s.recordingsDir(), "*.jsonl.gz"))
	matches = slices.DeleteFunc(matches, func(p string) bool { return strings.HasPrefix(filepath.Base(p), manualPrefix) })
	slices.Sort(matches)
	for _, old := range matches[:max(0, len(matches)-keep)] {
		if err := os.Remove(old); err == nil {
			s.log.Info("deleted old recording", "file", old)
		}
	}
}

func (s *Server) StopRecording() error {
	s.recMu.Lock()
	r := s.rec
	s.rec = nil
	s.recMu.Unlock()
	if r == nil {
		return nil
	}
	s.log.Info("recording saved", "file", r.path)
	return r.close()
}

func (s *Server) recordingPath() string {
	s.recMu.Lock()
	defer s.recMu.Unlock()
	if s.rec == nil {
		return ""
	}
	return s.rec.path
}

func (s *Server) record(body []byte) error {
	s.recMu.Lock()
	r := s.rec
	s.recMu.Unlock()
	if r == nil {
		return nil
	}
	return r.write(body)
}

func (r *recorder) write(body []byte) error {
	line, err := json.Marshal(recordedPayload{
		Millis:  time.Since(r.start).Milliseconds(),
		Payload: bytes.ReplaceAll(bytes.TrimSpace(body), r.token, []byte("redacted")),
	})
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.gz == nil {
		return nil
	}
	if _, err := r.gz.Write(append(line, '\n')); err != nil {
		return err
	}
	// Flush now and then, so a recording still in progress, or cut off by a crash, is readable.
	if time.Since(r.flushed) >= recordFlushEvery {
		r.flushed = time.Now()
		return r.gz.Flush()
	}
	return nil
}

const recordFlushEvery = 5 * time.Second

func (r *recorder) close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.gz == nil {
		return nil
	}
	err := errors.Join(r.gz.Close(), r.f.Close())
	r.gz = nil
	return err
}

func (s *Server) Close() error {
	s.cancel()
	return s.StopRecording()
}

func (s *Server) handleRecording(w http.ResponseWriter, r *http.Request) {
	var body struct {
		On bool `json:"on"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil {
		http.Error(w, `send {"on": true} or {"on": false}`, http.StatusBadRequest)
		return
	}
	var err error
	if body.On {
		_, err = s.StartRecording()
	} else {
		err = s.StopRecording()
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := s.settingsResponse()
	s.hub.publish("settings", resp)
	writeJSON(w, resp)
}
