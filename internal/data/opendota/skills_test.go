package opendota

import (
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestSkillConsensusFollowsMostPros(t *testing.T) {
	q, w, e, r := "remnant", "vortex", "overload", "ball"
	seqs := [][]string{
		{q, e, q, w, q, r, q, e, e, e, r},
		{q, e, q, w, q, r, q, w, e, e, r},
		{q, w, q, e, q, r, q, e, e, e, r},
		{e, q, q, w, q, r, q, e, e, e, r},
	}
	got := SkillConsensus(seqs)
	want := []string{q, e, q, w, q, r, q, e, e, e, r}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	// The ultimate is never suggested before any pro took it, whatever the votes say.
	early := [][]string{{r, q, q}, {r, q, q}, {q, r, q}}
	if got := SkillConsensus(early); got[0] != r {
		t.Fatalf("most pros opened with the ultimate here: %v", got)
	}
	capped := [][]string{{q, q, q, q, w}, {q, q, q, q, w}}
	if got := SkillConsensus(capped); strings.Count(strings.Join(got, ","), q) != 4 {
		t.Fatalf("no ability goes past the most levels pros gave it: %v", got)
	}
}

func TestLiveSkillBuild(t *testing.T) {
	if os.Getenv("LIVE_OPENDOTA") == "" {
		t.Skip("set LIVE_OPENDOTA=1 to ask the real OpenDota")
	}
	c := New(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.Start(t.Context())
	for _, q := range []struct {
		hero int
		role string
	}{{17, "mid"}, {14, "hard_support"}, {66, "carry"}, {11, "mid"}} {
		var b *SkillBuild
		for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline) && b == nil; time.Sleep(200 * time.Millisecond) {
			b = c.SkillBuildFor(q.hero, q.role)
		}
		if b == nil {
			t.Errorf("hero %d as %s: no skill build", q.hero, q.role)
			continue
		}
		var names []string
		for _, a := range b.Order {
			names = append(names, c.AbilityName(a))
		}
		t.Logf("hero %d as %s: position %d from %d games\n  %s", q.hero, q.role, b.Position, b.Games, strings.Join(names, ", "))
	}
}
