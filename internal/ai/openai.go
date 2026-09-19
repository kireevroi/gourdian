package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// Compatible calls any service that speaks OpenAI's chat completions API: OpenAI itself,
// OpenRouter, Google's Gemini API, Groq, DeepSeek, or a custom URL such as a local server.
type Compatible struct {
	ID_      string
	Name     string
	Base     func() string
	Key      func() string
	KeyURL   string
	Custom   bool
	Models   []Model
	Headers  map[string]string
	Client   *http.Client
	modeMu   sync.Mutex
	modeByID map[string]int // the response_format that works for each model
}

// Response formats, from strictest to loosest; a service that rejects one gets the next.
const (
	formatStrictSchema = iota
	formatSchema
	formatJSONObject
)

// CompatiblePresets are the OpenAI-compatible services offered on the AI page.
func CompatiblePresets(key func(id string) string, customURL func() string) []*Compatible {
	fixed := func(u string) func() string { return func() string { return u } }
	keyFor := func(id string) func() string { return func() string { return key(id) } }
	return []*Compatible{
		{ID_: "openai", Name: "OpenAI API", Base: fixed("https://api.openai.com/v1"), Key: keyFor("openai"),
			KeyURL: "https://platform.openai.com/api-keys"},
		{ID_: "openrouter", Name: "OpenRouter", Base: fixed("https://openrouter.ai/api/v1"), Key: keyFor("openrouter"),
			KeyURL:  "https://openrouter.ai/keys",
			Headers: map[string]string{"HTTP-Referer": "https://github.com/kireevroi/gourdian", "X-Title": "Dota Trainer"}},
		{ID_: "gemini-api", Name: "Google Gemini API", Base: fixed("https://generativelanguage.googleapis.com/v1beta/openai"), Key: keyFor("gemini-api"),
			KeyURL: "https://aistudio.google.com/apikey"},
		{ID_: "groq", Name: "Groq", Base: fixed("https://api.groq.com/openai/v1"), Key: keyFor("groq"),
			KeyURL: "https://console.groq.com/keys"},
		{ID_: "deepseek", Name: "DeepSeek", Base: fixed("https://api.deepseek.com/v1"), Key: keyFor("deepseek"),
			KeyURL: "https://platform.deepseek.com/api_keys"},
		{ID_: "custom", Name: "Custom OpenAI-compatible URL", Base: customURL, Key: keyFor("custom"), Custom: true},
	}
}

func (c *Compatible) Info() Info {
	billing := "API key, pay per use"
	if c.Custom {
		billing = "Your own endpoint (for example Ollama or LM Studio)"
	}
	return Info{ID: c.ID_, Name: c.Name, Kind: "api", Billing: billing, Models: c.Models, KeyURL: c.KeyURL, CustomURL: c.Custom}
}

func (c *Compatible) http() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: 3 * time.Minute}
}

func (c *Compatible) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	base := strings.TrimRight(c.Base(), "/")
	if base == "" {
		return nil, &Error{Kind: ErrAuth, Msg: "no URL set for " + c.Name}
	}
	var r io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if key := c.Key(); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, timeout(c.Name, ctx.Err())
		}
		return nil, &Error{Kind: ErrOther, Msg: c.Name + ": " + err.Error()}
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		kind := ErrOther
		switch resp.StatusCode {
		case 401, 403:
			kind = ErrAuth
		case 429:
			kind = ErrLimit
		}
		return data, &Error{Kind: kind, Msg: fmt.Sprintf("%s: %s: %s", c.Name, resp.Status, tail(string(data), 300))}
	}
	return data, nil
}

func (c *Compatible) Status(ctx context.Context) Status {
	if c.Custom && c.Base() == "" {
		return Status{State: StateKey, Detail: "Enter the endpoint URL"}
	}
	if !c.Custom && c.Key() == "" {
		return Status{State: StateKey, Detail: "No API key yet"}
	}
	if _, err := c.ListModels(ctx); err != nil {
		if KindOf(err) == ErrAuth {
			return Status{State: StateKey, Detail: "The API key was rejected"}
		}
		return Status{State: StateError, Detail: err.Error()}
	}
	return Status{State: StateReady, Detail: "Connected"}
}

func (c *Compatible) ListModels(ctx context.Context) ([]Model, error) {
	data, err := c.do(ctx, http.MethodGet, "/models", nil)
	if err != nil {
		return nil, err
	}
	var list struct {
		Data []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Created int64  `json:"created"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("%s: unexpected model list", c.Name)
	}
	out := make([]Model, 0, len(list.Data))
	for _, m := range list.Data {
		out = append(out, Model{ID: strings.TrimPrefix(m.ID, "models/"), Name: m.Name, Created: m.Created})
	}
	out = ChatModels(out)
	slices.SortStableFunc(out, newerFirst)
	return out, nil
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Complete asks for a JSON answer, trying a strict schema first and looser formats when the
// service or model doesn't support it.
func (c *Compatible) Complete(ctx context.Context, req Request) (json.RawMessage, error) {
	c.modeMu.Lock()
	mode := c.modeByID[req.Model]
	c.modeMu.Unlock()
	for ; mode <= formatJSONObject; mode++ {
		system := req.System
		body := map[string]any{"model": req.Model}
		switch mode {
		case formatStrictSchema, formatSchema:
			body["response_format"] = map[string]any{"type": "json_schema", "json_schema": map[string]any{
				"name": "answer", "strict": mode == formatStrictSchema, "schema": json.RawMessage(req.Schema)}}
		default:
			body["response_format"] = map[string]any{"type": "json_object"}
			system += schemaInstruction(req.Schema)
		}
		body["messages"] = []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": req.Prompt}}
		data, err := c.do(ctx, http.MethodPost, "/chat/completions", body)
		if err != nil {
			var e *Error
			if isBadRequest(err, &e) && mode < formatJSONObject {
				continue // this format isn't supported here; try the next one
			}
			return nil, err
		}
		var res chatResponse
		if err := json.Unmarshal(data, &res); err != nil || len(res.Choices) == 0 {
			return nil, &Error{Kind: ErrOther, Msg: c.Name + ": unexpected answer: " + tail(string(data), 200)}
		}
		c.modeMu.Lock()
		if c.modeByID == nil {
			c.modeByID = map[string]int{}
		}
		c.modeByID[req.Model] = mode
		c.modeMu.Unlock()
		return ExtractJSON(res.Choices[0].Message.Content)
	}
	return nil, &Error{Kind: ErrOther, Msg: c.Name + " doesn't support JSON answers"}
}

func isBadRequest(err error, target **Error) bool {
	e, ok := err.(*Error)
	*target = e
	return ok && e.Kind == ErrOther && (strings.Contains(e.Msg, "400") || strings.Contains(e.Msg, "422"))
}
