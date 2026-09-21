package opendota

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestBuildFollowsThePosition(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch sql := r.URL.Query().Get("sql"); {
		case r.URL.Path == "/constants/items":
			io.WriteString(w, `{"blink":{"id":1,"dname":"Blink Dagger","cost":2250},"aether_lens":{"id":232,"dname":"Aether Lens","cost":2275},
				"ultimate_scepter":{"id":108,"dname":"Aghanim's Scepter","cost":4200}}`)
		case r.URL.Path == "/constants/heroes":
			io.WriteString(w, `{"14":{"id":14,"localized_name":"Pudge"},"15":{"id":15,"localized_name":"Razor"}}`)
		case r.URL.Path == "/heroes/14/itemPopularity" || r.URL.Path == "/heroes/15/itemPopularity":
			io.WriteString(w, `{"mid_game_items":{"232":30,"1":25,"108":10}}`)
		case r.URL.Path == "/explorer" && strings.Contains(sql, "hero_id = 14") && strings.Contains(sql, "AND won AND") && strings.Contains(sql, "END = 2)"):
			io.WriteString(w, `{"rows":[{"phase":"mid_game_items","item":"ultimate_scepter","games":100,"total":122},
				{"phase":"mid_game_items","item":"blink","games":43,"total":122}],"err":null}`)
		case r.URL.Path == "/explorer" && strings.Contains(sql, "hero_id = 14") && strings.Contains(sql, "AND true AND") && strings.Contains(sql, "END = 1)"):
			io.WriteString(w, `{"rows":[{"phase":"mid_game_items","item":"ultimate_scepter","games":20,"total":30}],"err":null}`)
		case r.URL.Path == "/explorer" && strings.Contains(sql, "hero_id = 14") && strings.Contains(sql, "AND won AND") && strings.Contains(sql, "(0 = 0"):
			io.WriteString(w, `{"rows":[{"phase":"mid_game_items","item":"blink","games":30,"total":40}],"err":null}`)
		case r.URL.Path == "/explorer":
			io.WriteString(w, `{"rows":[{"phase":"mid_game_items","item":"blink","games":2,"total":3}],"err":null}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := New(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(c.Wait)
	c.SetBaseURL(srv.URL) // a fake OpenDota has no rate limit to keep to
	c.Start(t.Context())

	settle := func(hero int, role string) *Build {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if b := c.BuildFor(hero, role); b != nil && !b.Loading {
				return b
			}
		}
		t.Fatalf("the %s build never settled", role)
		return nil
	}
	names := func(b *Build) []string {
		var out []string
		for _, it := range b.Items {
			out = append(out, it.Name)
		}
		return out
	}
	mid := settle(14, "mid")
	if mid.Position != 2 || mid.Games != 122 || !mid.Won || slices.Contains(names(mid), "aether_lens") || !slices.Contains(names(mid), "ultimate_scepter") {
		t.Fatalf("mid Pudge should get the mid build: %+v", mid)
	}
	if carry := settle(14, "carry"); carry.Position != 1 || carry.Games != 30 || carry.Won {
		t.Fatalf("with 3 won pro games as carry, all 30 carry games stand: %+v", carry)
	}
	if support := settle(14, "hard_support"); support.Position != 0 || support.Games != 40 || !support.Won || slices.Contains(names(support), "aether_lens") {
		t.Fatalf("with few pro games as a 5, the won games in every position stand: %+v", support)
	}
	if razor := settle(15, "carry"); razor.Won || !slices.Contains(names(razor), "aether_lens") {
		t.Fatalf("with too few won pro games at all, OpenDota's recent pro build stands: %+v", razor)
	}
}

func TestLivePositionBuild(t *testing.T) {
	if os.Getenv("LIVE_OPENDOTA") == "" {
		t.Skip("set LIVE_OPENDOTA=1 to ask the real OpenDota")
	}
	c := New(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(c.Wait)
	c.Start(t.Context())
	for _, q := range []struct {
		hero int
		role string
	}{{14, "mid"}, {14, "hard_support"}, {66, "carry"}, {17, "mid"}} {
		var b *Build
		for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
			if b = c.BuildFor(q.hero, q.role); b != nil && !b.Loading {
				break
			}
		}
		var items []string
		for _, it := range b.Items {
			items = append(items, it.Phase+":"+it.DName)
		}
		t.Logf("hero %d as %s: position %d from %d games (won only: %v)\n  %s", q.hero, q.role, b.Position, b.Games, b.Won, strings.Join(items, ", "))
	}
}
