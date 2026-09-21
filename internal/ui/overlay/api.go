package overlay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gourdian/internal/sys/config"
)

// settingsView is the part of GET /api/settings the overlay needs.
type settingsView struct {
	Settings  config.Settings `json:"settings"`
	Recording string          `json:"recording"`
}

type api struct {
	base   string
	client *http.Client
}

func newAPI(base string) api {
	return api{base: strings.TrimRight(base, "/"), client: &http.Client{Timeout: 10 * time.Second}}
}

func (a api) do(ctx context.Context, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("trainer not reachable")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return fmt.Errorf("%s", strings.TrimSpace(string(msg)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// patch is a partial settings object; the server merges it into the current settings, so a
// change made in game never overwrites one made on the dashboard in the meantime.
type patch map[string]any

// wheelLayout applies one mouse-wheel notch while the HUD layout is being edited: size in
// steps of 10%, or with Ctrl held the background opacity in steps of 10%.
func wheelLayout(o config.OverlaySettings, up, ctrl bool) config.OverlaySettings {
	step := 10
	if !up {
		step = -10
	}
	if ctrl {
		o.HUDBackground = min(max(o.HUDBackground+step, 0), 100)
	} else {
		o.HUDScale = min(max(o.HUDScale+step, config.MinHUDScale), config.MaxHUDScale)
	}
	return o
}
