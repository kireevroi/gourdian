// Package dotadata fetches hero, item and build data from OpenDota with a disk cache.
package dotadata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"time"
)

const (
	constantsMaxAge = 7 * 24 * time.Hour
	buildMaxAge     = 3 * 24 * time.Hour
	buildRetry      = 3 * time.Minute
)

type Client struct {
	limit    *limiter        // every call to OpenDota waits its turn here
	ctx      context.Context // from Start: background fetches end with it
	base     string
	http     *http.Client
	cacheDir string
	log      *slog.Logger

	ready chan struct{}

	mu          sync.RWMutex
	items       map[string]ItemInfo
	heroes      map[int]HeroInfo
	builds      map[int]*Build
	failedAt    map[int]time.Time
	pending     map[int]bool
	rankTiers   map[string]int
	rankPending map[string]bool

	posBuilds  map[positionKey]*Build
	posPending map[positionKey]bool
	posFailed  map[positionKey]time.Time

	skillBuilds   map[positionKey]*SkillBuild
	skillPending  map[positionKey]bool
	skillFailed   map[positionKey]time.Time
	abilityIDs    map[string]string
	heroAbilities map[string][]string
	abilities     map[string]abilityInfo

	timings        map[timingsKey][]ItemTiming
	timingsPending map[timingsKey]bool
	timingsFailed  map[timingsKey]time.Time
}

func New(cacheDir string, log *slog.Logger) *Client {
	return &Client{
		base:     "https://api.opendota.com/api",
		http:     &http.Client{Timeout: 30 * time.Second},
		cacheDir: cacheDir,
		log:      log,
		ready:    make(chan struct{}),
		limit:    newLimiter(),
		builds:   map[int]*Build{},
		failedAt: map[int]time.Time{},
		pending:  map[int]bool{},

		posBuilds:  map[positionKey]*Build{},
		posPending: map[positionKey]bool{},
		posFailed:  map[positionKey]time.Time{},

		skillBuilds:  map[positionKey]*SkillBuild{},
		skillPending: map[positionKey]bool{},
		skillFailed:  map[positionKey]time.Time{},

		rankTiers:   map[string]int{},
		rankPending: map[string]bool{},

		timings:        map[timingsKey][]ItemTiming{},
		timingsPending: map[timingsKey]bool{},
		timingsFailed:  map[timingsKey]time.Time{},
	}
}

func (c *Client) Start(ctx context.Context) {
	c.mu.Lock()
	c.ctx = ctx
	c.mu.Unlock()
	go func() {
		defer close(c.ready)
		var items map[string]ItemInfo
		if err := c.getJSON(ctx, "/constants/items", "items.json", constantsMaxAge, &items); err != nil {
			c.log.Warn("item data unavailable; build suggestions disabled", "err", err)
		}
		var heroes map[string]HeroInfo
		if err := c.getJSON(ctx, "/constants/heroes", "heroes.json", constantsMaxAge, &heroes); err != nil {
			c.log.Warn("hero data unavailable", "err", err)
		}
		byID := make(map[int]HeroInfo, len(heroes))
		for _, h := range heroes {
			byID[h.ID] = h
		}
		c.mu.Lock()
		c.items, c.heroes = items, byID
		c.mu.Unlock()
		c.log.Info("dota data loaded", "items", len(items), "heroes", len(byID))
	}()
}

// life is the context passed to Start, which background fetches stop with, or Background
// before Start.
func (c *Client) life() context.Context {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.ctx == nil {
		return context.Background()
	}
	return c.ctx
}

// Items returns the shared item table (nil until loaded); callers must not modify it.
func (c *Client) Items() map[string]ItemInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.items
}

// Heroes lists every hero that has loaded.
func (c *Client) Heroes() []HeroInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]HeroInfo, 0, len(c.heroes))
	for _, h := range c.heroes {
		out = append(out, h)
	}
	return out
}

func (c *Client) Hero(id int) (HeroInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	h, ok := c.heroes[id]
	return h, ok
}

// BuildFor is what pros bought on the hero in the player's position, from the games they won
// when there are enough of them, else from all their games there. When pros rarely play the
// hero there, it's their won games in every position, and without enough of those OpenDota's
// recent pro games. It's nil while nothing has loaded.
func (c *Client) BuildFor(heroID int, role string) *Build {
	b := c.proBuild(heroID, 0, true, c.heroBuild(heroID))
	if pos := Positions[role]; pos != 0 {
		b = c.proBuild(heroID, pos, true, c.proBuild(heroID, pos, false, b))
	}
	return b
}

