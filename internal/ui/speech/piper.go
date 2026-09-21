package speech

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

// Piper (github.com/rhasspy/piper) speaks with neural voices, offline and fast on a CPU. The
// trainer downloads its Linux build and the voices, pinned to these versions and checksums.
const (
	piperRelease = "https://github.com/rhasspy/piper/releases/download/2023.11.14-2/"
	piperVoices  = "https://huggingface.co/rhasspy/piper-voices/resolve/v1.0.0/"
)

var piperArchives = map[string]struct {
	file, sha string
	size      int64
}{
	"amd64": {"piper_linux_x86_64.tar.gz", "a50cb45f355b7af1f6d758c1b360717877ba0a398cc8cbe6d2a7a3a26e225992", 26460462},
	"arm64": {"piper_linux_aarch64.tar.gz", "fea0fd2d87c54dbc7078d0f878289f404bd4d6eea6e7444a77835d1537ab88eb", 26004717},
}

// PiperVoice is a voice the trainer can download.
type PiperVoice struct {
	ID        string `json:"id"`
	Lang      string `json:"lang"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	Installed bool   `json:"installed"`
	repo      string // path in the voices repository, without the extension
	sha       string // of the .onnx model
	configSHA string // of its .onnx.json
}

var piperCatalog = []PiperVoice{
	{ID: "en_US-lessac-medium", Lang: "en", Name: "Lessac, American", Size: 63201294, repo: "en/en_US/lessac/medium/en_US-lessac-medium",
		sha: "5efe09e69902187827af646e1a6e9d269dee769f9877d17b16b1b46eeaaf019f", configSHA: "efe19c417bed055f2d69908248c6ba650fa135bc868b0e6abb3da181dab690a0"},
	{ID: "en_US-amy-medium", Lang: "en", Name: "Amy, American", Size: 63201294, repo: "en/en_US/amy/medium/en_US-amy-medium",
		sha: "b3a6e47b57b8c7fbe6a0ce2518161a50f59a9cdd8a50835c02cb02bdd6206c18", configSHA: "95a23eb4d42909d38df73bb9ac7f45f597dbfcde2d1bf9526fdeaf5466977d77"},
	{ID: "en_US-ryan-medium", Lang: "en", Name: "Ryan, American", Size: 63201294, repo: "en/en_US/ryan/medium/en_US-ryan-medium",
		sha: "abf4c274862564ed647ba0d2c47f8ee7c9b717d27bdad9219100eb310db4047a", configSHA: "44034c056cb15681b2ad494307c7f3f2e4499d1253c700c711fa0a4607ffe78d"},
	{ID: "en_GB-alan-medium", Lang: "en", Name: "Alan, British", Size: 63201294, repo: "en/en_GB/alan/medium/en_GB-alan-medium",
		sha: "0a309668932205e762801f1efc2736cd4b0120329622adf62be09e56339d3330", configSHA: "c0f0d124e5895c00e7c03b35dcc8287f319a6998a365b182deb5c8e752ee8c1e"},
	{ID: "ru_RU-irina-medium", Lang: "ru", Name: "Ирина", Size: 63201294, repo: "ru/ru_RU/irina/medium/ru_RU-irina-medium",
		sha: "8ff38212d23da300bbe3705c645e6e5b9475f0bfde01558eb17813e22acaaaaa", configSHA: "c2ec28bb38e2b59e93b959b3e40348c1afebbd272f30fed5d41205d08e98a9d7"},
	{ID: "ru_RU-dmitri-medium", Lang: "ru", Name: "Дмитрий", Size: 63201294, repo: "ru/ru_RU/dmitri/medium/ru_RU-dmitri-medium",
		sha: "f073356ebc4bd0f80c5af58df2953a5988bd5bdab1eb38635ce960b071fbefcb", configSHA: "667ef3117bc642c2892dff7690d8bdc8ca4228aeaa783b2dc1416df632855e0d"},
	{ID: "ru_RU-ruslan-medium", Lang: "ru", Name: "Руслан", Size: 63201294, repo: "ru/ru_RU/ruslan/medium/ru_RU-ruslan-medium",
		sha: "72a5f88e0b20928064eb45d88e1daa21f8af62d18613580d32cbb4aed48dcf7f", configSHA: "706a4fb17bc304abd07809b552deae615e64dcbffbfbd09854ba37ca59e88117"},
	{ID: "ru_RU-denis-medium", Lang: "ru", Name: "Денис", Size: 63201294, repo: "ru/ru_RU/denis/medium/ru_RU-denis-medium",
		sha: "15fab56e11a097858ee115545d0f697fc2a316c41a291a5362349fb870411b0a", configSHA: "831c860dac0b5073eaa81610a0a638ec23d90a6cf8e5f871b4485c2cec3767c8"},
}

// DefaultPiperVoice is the voice for a language until the player picks another.
func DefaultPiperVoice(lang string) string {
	if lang == "ru" {
		return "ru_RU-irina-medium"
	}
	return "en_US-lessac-medium"
}

func piperVoice(id string) (PiperVoice, bool) {
	i := slices.IndexFunc(piperCatalog, func(v PiperVoice) bool { return v.ID == id })
	if i < 0 {
		return PiperVoice{}, false
	}
	return piperCatalog[i], true
}

// PiperCatalog lists the voices, marking the ones in home.
func PiperCatalog(home string) []PiperVoice {
	out := slices.Clone(piperCatalog)
	for i := range out {
		out[i].Installed = voiceInstalled(home, out[i])
	}
	return out
}

func voicePath(home, id string) string { return filepath.Join(home, "voices", id+".onnx") }

func voiceInstalled(home string, v PiperVoice) bool {
	fi, err := os.Stat(voicePath(home, v.ID))
	_, cfgErr := os.Stat(voicePath(home, v.ID) + ".json")
	return err == nil && fi.Size() == v.Size && cfgErr == nil
}

// PiperBinary is the trainer's own Piper, else one from the system (the AUR's piper-tts), else "".
func PiperBinary(home string) string {
	own := filepath.Join(home, "bin", "piper", "piper")
	if fi, err := os.Stat(own); err == nil && fi.Mode()&0o111 != 0 {
		return own
	}
	for _, name := range []string{"piper-tts", "piper"} {
		exe, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		// Arch's "piper" package is a mouse configuration tool, not this.
		help, _ := exec.Command(exe, "--help").CombinedOutput()
		if strings.Contains(string(help), "--json-input") {
			return exe
		}
	}
	return ""
}

// Player is a command that plays a WAV file given after it, or nil when there's none.
func Player() []string {
	for _, p := range [][]string{{"pw-play"}, {"paplay"}, {"aplay", "-q"}, {"ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet"}} {
		if exe, err := exec.LookPath(p[0]); err == nil {
			return append([]string{exe}, p[1:]...)
		}
	}
	return nil
}

// Progress is how far a Piper download has got.
type Progress struct {
	What        string
	Done, Total int64
}

// InstallPiper downloads Piper, unless there is one already, and the voices, checking every
// file against its pinned checksum.
func InstallPiper(ctx context.Context, home string, voices []string, progress func(Progress)) error {
	type file struct {
		what, url, sha, dest string
		size                 int64
		unpack               bool
	}
	var files []file
	if PiperBinary(home) == "" {
		a, ok := piperArchives[runtime.GOARCH]
		if !ok {
			//lint:ignore ST1005 the setup page shows this as a sentence, and it starts with a name
			return fmt.Errorf("Piper has no build for %s", runtime.GOARCH)
		}
		files = append(files, file{what: "Piper", url: piperRelease + a.file, sha: a.sha, dest: filepath.Join(home, "bin"), size: a.size, unpack: true})
	}
	for _, id := range voices {
		v, ok := piperVoice(id)
		if !ok {
			return fmt.Errorf("unknown voice %q", id)
		}
		if voiceInstalled(home, v) {
			continue
		}
		files = append(files, file{what: v.Name + " (config)", url: piperVoices + v.repo + ".onnx.json", sha: v.configSHA, dest: voicePath(home, v.ID) + ".json"},
			file{what: v.Name, url: piperVoices + v.repo + ".onnx", sha: v.sha, dest: voicePath(home, v.ID), size: v.Size})
	}
	var total, done int64
	for _, f := range files {
		total += f.size
	}
	for _, f := range files {
		report := func(n int64) {
			if progress != nil {
				progress(Progress{What: f.what, Done: done + n, Total: total})
			}
		}
		tmp, err := download(ctx, f.url, f.sha, filepath.Join(home, "downloads"), report)
		if err != nil {
			return fmt.Errorf("%s: %w", f.what, err)
		}
		if f.unpack {
			// Unpacked beside the old copy and swapped in whole, so a failure leaves no half of it.
			fresh := f.dest + ".new"
			os.RemoveAll(fresh)
			if err = untar(tmp, fresh); err == nil {
				os.RemoveAll(f.dest)
				err = os.Rename(fresh, f.dest)
			}
			os.Remove(tmp)
		} else if err = os.MkdirAll(filepath.Dir(f.dest), 0o755); err == nil {
			err = os.Rename(tmp, f.dest)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", f.what, err)
		}
		done += f.size
	}
	return nil
}

// download saves url into dir and checks its SHA-256, reporting bytes as they arrive.
func download(ctx context.Context, url, sum, dir string, report func(int64)) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: %s", url, resp.Status)
	}
	f, err := os.CreateTemp(dir, "*.part")
	if err != nil {
		return "", err
	}
	h := sha256.New()
	var n int64
	last := time.Time{}
	buf := make([]byte, 256<<10)
	for {
		k, rerr := resp.Body.Read(buf)
		if k > 0 {
			if _, err := f.Write(buf[:k]); err != nil {
				f.Close()
				os.Remove(f.Name())
				return "", err
			}
			h.Write(buf[:k])
			n += int64(k)
			if time.Since(last) > 500*time.Millisecond {
				report(n)
				last = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(f.Name())
			return "", rerr
		}
	}
	report(n)
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != sum {
		os.Remove(f.Name())
		return "", fmt.Errorf("download %s: checksum %s, want %s", url, got, sum)
	}
	return f.Name(), nil
}

// untar unpacks a .tar.gz into dest, refusing paths that would land outside it.
func untar(path, dest string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	root := filepath.Clean(dest) + string(os.PathSeparator)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(dest, hdr.Name)
		if !strings.HasPrefix(target+string(os.PathSeparator), root) {
			return fmt.Errorf("unsafe path %q in archive", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(target, 0o755)
		case tar.TypeReg:
			err = writeFile(target, tr, os.FileMode(hdr.Mode)&0o755|0o644)
		case tar.TypeSymlink:
			if filepath.IsAbs(hdr.Linkname) || !strings.HasPrefix(filepath.Join(filepath.Dir(target), hdr.Linkname)+string(os.PathSeparator), root) {
				return fmt.Errorf("unsafe link %q in archive", hdr.Name)
			}
			os.Remove(target)
			if err = os.MkdirAll(filepath.Dir(target), 0o755); err == nil {
				err = os.Symlink(hdr.Linkname, target)
			}
		}
		if err != nil {
			return err
		}
	}
}

func writeFile(path string, r io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// piperEngine turns lines into WAV files with one Piper process per voice, which keeps the
// voice loaded between lines, and plays them.
type piperEngine struct {
	exe    string
	models map[string]string // language -> model file
	player []string
	tmp    string

	mu    sync.Mutex
	procs map[string]*piperProc
}

type piperProc struct {
	cmd   *exec.Cmd
	in    io.WriteCloser
	out   *bufio.Scanner
	scale float64
}

// lengthScale slows or speeds the voice like the other engines' rate, -10 to 10.
func lengthScale(rate int) float64 {
	return math.Round(min(max(math.Pow(3, -float64(rate)/10), 0.4), 2)*100) / 100
}

func (e *piperEngine) has(lang string) bool { _, ok := e.models[lang]; return ok }

// synth writes text to a WAV file with the language's voice and returns its path.
func (e *piperEngine) synth(lang, text string, rate int) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	scale := lengthScale(rate)
	p := e.procs[lang]
	if p != nil && p.scale != scale {
		p.stop()
		p = nil
	}
	if p == nil {
		cmd := exec.Command(e.exe, "--model", e.models[lang], "--json-input", "--quiet", "--length_scale", fmt.Sprint(scale), "--sentence_silence", "0.1")
		in, err := cmd.StdinPipe()
		if err != nil {
			return "", err
		}
		out, err := cmd.StdoutPipe()
		if err != nil {
			return "", err
		}
		if err := cmd.Start(); err != nil {
			return "", err
		}
		p = &piperProc{cmd: cmd, in: in, out: bufio.NewScanner(out), scale: scale}
		e.procs[lang] = p
	}
	wav := filepath.Join(e.tmp, fmt.Sprintf("line-%d.wav", time.Now().UnixNano()))
	line, _ := json.Marshal(map[string]string{"text": text, "output_file": wav})
	if _, err := p.in.Write(append(line, '\n')); err != nil {
		p.stop()
		delete(e.procs, lang)
		return "", err
	}
	if !p.out.Scan() {
		p.stop()
		delete(e.procs, lang)
		return "", errors.New("piper exited")
	}
	return wav, nil
}

func (p *piperProc) stop() {
	p.in.Close()
	done := make(chan struct{})
	go func() { p.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		p.cmd.Process.Kill()
	}
}

func (e *piperEngine) close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for lang, p := range e.procs {
		p.stop()
		delete(e.procs, lang)
	}
}
