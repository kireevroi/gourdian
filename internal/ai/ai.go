// Package ai connects the coach to a language model: the Claude Code or Codex CLIs using
// the player's subscriptions, or APIs with keys (Anthropic and OpenAI-compatible services).
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Request asks for one JSON answer matching Schema.
type Request struct {
	System string
	Prompt string
	Schema string
	Model  string
	Effort string // low, medium, high, xhigh, max; providers without effort ignore it
}

type Model struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Created int64  `json:"created,omitempty"` // unix time, when the provider says
}

// Info describes a provider for the dashboard.
type Info struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Kind    string   `json:"kind"`    // "cli" or "api"
	Billing string   `json:"billing"` // what pays for it
	Models  []Model  `json:"models"`  // suggestions; APIs can list the real ones
	Efforts []string `json:"efforts,omitempty"`
	// KeyURL is where to create an API key; CustomURL means the player enters the endpoint.
	KeyURL    string `json:"key_url,omitempty"`
	CustomURL bool   `json:"custom_url,omitempty"`
	// CanInstall means the trainer can download and set this one up itself.
	CanInstall bool `json:"can_install,omitempty"`
}

// Provider states.
const (
	StateReady   = "ready"
	StateMissing = "missing" // CLI not installed
	StateLogin   = "login"   // CLI not logged in
	StateKey     = "key"     // API key missing or rejected
	StateError   = "error"
)

type Status struct {
	State  string `json:"state"`
	Detail string `json:"detail"`
}

type Provider interface {
	Info() Info
	Status(ctx context.Context) Status
	Complete(ctx context.Context, req Request) (json.RawMessage, error)
}

// Loginer is a CLI provider that can open its login flow in a console window.
type Loginer interface {
	Login() error
}

// Switcher is a CLI provider that can log out and let the player sign in as someone else.
type Switcher interface {
	SwitchAccount() error
}

// ModelLister is a provider that can list the models an account can use.
type ModelLister interface {
	ListModels(ctx context.Context) ([]Model, error)
}

// ErrorKind says how the trainer should react to a failed request.
type ErrorKind string

const (
	ErrAuth    ErrorKind = "auth"  // logged out or a bad key: pause until it's fixed
	ErrLimit   ErrorKind = "limit" // usage or rate limit: back off
	ErrTimeout ErrorKind = "timeout"
	ErrOther   ErrorKind = "other"
)

type Error struct {
	Kind ErrorKind
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

// KindOf classifies any error from Complete.
func KindOf(err error) ErrorKind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrTimeout
	}
	return classify(err.Error())
}

var (
	authPattern  = regexp.MustCompile(`(?i)authenticat|oauth|not logged in|/login|log ?in again|invalid (x-)?api[ -]key|incorrect api key|unauthori[sz]ed|permission denied|\b40[13]\b`)
	limitPattern = regexp.MustCompile(`(?i)rate.?limit|usage limit|limit reached|quota|too many requests|\b429\b`)
)

func classify(msg string) ErrorKind {
	switch {
	case authPattern.MatchString(msg):
		return ErrAuth
	case limitPattern.MatchString(msg):
		return ErrLimit
	}
	return ErrOther
}

// failure builds an Error whose kind comes from its message.
func failure(format string, args ...any) *Error {
	msg := fmt.Sprintf(format, args...)
	return &Error{Kind: classify(msg), Msg: msg}
}

func timeout(name string, err error) *Error {
	return &Error{Kind: ErrTimeout, Msg: name + " took too long: " + err.Error()}
}

// ExtractJSON finds the JSON object in a model's text answer, which may be wrapped in a code
// fence or a sentence.
func ExtractJSON(text string) (json.RawMessage, error) {
	text = strings.TrimSpace(text)
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return nil, fmt.Errorf("no JSON in the answer: %s", tail(text, 200))
	}
	raw := json.RawMessage(text[start : end+1])
	if !json.Valid(raw) {
		return nil, fmt.Errorf("the answer's JSON is invalid: %s", tail(text, 200))
	}
	return raw, nil
}

// schemaInstruction asks models without schema support to answer in the schema's shape.
func schemaInstruction(schema string) string {
	return "\n\nReply with only a JSON object, no code fence and no other text, that matches this JSON schema:\n" + schema
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

// Efforts are the thinking effort levels the trainer offers.
var Efforts = []string{"low", "medium", "high", "xhigh", "max"}
