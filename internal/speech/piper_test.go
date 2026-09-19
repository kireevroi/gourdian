package speech

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakePiper stands in for piper --json-input: it logs each line with its model and writes the
// WAV file it's asked for. The fake player logs what it plays.
func fakePiper(t *testing.T) (exe string, player []string, log string) {
	dir := t.TempDir()
	log = filepath.Join(dir, "log")
	exe = filepath.Join(dir, "piper")
	script := `#!/bin/sh
model="$2"
while IFS= read -r line; do
  out=$(printf '%s' "$line" | sed 's/.*"output_file":"\([^"]*\)".*/\1/')
  printf 'RIFF' > "$out"
  echo "$(basename "$model") $line" >> "$LOG"
  echo "$out"
done
`
	if err := os.WriteFile(exe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	play := filepath.Join(dir, "play")
	if err := os.WriteFile(play, []byte("#!/bin/sh\nhead -c4 \"$1\" > /dev/null && echo played >> \"$LOG\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOG", log)
	return exe, []string{play}, log
}

func TestPiperSpeaksInTheRightVoice(t *testing.T) {
	exe, player, log := fakePiper(t)
	s := New("", 0, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer s.Close()
	s.UsePiper(exe, map[string]string{"en": "en.onnx", "ru": "ru.onnx"}, player, t.TempDir())
	s.SayIn("ru", "Руна силы через 15 секунд", "Power rune in 15 seconds", false)
	s.SayIn("ru", "Dota trainer voice check", "", false)
	var got string
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		b, _ := os.ReadFile(log)
		if got = string(b); strings.Count(got, "played") == 2 {
			break
		}
	}
	if !strings.Contains(got, `ru.onnx {"output_file"`) || !strings.Contains(got, "Руна силы через 15 секунд") ||
		!strings.Contains(got, `en.onnx {"output_file"`) || !strings.Contains(got, "Dota trainer voice check") || strings.Count(got, "played") != 2 {
		t.Fatalf("log:\n%s", got)
	}
	if s.Name() != "Piper natural voices" {
		t.Fatalf("name = %q", s.Name())
	}
}

func TestPiperFallsBackToEnglish(t *testing.T) {
	e := &piperEngine{models: map[string]string{"en": "en.onnx"}}
	if text, lang, ok := piperLine(e, utterance{text: "Руна силы", fallback: "Power rune", lang: "ru"}); !ok || lang != "en" || text != "Power rune" {
		t.Fatalf("without a Russian voice: %q %q %v", text, lang, ok)
	}
	if _, _, ok := piperLine(e, utterance{text: "Руна силы", lang: "ru"}); ok {
		t.Fatal("a Russian line with no English one goes to the other speech tool")
	}
	if lengthScale(0) != 1 || lengthScale(10) >= lengthScale(0) || lengthScale(-10) <= 1 {
		t.Fatalf("length scales %v %v %v", lengthScale(-10), lengthScale(0), lengthScale(10))
	}
}

func TestDownloadChecksTheChecksum(t *testing.T) {
	body := []byte("voice model")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }))
	defer srv.Close()
	sum := sha256.Sum256(body)
	dir := t.TempDir()
	path, err := download(t.Context(), srv.URL, hex.EncodeToString(sum[:]), dir, func(int64) {})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, body) {
		t.Fatalf("downloaded %q", got)
	}
	os.Remove(path)
	if _, err := download(t.Context(), srv.URL, strings.Repeat("0", 64), dir, func(int64) {}); err == nil {
		t.Fatal("a file with the wrong checksum is refused")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*.part")); len(left) != 0 {
		t.Fatalf("partial downloads are removed: %v", left)
	}
}

func tarball(t *testing.T, entries []tar.Header) string {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, h := range entries {
		body := "x"
		if h.Typeflag == tar.TypeReg {
			h.Size = int64(len(body))
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			tw.Write([]byte(body))
		}
	}
	tw.Close()
	gz.Close()
	path := filepath.Join(t.TempDir(), "a.tgz")
	os.WriteFile(path, buf.Bytes(), 0o644)
	return path
}

func TestUntarKeepsInsideTheFolder(t *testing.T) {
	good := tarball(t, []tar.Header{
		{Name: "piper/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "piper/piper", Typeflag: tar.TypeReg, Mode: 0o755},
		{Name: "piper/libx.so.1", Typeflag: tar.TypeReg, Mode: 0o644},
		{Name: "piper/libx.so", Typeflag: tar.TypeSymlink, Linkname: "libx.so.1"},
	})
	dest := t.TempDir()
	if err := untar(good, dest); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Join(dest, "piper", "piper")); err != nil || fi.Mode()&0o100 == 0 {
		t.Fatalf("the binary is executable: %v %v", fi, err)
	}
	if target, err := os.Readlink(filepath.Join(dest, "piper", "libx.so")); err != nil || target != "libx.so.1" {
		t.Fatalf("link = %q %v", target, err)
	}
	for _, bad := range [][]tar.Header{
		{{Name: "../evil", Typeflag: tar.TypeReg, Mode: 0o644}},
		{{Name: "piper/lib", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}},
		{{Name: "piper/lib", Typeflag: tar.TypeSymlink, Linkname: "../../evil"}},
	} {
		if err := untar(tarball(t, bad), t.TempDir()); err == nil {
			t.Errorf("%s was unpacked", bad[0].Name)
		}
	}
}

// TestLivePiper downloads Piper and a voice and speaks a line into a WAV file.
func TestLivePiper(t *testing.T) {
	home := os.Getenv("LIVE_PIPER")
	if home == "" {
		t.Skip("set LIVE_PIPER to a folder to download Piper and the Russian voice into")
	}
	var last Progress
	if err := InstallPiper(t.Context(), home, []string{"ru_RU-irina-medium"}, func(p Progress) { last = p }); err != nil {
		t.Fatal(err)
	}
	t.Logf("last progress: %+v", last)
	e := &piperEngine{exe: PiperBinary(home), models: map[string]string{"ru": voicePath(home, "ru_RU-irina-medium")}, tmp: t.TempDir(), procs: map[string]*piperProc{}}
	defer e.close()
	start := time.Now()
	wav, err := e.synth("ru", "У вас есть нераспределённое очко умений.", 0)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(wav)
	if len(b) < 10000 || string(b[:4]) != "RIFF" {
		t.Fatalf("%s is %d bytes", wav, len(b))
	}
	t.Logf("spoke %d bytes of WAV in %s with %s", len(b), time.Since(start), e.exe)
}
