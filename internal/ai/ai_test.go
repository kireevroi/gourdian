package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestErrorKinds(t *testing.T) {
	cases := map[string]ErrorKind{
		"claude (success): Failed to authenticate: OAuth session expired and could not be refreshed": ErrAuth,
		"claude (success): Invalid API key · Please run /login":                                      ErrAuth,
		"claude (success): Claude AI usage limit reached|1789000000":                                 ErrLimit,
		"claude: exit status 1: API Error: 429 Too Many Requests":                                    ErrLimit,
		"claude returned no suggestions":                                                             ErrOther,
		"claude: unexpected output: 14030 tokens":                                                    ErrOther,
	}
	for msg, want := range cases {
		if got := KindOf(errors.New(msg)); got != want {
			t.Errorf("KindOf(%q) = %s, want %s", msg, got, want)
		}
	}
	if got := KindOf(context.DeadlineExceeded); got != ErrTimeout {
		t.Errorf("deadline = %s", got)
	}
}

func TestExtractJSON(t *testing.T) {
	for in, want := range map[string]string{
		`{"tips":["a"]}`:                           `{"tips":["a"]}`,
		"```json\n{\"tips\":[\"a\"]}\n```":         `{"tips":["a"]}`,
		`Here you go: {"tips": ["x {y}"]} Thanks.`: `{"tips": ["x {y}"]}`,
	} {
		got, err := ExtractJSON(in)
		if err != nil || string(got) != want {
			t.Errorf("ExtractJSON(%q) = %s, %v", in, got, err)
		}
	}
	if _, err := ExtractJSON("no json here"); err == nil {
		t.Error("text without JSON should fail")
	}
}

const schema = `{"type":"object","properties":{"tips":{"type":"array","items":{"type":"string"}}},"required":["tips"],"additionalProperties":false}`

func TestCompatibleFallsBackToLooserFormats(t *testing.T) {
	var formats []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-good" {
			http.Error(w, `{"error":"bad key"}`, http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/models":
			w.Write([]byte(`{"data":[{"id":"models/b"},{"id":"a"}]}`))
		case "/chat/completions":
			var body struct {
				ResponseFormat struct {
					Type       string `json:"type"`
					JSONSchema struct {
						Strict bool `json:"strict"`
					} `json:"json_schema"`
				} `json:"response_format"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			formats = append(formats, body.ResponseFormat.Type)
			if body.ResponseFormat.Type == "json_schema" {
				http.Error(w, `{"error":"response_format not supported"}`, http.StatusBadRequest)
				return
			}
			w.Write([]byte(`{"choices":[{"message":{"content":"{\"tips\":[\"Push mid\"]}"}}]}`))
		}
	}))
	defer srv.Close()
	key := "sk-good"
	c := &Compatible{ID_: "test", Name: "Test", Base: func() string { return srv.URL }, Key: func() string { return key }}
	out, err := c.Complete(t.Context(), Request{System: "s", Prompt: "p", Schema: schema, Model: "m"})
	if err != nil || string(out) != `{"tips":["Push mid"]}` {
		t.Fatalf("answer %s, %v", out, err)
	}
	if strings.Join(formats, ",") != "json_schema,json_schema,json_object" {
		t.Fatalf("formats tried: %v", formats)
	}
	formats = nil
	c.Complete(t.Context(), Request{Schema: schema, Model: "m"})
	if len(formats) != 1 {
		t.Fatalf("the working format should be remembered: %v", formats)
	}
	if models, err := c.ListModels(t.Context()); err != nil || len(models) != 2 || models[1].ID != "b" {
		t.Fatalf("models = %+v, %v", models, err)
	}
	if st := c.Status(t.Context()); st.State != StateReady {
		t.Fatalf("status = %+v", st)
	}
	key = "sk-bad"
	if _, err := c.Complete(t.Context(), Request{Schema: schema, Model: "m"}); KindOf(err) != ErrAuth {
		t.Fatalf("bad key error = %v (%s)", err, KindOf(err))
	}
	if st := c.Status(t.Context()); st.State != StateKey {
		t.Fatalf("status with bad key = %+v", st)
	}
}

func TestAnthropicAPI(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "sk-ant-good" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
			return
		}
		data, _ := io.ReadAll(r.Body)
		json.Unmarshal(data, &got)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5","stop_reason":"end_turn",
			"content":[{"type":"text","text":"{\"tips\":[\"Stack at 0:53\"]}"}],"usage":{"input_tokens":10,"output_tokens":5}}`))
	}))
	defer srv.Close()
	key := "sk-ant-good"
	a := &Anthropic{Key: func() string { return key }, BaseURL: srv.URL}
	out, err := a.Complete(t.Context(), Request{System: "coach", Prompt: "state", Schema: schema, Model: "claude-opus-5", Effort: "low"})
	if err != nil || string(out) != `{"tips":["Stack at 0:53"]}` {
		t.Fatalf("answer %s, %v", out, err)
	}
	oc, _ := got["output_config"].(map[string]any)
	format, _ := oc["format"].(map[string]any)
	if oc["effort"] != "low" || format["type"] != "json_schema" || got["thinking"] == nil {
		t.Fatalf("request = %v", got)
	}
	key = "sk-ant-bad"
	if _, err := a.Complete(t.Context(), Request{Schema: schema, Model: "claude-opus-5"}); KindOf(err) != ErrAuth {
		t.Fatalf("bad key error = %v", err)
	}
	key = ""
	if st := a.Status(t.Context()); st.State != StateKey {
		t.Fatalf("status without key = %+v", st)
	}
}

// script writes a fake CLI; tests using it run only where shell scripts can.
func script(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake CLIs are shell scripts")
	}
	p := filepath.Join(t.TempDir(), "cli")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCodexCLI(t *testing.T) {
	exe := script(t, `
if [ "$1" = login ]; then echo "Logged in using ChatGPT"; exit 0; fi
out=""; while [ $# -gt 0 ]; do [ "$1" = "-o" ] && out="$2"; shift; done
cat > /dev/null
echo '{"tips":["Farm the triangle"]}' > "$out"
`)
	c := &Codex{Path: func() string { return exe }, WorkDir: t.TempDir()}
	if st := c.Status(t.Context()); st.State != StateReady || !strings.Contains(st.Detail, "ChatGPT") {
		t.Fatalf("status = %+v", st)
	}
	out, err := c.Complete(t.Context(), Request{System: "s", Prompt: "p", Schema: schema, Model: "gpt-5", Effort: "max"})
	if err != nil || strings.TrimSpace(string(out)) != `{"tips":["Farm the triangle"]}` {
		t.Fatalf("answer %s, %v", out, err)
	}
}

func TestClaudeCLI(t *testing.T) {
	exe := script(t, `
if [ "$1" = auth ]; then echo '{"loggedIn":false,"authMethod":"none"}'; exit 0; fi
cat > /dev/null
echo '{"is_error":true,"subtype":"success","result":"Failed to authenticate: OAuth session expired"}'
`)
	c := &Claude{Path: func() string { return exe }, WorkDir: t.TempDir()}
	if st := c.Status(t.Context()); st.State != StateLogin {
		t.Fatalf("status = %+v", st)
	}
	if _, err := c.Complete(t.Context(), Request{Schema: schema}); KindOf(err) != ErrAuth {
		t.Fatalf("error = %v", err)
	}
}
