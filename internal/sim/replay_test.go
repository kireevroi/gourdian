package sim

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadRecordingStopsAtCutOffLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "match.jsonl.gz")
	f, _ := os.Create(path)
	gz := gzip.NewWriter(f)
	for _, line := range []string{`{"t":0,"payload":{"map":{}}}`, `{"t":250,"payload":{"map":{}}}`} {
		gz.Write([]byte(line + "\n"))
	}
	gz.Write([]byte(`{"t":500,"payl`))
	gz.Flush() // never closed, like a recording still being written
	f.Close()

	var times []int64
	err := ReadRecording(path, func(ms int64, _ map[string]json.RawMessage) error {
		times = append(times, ms)
		return nil
	})
	if err != nil || len(times) != 2 || times[1] != 250 {
		t.Fatalf("times %v, err %v", times, err)
	}
}

func TestReadRecordingRejectsBadLineInTheMiddle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "match.jsonl")
	os.WriteFile(path, []byte("{\"t\":0,\"payload\":{}}\nnot json\n{\"t\":2,\"payload\":{}}\n"), 0o644)
	if err := ReadRecording(path, func(int64, map[string]json.RawMessage) error { return nil }); err == nil {
		t.Fatal("a broken line followed by more updates should be an error")
	}
}
