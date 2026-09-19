package sim

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Replay posts a recording made with `run -record` back to a trainer, keeping the original
// pacing divided by speed.
func Replay(ctx context.Context, path string, o Options, progress func(n int)) error {
	client := &http.Client{Timeout: 5 * time.Second}
	started := time.Now()
	n := 0
	return ReadRecording(path, func(millis int64, payload map[string]json.RawMessage) error {
		due := time.Duration(float64(millis)*float64(time.Millisecond)/o.Speed) - time.Since(started)
		if due > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(due):
			}
		}
		payload["auth"], _ = json.Marshal(map[string]string{"token": o.Token})
		body, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.URL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("is the trainer running? %w", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("trainer answered %s", resp.Status)
		}
		n++
		if progress != nil {
			progress(n)
		}
		return nil
	})
}

// ReadRecording calls f for each saved update in order. A recording cut off mid-line, such as
// one still being written, ends at its last complete update.
func ReadRecording(path string, f func(millis int64, payload map[string]json.RawMessage) error) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	var r io.Reader = file
	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(file)
		if err != nil {
			return err
		}
		defer gz.Close()
		r = gz
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	var bad error
	for line := 1; sc.Scan(); line++ {
		if bad != nil {
			return bad
		}
		var rec struct {
			Millis  int64                      `json:"t"`
			Payload map[string]json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil || rec.Payload == nil {
			bad = fmt.Errorf("line %d: not a recorded update: %v", line, err)
			continue
		}
		if err := f(rec.Millis, rec.Payload); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return err
	}
	return nil
}
