// Package stratz reads hero-against-hero advantage from STRATZ, which counts the week's ranked
// games at each rank. OpenDota's pairs are a few dozen games each, too few to find a counter.
package stratz

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const (
	// STRATZ recomputes the matchups once a week.
	maxAge = 7 * 24 * time.Hour
	retry  = 3 * time.Minute
)

// Edge is how one hero fares against another.
type Edge struct {
	Games int `json:"games"`
	// Pct is the queried hero's advantage over the other, in percentage points.
	Pct float64 `json:"pct"`
}

type key struct {
	hero    int
	bracket string
}

type Client struct {
	url   string
	http  *http.Client
	dir   string
	log   *slog.Logger
	token func() string

	mu      sync.Mutex
	ctx     context.Context
	got     map[key]map[int]Edge
	pending map[key]bool
	failed  map[key]time.Time
	fetches sync.WaitGroup
}

// New keeps its cache under cacheDir; token returns the player's STRATZ token, "" for none.
func New(cacheDir string, token func() string, log *slog.Logger) *Client {
	return &Client{url: "https://api.stratz.com/graphql", http: &http.Client{Timeout: 30 * time.Second},
		dir: filepath.Join(cacheDir, "stratz"), log: log, token: token, ctx: context.Background()}
}

// Start ties background fetches to ctx.
func (c *Client) Start(ctx context.Context) {
	c.mu.Lock()
	c.ctx = ctx
	c.mu.Unlock()
}

func (c *Client) SetURL(u string) { c.url = u }

// Wait blocks until the fetches started so far are done.
func (c *Client) Wait() { c.fetches.Wait() }

// Forget drops what is held, for a new token to be tried at once.
func (c *Client) Forget() {
	c.mu.Lock()
	c.got, c.pending, c.failed = nil, nil, nil
	c.mu.Unlock()
}

// Bracket is STRATZ's name for the ranks around an OpenDota rank tier.
func Bracket(rankTier int) string {
	switch rankTier / 10 {
	case 1, 2:
		return "HERALD_GUARDIAN"
	case 3, 4:
		return "CRUSADER_ARCHON"
	case 5, 6:
		return "LEGEND_ANCIENT"
	case 7, 8:
		return "DIVINE_IMMORTAL"
	}
	return "ALL"
}

// Against is heroID's advantage over every other hero at the player's rank, nil until it has
// loaded or when there is no token. The first call only starts the fetch.
func (c *Client) Against(heroID, rankTier int) map[int]Edge {
	if heroID <= 0 || c.token() == "" {
		return nil
	}
	k := key{heroID, Bracket(rankTier)}
	c.mu.Lock()
	defer c.mu.Unlock()
	if m, ok := c.got[k]; ok {
		return m
	}
	if c.pending[k] || time.Since(c.failed[k]) < retry {
		return nil
	}
	if c.pending == nil {
		c.pending = map[key]bool{}
	}
	c.pending[k] = true
	ctx := c.ctx
	c.fetches.Add(1)
	go func() {
		defer c.fetches.Done()
		m, err := c.load(ctx, k)
		c.mu.Lock()
		defer c.mu.Unlock()
		delete(c.pending, k)
		if err != nil {
			c.log.Warn("no STRATZ matchups for hero; will retry", "hero_id", k.hero, "bracket", k.bracket, "err", err)
			if c.failed == nil {
				c.failed = map[key]time.Time{}
			}
			c.failed[k] = time.Now()
			return
		}
		if c.got == nil {
			c.got = map[key]map[int]Edge{}
		}
		c.got[k] = m
		c.log.Info("STRATZ matchups loaded", "hero_id", k.hero, "bracket", k.bracket, "against", len(m))
	}()
	return nil
}

func (c *Client) load(ctx context.Context, k key) (map[int]Edge, error) {
	path := filepath.Join(c.dir, k.bracket, strconv.Itoa(k.hero)+".json")
	if st, err := os.Stat(path); err == nil && time.Since(st.ModTime()) < maxAge {
		if data, err := os.ReadFile(path); err == nil {
			var m map[int]Edge
			if json.Unmarshal(data, &m) == nil && len(m) > 0 {
				return m, nil
			}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	m, err := c.fetch(ctx, k)
	if err != nil {
		return nil, err
	}
	if data, err := json.Marshal(m); err == nil {
		if os.MkdirAll(filepath.Dir(path), 0o755) == nil {
			_ = os.WriteFile(path, data, 0o644)
		}
	}
	return m, nil
}

const query = `query($hero: Short!, $brackets: [RankBracketBasicEnum]) {
  heroStats { heroVsHeroMatchup(heroId: $hero, bracketBasicIds: $brackets, take: 200) {
    advantage { heroId vs { heroId2 matchCount synergy } } } }
}`

var errToken = errors.New("STRATZ refused the token; paste a new one on the settings page")

func (c *Client) fetch(ctx context.Context, k key) (map[int]Edge, error) {
	var brackets []string
	if k.bracket != "ALL" {
		brackets = []string{k.bracket}
	}
	body, err := json.Marshal(map[string]any{"query": query,
		"variables": map[string]any{"hero": k.hero, "brackets": brackets}})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token())
	// STRATZ turns away requests without this exact agent.
	req.Header.Set("User-Agent", "STRATZ_API")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, errToken
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("STRATZ: %s", resp.Status)
	}
	var out struct {
		Data struct {
			HeroStats struct {
				HeroVsHeroMatchup struct {
					Advantage []struct {
						HeroID int `json:"heroId"`
						Vs     []struct {
							HeroID2    int     `json:"heroId2"`
							MatchCount int     `json:"matchCount"`
							Synergy    float64 `json:"synergy"`
						} `json:"vs"`
					} `json:"advantage"`
				} `json:"heroVsHeroMatchup"`
			} `json:"heroStats"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Errors) > 0 {
		return nil, fmt.Errorf("STRATZ: %s", out.Errors[0].Message)
	}
	m := map[int]Edge{}
	for _, a := range out.Data.HeroStats.HeroVsHeroMatchup.Advantage {
		if a.HeroID != k.hero {
			continue
		}
		for _, v := range a.Vs {
			if v.MatchCount > 0 {
				m[v.HeroID2] = Edge{Games: v.MatchCount, Pct: v.Synergy}
			}
		}
	}
	if len(m) == 0 {
		return nil, errors.New("STRATZ sent no matchups")
	}
	return m, nil
}
