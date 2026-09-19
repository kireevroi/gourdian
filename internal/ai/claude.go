package ai

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"time"
)

// Claude runs Claude Code headless, billed to the player's Claude subscription.
type Claude struct {
	Path    func() string // configured path override
	WorkDir string
}

func (c *Claude) Info() Info {
	return Info{ID: "claude", Name: "Claude Code", Kind: "cli", Billing: "Claude subscription",
		// Claude Code's aliases always mean the newest model of each family, so nothing here
		// goes out of date when Anthropic releases a model.
		Models: []Model{
			{ID: "fable", Name: "Fable, newest (most capable)"},
			{ID: "opus", Name: "Opus, newest (best advice)"},
			{ID: "sonnet", Name: "Sonnet, newest (faster)"},
			{ID: "haiku", Name: "Haiku, newest (fastest)"},
		},
		Efforts: Efforts, CanInstall: true}
}

// FindClaude locates the Claude Code CLI, including the copy bundled with the VS Code extension.
func FindClaude(override string) string {
	exe := "claude"
	if runtime.GOOS == "windows" {
		exe = "claude.exe"
	}
	var extra []string
	extra = append(extra, filepath.Join(".local", "bin", exe), filepath.Join(".claude", "local", exe))
	for _, editor := range []string{".vscode-server", ".vscode", ".cursor-server", ".cursor"} {
		extra = append(extra, filepath.Join(editor, "extensions", "anthropic.claude-code-*", "resources", "native-binary", exe))
	}
	return findCLI(override, "claude", extra...)
}

func (c *Claude) exe() string {
	override := ""
	if c.Path != nil {
		override = c.Path()
	}
	return FindClaude(override)
}

type claudeAuth struct {
	LoggedIn   bool   `json:"loggedIn"`
	AuthMethod string `json:"authMethod"`
}

func (c *Claude) Status(ctx context.Context) Status {
	exe := c.exe()
	if exe == "" {
		return Status{State: StateMissing, Detail: "Claude Code isn't installed"}
	}
	out, _, err := run(ctx, c.WorkDir, exe, "", "auth", "status")
	var st claudeAuth
	if jsonErr := json.Unmarshal(out, &st); jsonErr != nil {
		if err == nil {
			err = jsonErr
		}
		return Status{State: StateError, Detail: "couldn't check the login: " + err.Error()}
	}
	switch {
	case !st.LoggedIn:
		return Status{State: StateLogin, Detail: "Claude Code is logged out"}
	case st.AuthMethod != "claude.ai":
		return Status{State: StateReady, Detail: "Logged in with " + st.AuthMethod + ", which bills that account rather than a subscription · " + exe}
	}
	return Status{State: StateReady, Detail: "Logged in with your Claude subscription · " + exe}
}

func (c *Claude) Login() error {
	exe := c.exe()
	if exe == "" {
		return &Error{Kind: ErrOther, Msg: "Claude Code isn't installed"}
	}
	return openConsole(c.WorkDir, exe, "auth", "login")
}

// SwitchAccount signs out and opens the login again, for moving to another Claude account.
func (c *Claude) SwitchAccount() error {
	exe := c.exe()
	if exe == "" {
		return &Error{Kind: ErrOther, Msg: "Claude Code isn't installed"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	run(ctx, c.WorkDir, exe, "", "auth", "logout")
	return openConsole(c.WorkDir, exe, "auth", "login")
}

type claudeResult struct {
	IsError          bool            `json:"is_error"`
	Subtype          string          `json:"subtype"`
	Result           string          `json:"result"`
	StructuredOutput json.RawMessage `json:"structured_output"`
}

// Complete runs one headless turn with every tool, MCP server and settings file disabled,
// so the call is a plain question and answer.
func (c *Claude) Complete(ctx context.Context, req Request) (json.RawMessage, error) {
	exe := c.exe()
	if exe == "" {
		return nil, &Error{Kind: ErrAuth, Msg: "Claude Code isn't installed"}
	}
	args := []string{"-p", "--output-format", "json", "--tools", "", "--strict-mcp-config", "--setting-sources", "",
		"--no-session-persistence", "--disable-slash-commands", "--system-prompt", req.System, "--json-schema", req.Schema}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.Effort != "" {
		args = append(args, "--effort", req.Effort)
	}
	out, errOut, runErr := run(ctx, c.WorkDir, exe, req.Prompt, args...)
	var res claudeResult
	if err := json.Unmarshal(out, &res); err != nil {
		switch {
		case ctx.Err() != nil:
			return nil, timeout("Claude Code", ctx.Err())
		case runErr != nil:
			return nil, failure("claude: %v: %s", runErr, tail(string(errOut), 300))
		}
		return nil, failure("claude: unexpected output: %s", tail(string(out), 300))
	}
	if res.IsError || len(res.StructuredOutput) == 0 || string(res.StructuredOutput) == "null" {
		return nil, failure("claude (%s): %s", res.Subtype, tail(res.Result, 300))
	}
	return res.StructuredOutput, nil
}

func (c *Claude) Install(ctx context.Context, progress func(string)) error {
	return installClaude(ctx, progress)
}