func (c *Client) heroBuild(heroID int) *Build {
	c.mu.Lock()
	defer c.mu.Unlock()
	if b, ok := c.builds[heroID]; ok || c.pending[heroID] || heroID <= 0 || time.Since(c.failedAt[heroID]) < buildRetry {
		return b
	}
	c.pending[heroID] = true
	go c.fetchBuild(heroID)
	return nil
}

func (c *Client) fetchBuild(heroID int) {
	<-c.ready
	ctx, cancel := context.WithTimeout(c.life(), time.Minute)
	defer cancel()
	var pop Popularity
	path := fmt.Sprintf("/heroes/%d/itemPopularity", heroID)
	err := c.getJSON(ctx, path, filepath.Join("builds", strconv.Itoa(heroID)+".json"), buildMaxAge, &pop)
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pending, heroID)
	if err != nil || c.items == nil {
		c.log.Warn("no item build for hero; will retry", "hero_id", heroID, "err", err)
		c.failedAt[heroID] = time.Now()
		return
	}
	c.builds[heroID] = BuildFromPopularity(heroID, pop, c.items)
	c.log.Info("item build loaded", "hero_id", heroID, "items", len(c.builds[heroID].Items))
}

// RankTier returns the player's medal as OpenDota encodes it (tens digit = medal,
// ones = stars), or 0 while unknown. The first call per account starts a fetch.
func (c *Client) RankTier(accountID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if tier, ok := c.rankTiers[accountID]; ok || c.rankPending[accountID] || accountID == "" || accountID == "0" {
		return tier
	}
	c.rankPending[accountID] = true
	go func() {
		ctx, cancel := context.WithTimeout(c.life(), 30*time.Second)
		defer cancel()
		var player struct {
			RankTier int `json:"rank_tier"`
		}
		data, err := c.fetch(ctx, "/players/"+accountID)
		if err == nil {
			err = json.Unmarshal(data, &player)
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		c.rankTiers[accountID] = player.RankTier
		if err != nil {
			c.log.Warn("rank lookup failed", "err", err)
		}
	}()
	return 0
}

// getJSON decodes OpenDota's answer at path into out, cached as cacheName for maxAge.
func (c *Client) getJSON(ctx context.Context, path, cacheName string, maxAge time.Duration, out any) error {
	return c.cachedBytes(cacheName, maxAge, func() ([]byte, error) { return c.fetch(ctx, path) }, func(data []byte) error {
		// Decoded into a fresh value, so a copy that fails leaves nothing behind in out.
		fresh := reflect.New(reflect.TypeOf(out).Elem())
		if err := json.Unmarshal(data, fresh.Interface()); err != nil {
			return err
		}
		reflect.ValueOf(out).Elem().Set(fresh.Elem())
		return nil
	}, nil)
}

func (c *Client) fetch(ctx context.Context, path string) ([]byte, error) {
	return c.do(ctx, http.MethodGet, path)
}

// do calls OpenDota within the free tier's rate, waiting out and retrying a 429 twice.
func (c *Client) do(ctx context.Context, method, path string) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		if err := c.limit.wait(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, method, c.base+path, nil)
		if err != nil {
			return nil, err
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			// Every call waits out the pause, whether or not this one tries again.
			c.limit.pause(retryAfter(resp))
			if attempt < 2 {
				resp.Body.Close()
				continue
			}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s %s: %s", method, path, resp.Status)
		}
		return io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	}
}

// SetBaseURL points the client at another OpenDota-compatible server, as tests do with a
// fake OpenDota, which has no rate limit to keep to.
func (c *Client) SetBaseURL(u string) {
	c.base = u
	c.limit = &limiter{}
}

// WaitReady blocks until hero and item data has loaded (or failed to).
func (c *Client) WaitReady(ctx context.Context) {
	select {
	case <-c.ready:
	case <-ctx.Done():
	}
}

// HeroName returns the hero's display name, or "hero <id>" if unknown.
func (c *Client) HeroName(id int) string {
	if h, ok := c.Hero(id); ok {
		return h.LocalizedName
	}
	return fmt.Sprintf("hero %d", id)
}
