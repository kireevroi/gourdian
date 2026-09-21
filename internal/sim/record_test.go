package sim

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newRecorder(t *testing.T) *Recorder {
	return &Recorder{Dir: t.TempDir(), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestWhatIsRecordedReadsBack(t *testing.T) {
	r := newRecorder(t)
	path, err := r.StartManual("secret-token")
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"map":{"clock_time":10},"auth":{"token":"secret-token"}}`,
		`{"map":{"clock_time":11},"auth":{"token":"secret-token"}}`,
	} {
		if err := r.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Stop(); err != nil {
		t.Fatal(err)
	}

	var clocks []string
	err = ReadRecording(path, func(_ int64, payload map[string]json.RawMessage) error {
		if strings.Contains(string(payload["auth"]), "secret-token") {
			t.Errorf("the token was saved: %s", payload["auth"])
		}
		clocks = append(clocks, string(payload["map"]))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(clocks) != 2 || !strings.Contains(clocks[1], "11") {
		t.Fatalf("read back %q, want both updates in order", clocks)
	}
}

func TestNothingIsWrittenWithoutARecording(t *testing.T) {
	r := newRecorder(t)
	if err := r.Write([]byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if r.Path() != "" {
		t.Fatalf("Path = %q, want none", r.Path())
	}
	if entries, _ := os.ReadDir(r.Dir); len(entries) != 0 {
		t.Fatalf("a file was written with no recording open: %v", entries)
	}
}

func TestAManualRecordingOutranksAutomaticOnes(t *testing.T) {
	r := newRecorder(t)
	manual, err := r.StartManual("t")
	if err != nil {
		t.Fatal(err)
	}
	r.StartAuto("8001", "t")
	if r.Path() != manual {
		t.Fatalf("an automatic recording replaced the manual one: %q", r.Path())
	}
	r.StopAuto(0)
	if r.Path() != manual {
		t.Fatal("stopping automatic recording ended the manual one")
	}
	r.Stop()
}

func TestOldAutomaticRecordingsArePrunedButManualOnesKept(t *testing.T) {
	r := newRecorder(t)
	os.WriteFile(filepath.Join(r.Dir, ManualPrefix+"old.jsonl.gz"), []byte("x"), 0o644)
	for _, name := range []string{"2026-01-01_a", "2026-01-02_b", "2026-01-03_c"} {
		os.WriteFile(filepath.Join(r.Dir, name+".jsonl.gz"), []byte("x"), 0o644)
	}
	r.StartAuto("8004", "t")
	r.StopAuto(2)

	left, _ := filepath.Glob(filepath.Join(r.Dir, "*.jsonl.gz"))
	var names []string
	for _, f := range left {
		names = append(names, filepath.Base(f))
	}
	joined := strings.Join(names, " ")
	if !strings.Contains(joined, ManualPrefix+"old") {
		t.Fatalf("a manual recording was pruned: %v", names)
	}
	if strings.Contains(joined, "2026-01-01_a") || strings.Contains(joined, "2026-01-02_b") {
		t.Fatalf("the oldest automatic recordings should go: %v", names)
	}
	if len(names) != 3 {
		t.Fatalf("want the manual one and the two newest automatic ones, got %v", names)
	}
}

func TestAPracticeMatchIsNamedAsOne(t *testing.T) {
	r := newRecorder(t)
	r.StartAuto("local-7", "t")
	defer r.Stop()
	if !strings.HasSuffix(r.Path(), "_practice.jsonl.gz") {
		t.Fatalf("Path = %q, want it named as practice", r.Path())
	}
}
