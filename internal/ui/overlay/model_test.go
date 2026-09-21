package overlay

import (
	"encoding/json"
	"testing"
	"time"

	"gourdian/internal/coaching/coach"
	"gourdian/internal/ui/hud"
)

func apply(t *testing.T, m *model, event string, v any, now time.Time) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if !m.apply(event, data, now) {
		t.Fatalf("event %s not applied", event)
	}
}

func TestViewHiddenOutsideMatchAfterBanner(t *testing.T) {
	start := time.Now()
	m := newModel(start, false)
	if v := m.view(start, false); v.Banner == "" || len(v.Rows) != 1 {
		t.Fatalf("offline view should explain the trainer isn't reachable: %+v", v)
	}
	apply(t, m, "snapshot", coach.Snapshot{Connected: true}, start)
	if v := m.view(start.Add(bannerFor+time.Second), false); !v.Empty() {
		t.Fatalf("menus should hide the overlay: %+v", v)
	}
}

func TestQuietModelAnnouncesAtFirstMatch(t *testing.T) {
	start := time.Now()
	m := newModel(start, true)
	if v := m.view(start, false); !v.Empty() {
		t.Fatalf("started at sign-in, the HUD should stay hidden: %+v", v)
	}
	later := start.Add(time.Hour)
	apply(t, m, "snapshot", coach.Snapshot{Connected: true, InMatch: true, Clock: -60}, later)
	if v := m.view(later.Add(time.Second), false); v.Banner == "" {
		t.Fatal("the hotkey banner should show when the first match starts")
	}
}

func TestViewUsesServerHUDAndSampleWhileEditing(t *testing.T) {
	now := time.Now()
	m := newModel(now.Add(-time.Hour), false)
	apply(t, m, "snapshot", coach.Snapshot{Connected: true}, now)
	payload := hud.Payload{Sample: hud.View{Rows: []hud.Line{{Text: "0:18  Power rune", Kind: hud.KindWarn}}}}
	apply(t, m, "hud", payload, now)
	if v := m.view(now, false); !v.Empty() {
		t.Fatalf("nothing live to show: %+v", v)
	}
	if v := m.view(now, true); len(v.Rows) != 1 || v.Rows[0].Text != "0:18  Power rune" {
		t.Fatalf("editing outside a match shows the sample: %+v", v)
	}
	payload.Live = hud.View{Alert: &hud.Line{Text: "Buy a TP", Kind: hud.KindWarn}}
	apply(t, m, "hud", payload, now)
	if v := m.view(now, true); v.Alert == nil || len(v.Rows) != 0 {
		t.Fatalf("live content wins while editing in a match: %+v", v)
	}
}

func TestAskingPositionUntilLaneDetection(t *testing.T) {
	now := time.Now()
	m := newModel(now, false)
	apply(t, m, "snapshot", coach.Snapshot{Connected: true, InMatch: true, Clock: -30}, now)
	if !m.askingPosition() {
		t.Fatal("position keys are active before the horn")
	}
	apply(t, m, "snapshot", coach.Snapshot{Connected: true, InMatch: true, Clock: hud.PositionUntil}, now)
	if m.askingPosition() {
		t.Fatal("position keys stop at 2:30")
	}
}

// The position keys are live while the player is choosing a hero. The pick advice is worked
// out for a position, so that is the moment to be able to say which one; they used to come
// alive only once a match had started.
func TestPositionKeysAreLiveWhileChoosing(t *testing.T) {
	m := newModel(time.Now(), false)
	m.apply("snapshot", []byte(`{"connected":true,"in_match":false}`), time.Now())
	if m.askingPosition() {
		t.Error("the keys are live with nothing happening")
	}
	m.apply("hud", []byte(`{"choosing":true}`), time.Now())
	if !m.askingPosition() {
		t.Error("the keys are dead while the player is choosing a hero")
	}
	// Reading the screen is a different thing, and off by default: the keys can't depend on it.
	m.apply("hud", []byte(`{"choosing":true,"draft":false}`), time.Now())
	if !m.askingPosition() {
		t.Error("the keys went dead because the screen isn't being read")
	}
	// Once the hero is picked and the game is on, the old rule takes over again.
	m.apply("hud", []byte(`{"choosing":false}`), time.Now())
	if m.askingPosition() {
		t.Error("the keys stayed live after the pick, with no match running")
	}
}
