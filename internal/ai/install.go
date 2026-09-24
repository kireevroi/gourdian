package ai

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Installer is a provider the trainer can install by itself.
type Installer interface {
	Install(ctx context.Context, progress func(string)) error
}

var (
	claudeDist = "https://downloads.claude.ai/claude-code-releases"
	codexAPI   = "https://api.github.com/repos/openai/codex/releases/latest"
)

// ToolsBin is where the trainer keeps CLIs it installed, empty when it has no folder yet.
func ToolsBin() string {
	dir, _ := toolsDir.Load().(string)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "tools")
}

// claudePlatform is this machine's name in Claude Code's release manifest.
func claudePlatform() (string, error) {
	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[runtime.GOARCH]
	os := map[string]string{"windows": "win32", "linux": "linux", "darwin": "darwin"}[runtime.GOOS]
	if arch == "" || os == "" {
		return "", fmt.Errorf("Claude Code has no build for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return os + "-" + arch, nil
}

var versionRE = regexp.MustCompile(`^\d+\.\d+\.\d+`)

type claudeManifest struct {
	Version   string `json:"version"`
	Platforms map[string]struct {
		Checksum string `json:"checksum"`
		Size     int64  `json:"size"`
	} `json:"platforms"`
}

// installClaude downloads Claude Code's own build for this machine and lets it install
// itself, the same steps as the installers at claude.ai/install.ps1 and install.sh.
func installClaude(ctx context.Context, progress func(string)) error {
	exe, err := downloadClaude(ctx, progress)
	if err != nil {
		return err
	}
	defer os.Remove(exe)
	say(progress, "Setting up Claude Code…")
	if err := runStream(ctx, os.TempDir(), exe, func(line string) { say(progress, line) }, "install"); err != nil {
		return fmt.Errorf("Claude Code's installer failed: %w", err)
	}
	return nil
}

func downloadClaude(ctx context.Context, progress func(string)) (string, error) {
	platform, err := claudePlatform()
	if err != nil {
		return "", err
	}
	binary := "claude"
	if runtime.GOOS == "windows" {
		binary = "claude.exe"
	}
	body, err := get(ctx, claudeDist+"/latest")
	if err != nil {
		return "", err
	}
	raw, err := io.ReadAll(io.LimitReader(body, 1<<10))
	body.Close()
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(string(raw))
	if !versionRE.MatchString(version) {
		return "", errors.New("downloads.claude.ai didn't give a version number; it may be blocked here")
	}
	var m claudeManifest
	if err := getJSON(ctx, claudeDist+"/"+version+"/manifest.json", &m); err != nil {
		return "", err
	}
	plat, ok := m.Platforms[platform]
	if !ok || plat.Checksum == "" {
		return "", fmt.Errorf("Claude Code %s has no %s build", version, platform)
	}
	say(progress, fmt.Sprintf("Downloading Claude Code %s (%d MB)…", version, plat.Size>>20))
	f, err := os.CreateTemp("", "claude-*-"+binary)
	if err != nil {
		return "", err
	}
	_, err = download(ctx, claudeDist+"/"+version+"/"+platform+"/"+binary, f, plat.Checksum, progress)
	f.Close()
	if err == nil {
		err = os.Chmod(f.Name(), 0o755)
	}
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

type ghAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	// Digest is GitHub's checksum of the file, "sha256:<hex>".
	Digest string `json:"digest"`
}

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

func findAsset(rel ghRelease, name string) (ghAsset, bool) {
	for _, a := range rel.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return ghAsset{}, false
}

// sha256 is the asset's SHA-256 in hex, or "" when GitHub gave none.
func (a ghAsset) sha256() string {
	sum, ok := strings.CutPrefix(a.Digest, "sha256:")
	if !ok || len(sum) != 64 {
		return ""
	}
	return sum
}

// codexAsset is the release file with this machine's Codex CLI; Linux builds come packed.
func codexAsset() (asset, binary string, err error) {
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH]
	switch {
	case arch == "":
	case runtime.GOOS == "windows":
		return "codex-" + arch + "-pc-windows-msvc.exe", "codex.exe", nil
	case runtime.GOOS == "linux":
		return "codex-" + arch + "-unknown-linux-musl.tar.gz", "codex", nil
	}
	return "", "", fmt.Errorf("the Codex CLI has no build for %s/%s", runtime.GOOS, runtime.GOARCH)
}

// installCodex downloads the Codex CLI from its GitHub release. The npm package only wraps
// this same binary, so this skips Node.js. The file must match the checksum GitHub lists for it.
func installCodex(ctx context.Context, progress func(string)) error {
	target, binary, err := codexAsset()
	if err != nil {
		return err
	}
	var rel ghRelease
	if err := getJSON(ctx, codexAPI, &rel); err != nil {
		return err
	}
	a, ok := findAsset(rel, target)
	if !ok {
		return fmt.Errorf("the Codex release %s has no %s", rel.TagName, target)
	}
	sum := a.sha256()
	if sum == "" {
		return fmt.Errorf("GitHub gave no checksum for %s, so it wasn't installed", target)
	}
	dir := ToolsBin()
	if dir == "" {
		return errors.New("there's nowhere to install the Codex CLI")
	}
	say(progress, "Downloading the Codex CLI "+rel.TagName+"…")
	path := filepath.Join(dir, binary)
	if !strings.HasSuffix(target, ".tar.gz") {
		return downloadExe(ctx, a.URL, sum, path, progress)
	}
	return downloadTarred(ctx, a.URL, sum, strings.TrimSuffix(target, ".tar.gz"), path, progress)
}

// maxTool is the most a downloaded program may weigh.
const maxTool = 512 << 20

// downloadTarred saves one program out of a .tar.gz release whose SHA-256 is wantSHA. The
// program only replaces the old copy once the whole archive has been checked.
func downloadTarred(ctx context.Context, url, wantSHA, member, path string, progress func(string)) error {
	body, err := get(ctx, url)
	if err != nil {
		return err
	}
	defer body.Close()
	sum := sha256.New()
	archive := io.TeeReader(body, sum)
	gz, err := gzip.NewReader(&counter{r: archive, progress: progress})
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("%s isn't in the download", member)
		}
		if err != nil {
			return err
		}
		if filepath.Base(h.Name) != member || h.Typeflag != tar.TypeReg {
			continue
		}
		if h.Size > maxTool {
			return fmt.Errorf("%s is %d MB, more than a program should be", member, h.Size>>20)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		tmp := path + ".new"
		f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		n, err := io.Copy(f, tr)
		if err == nil && n != h.Size {
			err = fmt.Errorf("%s arrived cut short", member)
		}
		if err == nil {
			// The rest of the archive counts towards its checksum too.
			_, err = io.Copy(io.Discard, archive)
		}
		if err == nil && !strings.EqualFold(hex.EncodeToString(sum.Sum(nil)), wantSHA) {
			err = errChecksum
		}
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(tmp)
			return err
		}
		return os.Rename(tmp, path)
	}
}

// downloadExe saves a program next to the trainer's other tools, replacing any older copy
// once the whole download has arrived.
func downloadExe(ctx context.Context, url, wantSHA, path string, progress func(string)) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".new"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	_, err = download(ctx, url, f, wantSHA, progress)
	f.Close()
	if err != nil {
		os.Remove(tmp)
		return err
	}
	os.Remove(path)
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
