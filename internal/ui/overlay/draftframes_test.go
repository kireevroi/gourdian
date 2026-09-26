package overlay

import (
	"bufio"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gourdian/internal/ui/hud"
	"gourdian/internal/ui/screen"
)

func readDraft(t *testing.T, payload hud.Payload, root string) {
	t.Helper()
	heroes := []int{1, 8, 14, 26, 35}
	table := screen.Table{}
	for _, id := range heroes {
		table.Add(id, portrait(id))
	}
	var slots [2 * screen.Slots]int
	copy(slots[screen.Slots:], heroes)
	size := image.Rect(0, 0, 1920, 1080)
	shot := fakeScreen(size, slots)
	// Stop once the draft has settled, not after a fixed time: under -race a frame can take
	// longer than the whole window.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/draft" {
			cancel()
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	m := newModel(time.Now(), false)
	m.apply("snapshot", []byte(`{"connected":true,"team":"radiant"}`), time.Now())
	m.apply("hud", mustJSON(payload), time.Now())
	a := newAPI(srv.URL)
	watchWith(ctx, m, &a, table, eyes{
		size: func() (image.Rectangle, error) { return size, nil },
		grab: func(image.Rectangle) (image.Image, error) { return shot, nil },
	}, 10*time.Millisecond, &frameKeeper{root: root}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestDraftFramesAreKeptWhenAsked(t *testing.T) {
	root := t.TempDir()
	readDraft(t, hud.Payload{Draft: true, KeepFrames: true}, root)

	drafts, _ := os.ReadDir(root)
	if len(drafts) != 1 {
		t.Fatalf("got %d draft folders, want 1", len(drafts))
	}
	dir := filepath.Join(root, drafts[0].Name())
	f, err := os.Open(filepath.Join(dir, "reading.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var last frameNote
	lines := 0
	for sc := bufio.NewScanner(f); sc.Scan(); lines++ {
		if err := json.Unmarshal(sc.Bytes(), &last); err != nil {
			t.Fatal(err)
		}
	}
	if lines == 0 {
		t.Fatal("no frames noted")
	}
	if last.Screen != "1920x1080" || last.Seen[screen.Slots] != 1 || last.Settled[screen.Slots] != 1 {
		t.Errorf("the note doesn't say what was read: %+v", last)
	}
	pf, err := os.Open(filepath.Join(dir, last.Frame))
	if err != nil {
		t.Fatal(err)
	}
	defer pf.Close()
	img, err := png.Decode(pf)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 1920 || b.Dy() != 180 {
		t.Errorf("saved %v, want the top sixth of the screen", b)
	}
}

func TestDraftFramesAreNotKeptUnlessAsked(t *testing.T) {
	root := t.TempDir()
	readDraft(t, hud.Payload{Draft: true}, root)
	if drafts, _ := os.ReadDir(root); len(drafts) != 0 {
		t.Errorf("saved %d drafts without being asked", len(drafts))
	}
}

func TestOnlyTheNewestDraftsAreKept(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"2026-01-01_00-00-01", "2026-01-01_00-00-02", "2026-01-01_00-00-03",
		"2026-01-01_00-00-04", "2026-01-01_00-00-05", "2026-01-01_00-00-06"} {
		os.Mkdir(filepath.Join(root, name), 0o755)
	}
	k := frameKeeper{root: root}
	shot := image.NewRGBA(image.Rect(0, 0, 640, 360))
	if err := k.save(shot, shot.Bounds(), frameNote{Bar: screen.Predict(shot.Bounds())}); err != nil {
		t.Fatal(err)
	}
	drafts, _ := os.ReadDir(root)
	if len(drafts) != keepDrafts {
		t.Fatalf("kept %d drafts, want %d", len(drafts), keepDrafts)
	}
	if drafts[0].Name() != "2026-01-01_00-00-03" {
		t.Errorf("the oldest kept is %s, want the two oldest gone", drafts[0].Name())
	}
}
