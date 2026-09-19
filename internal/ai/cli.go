package ai

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gourdian/internal/hidewin"
)

// findCLI looks for a command-line tool: the configured path, then PATH, then npm's global
// folder, then any extra glob patterns under the home folder (newest match wins).
func findCLI(override string, name string, extra ...string) string {
	if override != "" {
		if _, err := os.Stat(override); err == nil {
			return override
		}
		return ""
	}
	names := []string{name}
	if runtime.GOOS == "windows" {
		names = []string{name + ".exe", name + ".cmd", name}
	}
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	if bin := ToolsBin(); bin != "" {
		for _, n := range names {
			if p := filepath.Join(bin, n); fileExists(p) {
				return p
			}
		}
	}
	if appData := os.Getenv("APPDATA"); appData != "" {
		for _, n := range names {
			if p := filepath.Join(appData, "npm", n); fileExists(p) {
				return p
			}
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	var best string
	var bestTime time.Time
	for _, pattern := range extra {
		matches, _ := filepath.Glob(filepath.Join(home, pattern))
		for _, m := range matches {
			if fi, err := os.Stat(m); err == nil && !fi.IsDir() && fi.ModTime().After(bestTime) {
				best, bestTime = m, fi.ModTime()
			}
		}
	}
	return best
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// run starts a CLI without a console window, feeding stdin, and returns stdout and stderr.
func run(ctx context.Context, dir, exe string, stdin string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = dir
	cmd.Env = envWithTools()
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	hidewin.Apply(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// openConsole runs a CLI in its own console window, for logins that need the player.
func openConsole(dir, exe string, args ...string) error {
	if runtime.GOOS != "windows" {
		return errors.New("run `" + filepath.Base(exe) + " " + strings.Join(args, " ") + "` in a terminal")
	}
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.Env = envWithTools()
	hidewin.Visible(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// runStream runs a command and reports its output line by line as it arrives.
func runStream(ctx context.Context, dir, exe string, onLine func(string), args ...string) error {
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = dir
	cmd.Env = envWithTools()
	hidewin.Apply(cmd)
	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}
	defer pr.Close()
	cmd.Stdout, cmd.Stderr = pw, pw
	err = cmd.Start()
	pw.Close()
	if err != nil {
		return err
	}
	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 0, 8<<10), 1<<20)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" && onLine != nil {
			onLine(line)
		}
	}
	return cmd.Wait()
}

// envWithTools puts the trainer's own tools folder first on PATH, so a CLI it installed can
// find its helpers.
func envWithTools() []string {
	node := ToolsBin()
	if node == "" || !dirExists(node) {
		return nil
	}
	env := os.Environ()
	for i, kv := range env {
		if k, v, _ := strings.Cut(kv, "="); strings.EqualFold(k, "PATH") {
			env[i] = k + "=" + node + string(os.PathListSeparator) + v
			return env
		}
	}
	return append(env, "PATH="+node)
}

// tempDir is an empty folder for a CLI to run in, so it can't read anything unrelated.
func tempDir() (string, func(), error) {
	dir, err := os.MkdirTemp("", "gourdian-ai-")
	if err != nil {
		return "", nil, err
	}
	return dir, func() { os.RemoveAll(dir) }, nil
}
