package ai

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// claudeDownloads serves what downloads.claude.ai does: a version, a manifest and a binary.
func claudeDownloads(t *testing.T, body, checksum string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "2.1.274\n") })
	mux.HandleFunc("/2.1.274/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		platforms := map[string]any{}
		for _, p := range []string{"win32-x64", "win32-arm64", "linux-x64", "linux-arm64", "darwin-x64", "darwin-arm64"} {
			platforms[p] = map[string]any{"checksum": checksum, "size": int64(len(body))}
		}
		json.NewEncoder(w).Encode(map[string]any{"version": "2.1.274", "platforms": platforms})
	})
	mux.HandleFunc("/2.1.274/win32-", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/claude.exe") || strings.HasSuffix(r.URL.Path, "/claude") {
			fmt.Fprint(w, body)
			return
		}
		http.NotFound(w, r)
	})
	return httptest.NewServer(mux)
}

func TestDownloadClaudeChecksTheChecksum(t *testing.T) {
	body := "not really a binary"
	sum := sha256.Sum256([]byte(body))
	srv := claudeDownloads(t, body, hex.EncodeToString(sum[:]))
	defer srv.Close()
	claudeDist = srv.URL
	path, err := downloadClaude(t.Context(), nil)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer os.Remove(path)
	if got, _ := os.ReadFile(path); string(got) != body {
		t.Fatalf("saved %q", got)
	}
}

func TestDownloadClaudeRefusesAWrongChecksum(t *testing.T) {
	srv := claudeDownloads(t, "tampered", strings.Repeat("ab", 32))
	defer srv.Close()
	claudeDist = srv.URL
	if _, err := downloadClaude(t.Context(), nil); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("error = %v", err)
	}
}

func TestCodexAssetPicksThisPCsBuild(t *testing.T) {
	rel := ghRelease{TagName: "rust-v0.154.0", Assets: []ghAsset{
		{Name: "codex-aarch64-pc-windows-msvc.exe", URL: "arm"},
		{Name: "codex-x86_64-pc-windows-msvc.exe", URL: "x64"},
		{Name: "codex-x86_64-unknown-linux-musl.tar.gz", URL: "linux"},
	}}
	if got := assetURL(rel, "codex-x86_64-pc-windows-msvc.exe"); got != "x64" {
		t.Fatalf("url = %q", got)
	}
	if got := assetURL(rel, "codex-riscv.exe"); got != "" {
		t.Fatalf("unknown asset gave %q", got)
	}
}

func TestDownloadExeReplacesTheOldCopy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "new build")
	}))
	defer srv.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "tools", "codex.exe")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("old build"), 0o755)
	if err := downloadExe(t.Context(), srv.URL, path, nil); err != nil {
		t.Fatalf("download: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "new build" {
		t.Fatalf("file = %q", got)
	}
	if _, err := os.Stat(path + ".new"); !os.IsNotExist(err) {
		t.Fatal("the part-downloaded file was left behind")
	}
}

func TestToolsBinIsFoundBeforeTheHomeFolder(t *testing.T) {
	dir := t.TempDir()
	SetToolsDir(dir)
	defer SetToolsDir("")
	name := "codex"
	if runtime.GOOS == "windows" {
		name = "codex.exe"
	}
	os.MkdirAll(filepath.Join(dir, "tools"), 0o755)
	exe := filepath.Join(dir, "tools", name)
	os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755)
	if got := findCLI("", "codex"); got != exe {
		t.Fatalf("findCLI = %q, want %q", got, exe)
	}
}

func TestCodexTarballGivesTheProgram(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	content := []byte("#!/bin/sh\necho codex\n")
	tw.WriteHeader(&tar.Header{Name: "codex-x86_64-unknown-linux-musl", Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg})
	tw.Write(content)
	tw.Close()
	gz.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(buf.Bytes()) }))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "tools", "codex")
	if err := downloadTarred(t.Context(), srv.URL, "codex-x86_64-unknown-linux-musl", path, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(content) {
		t.Fatalf("saved %q", got)
	}
}
