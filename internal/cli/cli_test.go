package cli

import (
	"io"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestReorderFlagsLetsTheFileComeFirst(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []string
		want []string
	}{
		{"file then flag", []string{"match.jsonl", "-speed", "10"}, []string{"-speed", "10", "match.jsonl"}},
		{"flag then file", []string{"-speed", "10", "match.jsonl"}, []string{"-speed", "10", "match.jsonl"}},
		{"flag with equals", []string{"match.jsonl", "-speed=10"}, []string{"-speed=10", "match.jsonl"}},
		{"file only", []string{"match.jsonl"}, []string{"match.jsonl"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := reorderFlags(tc.in); !slices.Equal(got, tc.want) {
				t.Fatalf("reorderFlags(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestHelpIsPrintedAndSucceeds(t *testing.T) {
	out := capture(t, &os.Stdout, func() { mainIs(t, 0, "help") })
	if !strings.Contains(out, "gourdian: live Dota 2 coaching") {
		t.Fatalf("help didn't print the usage, got %q", out)
	}
}

func TestAnUnknownCommandSaysSoAndFails(t *testing.T) {
	out := capture(t, &os.Stderr, func() { mainIs(t, 2, "teleport") })
	if !strings.Contains(out, "Usage:") {
		t.Fatalf("an unknown command should print the usage, got %q", out)
	}
}

// TestEveryListedCommandIsDispatched keeps the usage text and the switch in step: a command the
// help promises but Main doesn't know would exit 2 instead of running.
func TestEveryListedCommandIsDispatched(t *testing.T) {
	for _, line := range strings.Split(usage, "\n") {
		name, ok := strings.CutPrefix(strings.TrimSpace(line), "gourdian ")
		if !ok {
			continue
		}
		name, _, _ = strings.Cut(name, " ")
		if name = strings.Trim(name, "[]"); name == "" {
			continue
		}
		if !slices.Contains(commands(), name) {
			t.Errorf("usage lists %q, but Main has no case for it", name)
		}
	}
}

// commands is the list Main switches on, kept beside it so the test above can check the usage.
func commands() []string {
	return []string{"run", "install", "uninstall", "simulate", "overlay", "stats", "doctor",
		"setup", "quit", "version", "replay", "mmr", "import", "help"}
}

func mainIs(t *testing.T, want int, args ...string) {
	t.Helper()
	if got := Main(args); got != want {
		t.Fatalf("Main(%q) = %d, want %d", args, got, want)
	}
}

// capture swaps a standard stream for a pipe while fn runs and returns what was written to it.
func capture(t *testing.T, stream **os.File, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := *stream
	*stream = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	w.Close()
	*stream = saved
	return <-done
}
