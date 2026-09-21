package overlay

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	xdraw "golang.org/x/image/draw"

	"gourdian/internal/ui/hud"
	"gourdian/internal/ui/screen"
)

// portrait invents a hero's art, distinct enough to be told from the others.
func portrait(heroID int) image.Image {
	rng := rand.New(rand.NewPCG(uint64(heroID), 7))
	seed := image.NewRGBA(image.Rect(0, 0, 12, 10))
	for y := range 10 {
		for x := range 12 {
			seed.Set(x, y, color.RGBA{uint8(rng.IntN(256)), uint8(rng.IntN(256)), uint8(rng.IntN(256)), 255})
		}
	}
	big := image.NewRGBA(image.Rect(0, 0, 128, 72))
	xdraw.CatmullRom.Scale(big, big.Bounds(), seed, seed.Bounds(), xdraw.Src, nil)
	return big
}

// fakeScreen paints a bar of the given heroes where the trainer expects one.
func fakeScreen(size image.Rectangle, slots [2 * screen.Slots]int) image.Image {
	shot := image.NewRGBA(size)
	draw.Draw(shot, size, image.NewUniform(color.RGBA{18, 20, 26, 255}), image.Point{}, draw.Src)
	bar := screen.Predict(size)
	for i, id := range slots {
		if id == 0 {
			continue
		}
		cell := bar.Cell(i)
		xdraw.ApproxBiLinear.Scale(shot, cell, portrait(id), portrait(id).Bounds(), xdraw.Src, nil)
	}
	return shot
}

// The loop reads the screen while the trainer says a draft is on, tells it what settled, and
// stops the moment the draft is over.
func TestTheDraftIsReadAndReported(t *testing.T) {
	heroes := []int{1, 8, 14, 26, 35, 44, 53, 74, 86, 101}
	table := screen.Table{}
	for _, id := range heroes {
		table.Add(id, portrait(id))
	}
	var slots [2 * screen.Slots]int
	copy(slots[:], heroes)
	size := image.Rect(0, 0, 1920, 1080)
	shot := fakeScreen(size, slots)

	var mu sync.Mutex
	var posted []struct{ Ours, Theirs []int }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Ours, Theirs []int }
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		posted = append(posted, body)
		mu.Unlock()
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	m := newModel(time.Now(), false)
	m.apply("snapshot", []byte(`{"connected":true,"team":"radiant"}`), time.Now())
	m.apply("hud", mustJSON(hud.Payload{Draft: true}), time.Now())
	a := newAPI(srv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchWith(ctx, m, &a, table, eyes{
			size: func() (image.Rectangle, error) { return size, nil },
			grab: func(image.Rectangle) (image.Image, error) { return shot, nil },
		}, 10*time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()

	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		n := len(posted)
		mu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("the draft was never reported")
		case <-time.After(10 * time.Millisecond):
		}
	}
	mu.Lock()
	last := posted[len(posted)-1]
	mu.Unlock()
	if len(last.Theirs) != screen.Slots {
		t.Errorf("reported %d enemies, want %d: %+v", len(last.Theirs), screen.Slots, last)
	}
	if len(last.Ours) != screen.Slots {
		t.Errorf("reported %d of the player's own side: %+v", len(last.Ours), last)
	}
	cancel()
	<-done
}

// Nothing is read when the trainer doesn't say a draft is on, which is nearly all the time.
func TestNothingIsReadOutsideADraft(t *testing.T) {
	looked := 0
	m := newModel(time.Now(), false)
	m.apply("snapshot", []byte(`{"connected":true}`), time.Now())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("the trainer was told about a draft that wasn't happening")
	}))
	defer srv.Close()
	a := newAPI(srv.URL)
	table := screen.Table{}
	table.Add(1, portrait(1))

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	watchWith(ctx, m, &a, table, eyes{
		size: func() (image.Rectangle, error) { looked++; return image.Rect(0, 0, 1920, 1080), nil },
		grab: func(image.Rectangle) (image.Image, error) { looked++; return nil, nil },
	}, 10*time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if looked != 0 {
		t.Errorf("the screen was looked at %d times outside a draft", looked)
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// The trainer drops a reading that stops being renewed, so the loop has to keep telling it
// what it can see even when nothing new has settled. Most of a draft is quiet: the first few
// heroes go in and then nothing happens for a while.
func TestTheDraftIsReportedEvenWhenNothingChanges(t *testing.T) {
	heroes := []int{1, 8}
	table := screen.Table{}
	for _, id := range heroes {
		table.Add(id, portrait(id))
	}
	var slots [2 * screen.Slots]int
	slots[screen.Slots], slots[screen.Slots+1] = heroes[0], heroes[1]
	size := image.Rect(0, 0, 1920, 1080)
	shot := fakeScreen(size, slots)

	var mu sync.Mutex
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		posts++
		mu.Unlock()
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	m := newModel(time.Now(), false)
	m.apply("snapshot", []byte(`{"connected":true,"team":"radiant"}`), time.Now())
	m.apply("hud", mustJSON(hud.Payload{Draft: true}), time.Now())
	a := newAPI(srv.URL)

	// Long enough that the race detector's slowdown can't make an unchanging draft look like
	// a quiet one.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		for ctx.Err() == nil {
			mu.Lock()
			enough := posts >= 3
			mu.Unlock()
			if enough {
				cancel()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	watchWith(ctx, m, &a, table, eyes{
		size: func() (image.Rectangle, error) { return size, nil },
		grab: func(image.Rectangle) (image.Image, error) { return shot, nil },
	}, 20*time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)))

	mu.Lock()
	defer mu.Unlock()
	// The two heroes settle once and never change; the trainer must still hear about them.
	if posts < 3 {
		t.Errorf("told the trainer %d times over an unchanging draft, want it kept up", posts)
	}
}
