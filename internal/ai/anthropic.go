package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Anthropic calls the Claude API with an API key, billed per request.
type Anthropic struct {
	Key     func() string
	BaseURL string // for tests
}

func (a *Anthropic) Info() Info {
	return Info{ID: "anthropic", Name: "Anthropic API", Kind: "api", Billing: "API key, pay per use",
		Models: []Model{
			{ID: string(anthropic.ModelClaudeFable5_1), Name: "Claude Fable 5.1"},
			{ID: string(anthropic.ModelClaudeOpus5), Name: "Claude Opus 5"},
			{ID: string(anthropic.ModelClaudeSonnet5), Name: "Claude Sonnet 5"},
			{ID: string(anthropic.ModelClaudeHaiku4_5), Name: "Claude Haiku 4.5"},
		},
		Efforts: Efforts, KeyURL: "https://platform.claude.com/settings/keys"}
}

func (a *Anthropic) client() (anthropic.Client, bool) {
	key := a.Key()
	opts := []option.RequestOption{option.WithAPIKey(key), option.WithMaxRetries(1)}
	if a.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(a.BaseURL))
	}
	return anthropic.NewClient(opts...), key != ""
}

func (a *Anthropic) Status(ctx context.Context) Status {
	if _, ok := a.client(); !ok {
		return Status{State: StateKey, Detail: "No API key yet"}
	}
	if _, err := a.ListModels(ctx); err != nil {
		if KindOf(err) == ErrAuth {
			return Status{State: StateKey, Detail: "The API key was rejected"}
		}
		return Status{State: StateError, Detail: err.Error()}
	}
	return Status{State: StateReady, Detail: "API key works"}
}

func (a *Anthropic) ListModels(ctx context.Context) ([]Model, error) {
	c, ok := a.client()
	if !ok {
		return nil, &Error{Kind: ErrAuth, Msg: "no Anthropic API key"}
	}
	var out []Model
	pager := c.Models.ListAutoPaging(ctx, anthropic.ModelListParams{})
	for pager.Next() {
		m := pager.Current()
		out = append(out, Model{ID: m.ID, Name: m.DisplayName, Created: m.CreatedAt.Unix()})
	}
	return out, apiError(pager.Err())
}

func (a *Anthropic) Complete(ctx context.Context, req Request) (json.RawMessage, error) {
	c, ok := a.client()
	if !ok {
		return nil, &Error{Kind: ErrAuth, Msg: "no Anthropic API key"}
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(req.Schema), &schema); err != nil {
		return nil, err
	}
	params := anthropic.MessageNewParams{
		Model:        anthropic.Model(req.Model),
		MaxTokens:    8000,
		System:       []anthropic.TextBlockParam{{Text: req.System}},
		Messages:     []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(req.Prompt))},
		Thinking:     anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{}},
		OutputConfig: anthropic.OutputConfigParam{Format: anthropic.JSONOutputFormatParam{Schema: schema}},
	}
	if req.Effort != "" {
		params.OutputConfig.Effort = anthropic.OutputConfigEffort(req.Effort)
	}
	msg, err := c.Messages.New(ctx, params)
	if err != nil {
		if ctx.Err() != nil {
			return nil, timeout("the Anthropic API", ctx.Err())
		}
		return nil, apiError(err)
	}
	if msg.StopReason == anthropic.StopReasonRefusal {
		return nil, &Error{Kind: ErrOther, Msg: "the model declined to answer"}
	}
	var text strings.Builder
	for _, block := range msg.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	return ExtractJSON(text.String())
}

// apiError turns SDK errors into typed errors by HTTP status.
func apiError(err error) error {
	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) {
		return err
	}
	kind := ErrOther
	switch apiErr.StatusCode {
	case 401, 403:
		kind = ErrAuth
	case 429:
		kind = ErrLimit
	}
	return &Error{Kind: kind, Msg: "Anthropic API: " + tail(apiErr.Error(), 300)}
}
