package ai

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Codex runs OpenAI's Codex CLI headless, billed to the player's ChatGPT plan.
type Codex struct {
	Path    func() string
	WorkDir string
}

func (c *Codex) Info() Info {
	return Info{ID: "codex", Name: "OpenAI Codex CLI", Kind: "cli", Billing: "ChatGPT subscription",
		// Codex picks the current model for your ChatGPT plan itself; any other name can still
		// be typed on the AI page.
		Models:  []Model{{ID: "", Name: "Codex's current model"}},
		Efforts: []string{"low", "medium", "high"}, CanInstall: true}
}

func (c *Codex) exe() string {
	override := ""
	if c.Path != nil {
		override = c.Path()
	}
	return findCLI(override, "codex")
}

func (c *Codex) Status(ctx context.Context) Status {
	exe := c.exe()
	if exe == "" {
		return Status{State: StateMissing, Detail: "The Codex CLI isn't installed"}
	}
	out, errOut, err := run(ctx, c.WorkDir, exe, "", "login", "status")
	text := strings.TrimSpace(string(out) + " " + string(errOut))
	if err != nil || strings.Contains(strings.ToLower(text), "not logged in") {
		return Status{State: StateLogin, Detail: "Codex is logged out"}
	}
	return Status{State: StateReady, Detail: text + " · " + exe}
}

func (c *Codex) Login() error {
	exe := c.exe()
	if exe == "" {
		return &Error{Kind: ErrOther, Msg: "the Codex CLI isn't installed"}
	}
	return openConsole(c.WorkDir, exe, "login")
}

// Complete runs `codex exec` in a read-only sandbox in an empty folder, with the answer's
// shape enforced by --output-schema and written to a file.
func (c *Codex) Complete(ctx context.Context, req Request) (json.RawMessage, error) {
	exe := c.exe()
	if exe == "" {
		return nil, &Error{Kind: ErrAuth, Msg: "the Codex CLI isn't installed"}
	}
	dir, cleanup, err := tempDir()
	if err != nil {
		return nil, err
	}
	defer cleanup()
	schema, answer := filepath.Join(dir, "schema.json"), filepath.Join(dir, "answer.json")
	if err := os.WriteFile(schema, []byte(req.Schema), 0o600); err != nil {
		return nil, err
	}
	args := []string{"exec", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "--output-schema", schema, "-o", answer}
	if req.Model != "" {
		args = append(args, "-m", req.Model)
	}
	if effort := codexEffort(req.Effort); effort != "" {
		args = append(args, "-c", "model_reasoning_effort="+effort)
	}
	args = append(args, "-")
	_, errOut, runErr := run(ctx, dir, exe, req.System+"\n\n"+req.Prompt, args...)
	data, readErr := os.ReadFile(answer)
	switch {
	case ctx.Err() != nil:
		return nil, timeout("Codex", ctx.Err())
	case readErr != nil || len(strings.TrimSpace(string(data))) == 0:
		return nil, failure("codex: %v: %s", runErr, tail(string(errOut), 300))
	}
	return ExtractJSON(string(data))
}

func codexEffort(e string) string {
	switch e {
	case "":
		return ""
	case "xhigh", "max":
		return "high"
	}
	return e
}

func (c *Codex) Install(ctx context.Context, progress func(string)) error {
	return installCodex(ctx, progress)
}

// SwitchAccount signs out and opens the login again, for moving to another ChatGPT account.
func (c *Codex) SwitchAccount() error {
	exe := c.exe()
	if exe == "" {
		return &Error{Kind: ErrOther, Msg: "the Codex CLI isn't installed"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	run(ctx, c.WorkDir, exe, "", "logout")
	return openConsole(c.WorkDir, exe, "login")
}
