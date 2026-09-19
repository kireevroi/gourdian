package speech

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRussianLettersAreRecognised(t *testing.T) {
	for text, want := range map[string]bool{
		"Водяные руны через 15 секунд":                        true,
		"Dota trainer voice check. Power rune in 15 seconds.": false,
		"15": false,
	} {
		if got := hasLetters(text, "ru"); got != want {
			t.Errorf("hasLetters(%q) = %v", text, got)
		}
	}
	if !hasLetters("anything", "en") {
		t.Error("English lines always count as English")
	}
}

func TestBackendFromTheProgramFound(t *testing.T) {
	for exe, want := range map[string]string{
		"/usr/bin/spd-say":   spd,
		"/usr/bin/espeak-ng": espeak,
		`C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe`: sapi,
	} {
		if got := backendOf(exe); got != want {
			t.Errorf("backendOf(%q) = %s, want %s", exe, got, want)
		}
	}
}

// fakeEspeak writes a stand-in for espeak-ng that takes a while to "say" each line and logs it.
func fakeEspeak(t *testing.T, seconds string) (exe, log string) {
	t.Helper()
	dir := t.TempDir()
	log = filepath.Join(dir, "said.log")
	exe = filepath.Join(dir, "espeak-ng")
	script := "#!/bin/sh\nfor last; do :; done\necho \"start $last\" >> " + log + "\nsleep " + seconds + "\necho \"done $last\" >> " + log + "\n"
	if err := os.WriteFile(exe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe, log
}

func readLog(t *testing.T, path string) string {
	data, _ := os.ReadFile(path)
	return string(data)
}

func TestUrgentLineCutsInOnLinux(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the speech tool")
	}
	exe, log := fakeEspeak(t, "2")
	s := New(exe, 0, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer s.Close()
	s.Say("A long reminder about stacking camps", false)
	time.Sleep(300 * time.Millisecond)
	s.Say("Low health. Back off", true)
	time.Sleep(3 * time.Second)
	got := readLog(t, log)
	if strings.Contains(got, "done A long reminder") {
		t.Fatalf("the reminder finished instead of being cut short:\n%s", got)
	}
	if !strings.Contains(got, "start Low health") {
		t.Fatalf("the urgent line wasn't said:\n%s", got)
	}
}

func TestTheSameLineIsntSaidTwice(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the speech tool")
	}
	exe, log := fakeEspeak(t, "0.2")
	s := New(exe, 0, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer s.Close()
	for range 3 {
		s.Say("Stack a camp", false)
	}
	time.Sleep(time.Second)
	s.Say("Stack a camp", false) // within the repeat gap
	time.Sleep(time.Second)
	if n := strings.Count(readLog(t, log), "start Stack a camp"); n != 1 {
		t.Fatalf("said %d times:\n%s", n, readLog(t, log))
	}
}

// TestWindowsScript runs the real speech script without saying anything; it needs Windows
// PowerShell, so it only runs when asked to.
func TestWindowsScript(t *testing.T) {
	exe, err := exec.LookPath("powershell.exe")
	if err != nil || os.Getenv("WINDOWS_SPEECH") == "" {
		t.Skip("set WINDOWS_SPEECH=1 on Windows or WSL")
	}
	s := &Speaker{exe: exe, backend: sapi}
	defer s.stop()
	for _, line := range []string{"!voices", "!rate 2", "!lang en", "!busy", "!lang ru", "!lang xx"} {
		reply, err := s.send(line)
		if err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		t.Logf("%-10s -> %s", line, reply)
	}
	// Say a line through WinRT, silently, and wait for it to finish.
	for _, line := range []string{"!volume 0", "!prefer winrt", "!lang en", "Power runes in fifteen seconds"} {
		if reply, err := s.send(line); err != nil || reply != "ok" {
			t.Fatalf("%s: %q, %v", line, reply, err)
		}
	}
	var busy []string
	for range 20 {
		reply, _ := s.send("!busy")
		busy = append(busy, reply)
		if reply == "no" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if len(busy) < 3 || busy[len(busy)-1] != "no" {
		t.Fatalf("WinRT speech should be busy for a while, then done: %v", busy)
	}
}

func TestANewVoiceIsFoundAndUsed(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "powershell.exe")
	fake := `#!/bin/sh
while IFS= read -r line; do
  echo "$line" >> "$FAKE_DIR/said"
  case "$line" in
    '!voices') if [ -f "$FAKE_DIR/ru" ]; then echo en,ru; else echo en; fi ;;
    '!busy') echo no ;;
    '!lang ru') if [ -f "$FAKE_DIR/ru" ]; then echo ok; else echo none; fi ;;
    *) echo ok ;;
  esac
done
`
	if err := os.WriteFile(exe, []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_DIR", dir)
	s := New(exe, 0, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer s.Close()
	eventually := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			if ok() {
				return
			}
		}
		t.Fatal(what)
	}
	eventually("the voices were never learned", func() bool { return len(s.Languages()) == 1 })
	os.WriteFile(filepath.Join(dir, "ru"), nil, 0o644)
	s.Recheck()
	eventually("the Russian voice installed later wasn't found", func() bool { return slices.Contains(s.Languages(), "ru") })
	s.SayIn("ru", "Руна силы через 15 секунд", "Power rune in 15 seconds", false)
	eventually("the Russian line wasn't said with the Russian voice", func() bool {
		said, _ := os.ReadFile(filepath.Join(dir, "said"))
		lines := strings.Split(string(said), "\n")
		i := slices.Index(lines, "!lang ru")
		return i >= 0 && slices.Index(lines, "Руна силы через 15 секунд") > i
	})
	// A restarted process starts on the default voice, so the next line picks Russian again.
	s.Recheck()
	eventually("the voices weren't asked for again", func() bool {
		said, _ := os.ReadFile(filepath.Join(dir, "said"))
		return strings.Count(string(said), "!voices") >= 3
	})
	s.SayIn("ru", "Водяные руны через 15 секунд", "Water runes in 15 seconds", false)
	eventually("after a restart the Russian line went out on the default voice", func() bool {
		said, _ := os.ReadFile(filepath.Join(dir, "said"))
		lines := strings.Split(string(said), "\n")
		last := slices.Index(lines, "Водяные руны через 15 секунд")
		return last > 0 && slices.Contains(lines[slices.Index(lines, "Руна силы через 15 секунд"):last], "!lang ru")
	})
}
