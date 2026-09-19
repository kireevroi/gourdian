package matchdata

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"gourdian/internal/config"
	"gourdian/internal/dotadata"
	"gourdian/internal/stats"
)

const (
	matchID   = "9000000001"
	accountID = "100000001"
)

func newService(t *testing.T) Service {
	t.Helper()
	parsed, err := os.ReadFile("../dotadata/testdata/match_parsed.json")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/matches/" + matchID:
			w.Write(parsed)
		case "/players/" + accountID + "/matches":
			w.Write([]byte(`[{"match_id":9000000001,"player_slot":0,"hero_id":44,"start_time":1789631145,"duration":499}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	cache := t.TempDir()
	os.WriteFile(filepath.Join(cache, "items.json"), []byte(`{
		"bfury": {"id": 145, "dname": "Battle Fury", "cost": 3900},
		"pers": {"id": 69, "dname": "Perseverance", "cost": 1400}}`), 0o644)
	os.WriteFile(filepath.Join(cache, "heroes.json"), []byte(`{
		"44": {"id": 44, "localized_name": "Phantom Assassin"},
		"98": {"id": 98, "localized_name": "Timbersaw"},
		"15": {"id": 15, "localized_name": "Razor"}}`), 0o644)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	data := dotadata.New(cache, log)
	data.SetBaseURL(srv.URL)
	data.Start(t.Context())
	data.WaitReady(t.Context())
	return Service{Data: data, Stats: openStore(t), Log: log}
}

func TestEnrichStoresParsedData(t *testing.T) {
	s := newService(t)
	live := stats.MatchSummary{MatchID: matchID, HeroID: 44, Hero: "Phantom Assassin", Source: stats.SourceLive,
		LastHitsAt: map[string]int{"5:00": 12}}
	if err := s.Stats.AppendMatch(live); err != nil {
		t.Fatal(err)
	}
	d, err := s.Enrich(t.Context(), live, accountID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Parsed || d.LaneRoleName != "safe lane" || !slices.Equal(d.LaneOpponents, []string{"Timbersaw", "Razor"}) {
		t.Fatalf("detail = %+v", d)
	}
	if len(d.CoreItems) != 1 || d.CoreItems[0] != (ItemTime{"Battle Fury", 480}) {
		t.Fatalf("core items = %+v (Perseverance is below the core cost)", d.CoreItems)
	}
	rows, _ := s.Stats.Matches()
	m := rows[0]
	if !m.Parsed || m.NetWorth != 4412 || m.ObsPlaced != 2 || len(m.EnemyHeroes) != 5 {
		t.Fatalf("stored match = %+v", m)
	}
	if m.LastHitsAt["5:00"] != 12 {
		t.Fatal("the live 5:00 sample must win over OpenDota's")
	}
	items, _ := s.Stats.Items()
	if len(items) != 1 || items[0].Item != "bfury" || items[0].Time != 480 {
		t.Fatalf("items = %+v", items)
	}
}

func TestImportAddsMissingMatchesOnce(t *testing.T) {
	s := newService(t)
	var last ImportProgress
	added, err := s.Import(t.Context(), accountID, 5, func(p ImportProgress) { last = p })
	if err != nil || added != 1 || last != (ImportProgress{Done: 1, Total: 1, Added: 1}) {
		t.Fatalf("import = %d, %v, progress %+v", added, err, last)
	}
	rows, _ := s.Stats.Matches()
	m := rows[0]
	if m.Source != stats.SourceOpenDota || m.Result != "loss" || m.Role != config.RoleHardSupport || m.LastHitsAt["5:00"] != 11 || !m.Parsed {
		t.Fatalf("imported match = %+v", m)
	}
	if added, err := s.Import(t.Context(), accountID, 5, nil); err != nil || added != 0 {
		t.Fatalf("second import added %d, %v", added, err)
	}
	if _, err := s.Import(t.Context(), "", 5, nil); err == nil {
		t.Fatal("import without an account id should fail")
	}
}

func TestRoleFor(t *testing.T) {
	cases := []struct {
		d    dotadata.PlayerDetail
		want string
	}{
		{dotadata.PlayerDetail{LaneRole: dotadata.LaneMid, NetWorthRank: 4}, config.RoleMid},
		{dotadata.PlayerDetail{LaneRole: dotadata.LaneSafe, NetWorthRank: 1}, config.RoleCarry},
		{dotadata.PlayerDetail{LaneRole: dotadata.LaneSafe, NetWorthRank: 5}, config.RoleHardSupport},
		{dotadata.PlayerDetail{LaneRole: dotadata.LaneOff, NetWorthRank: 2}, config.RoleOfflane},
		{dotadata.PlayerDetail{LaneRole: dotadata.LaneOff, NetWorthRank: 4}, config.RoleSoftSupport},
		{dotadata.PlayerDetail{LaneRole: dotadata.LaneJungle, Roaming: true, NetWorthRank: 5}, config.RoleSoftSupport},
	}
	for _, c := range cases {
		if got := roleFor(c.d); got != c.want {
			t.Errorf("roleFor(%+v) = %s, want %s", c.d, got, c.want)
		}
	}
}

func TestItemTimingsSkipCombinedComponents(t *testing.T) {
	items := map[string]dotadata.ItemInfo{
		"sange":           {Cost: 2050},
		"yasha":           {Cost: 2050},
		"sange_and_yasha": {Cost: 4100, Components: []string{"sange", "yasha"}},
		"black_king_bar":  {Cost: 4050},
	}
	d := dotadata.PlayerDetail{ItemTimes: map[string]int{"sange": 900, "yasha": 1000, "sange_and_yasha": 1010, "black_king_bar": 700}}
	var got []string
	for _, it := range itemTimings(matchID, "Juggernaut", d, items) {
		got = append(got, it.Item)
	}
	if !slices.Equal(got, []string{"black_king_bar", "sange_and_yasha"}) {
		t.Fatalf("items = %v", got)
	}
}

func openStore(t *testing.T) *stats.Store {
	t.Helper()
	s, err := stats.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
