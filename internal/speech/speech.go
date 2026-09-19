// Package speech speaks tips aloud: on Windows (and from WSL) through SAPI in a long-lived
// PowerShell process, on Linux through speech-dispatcher or espeak-ng.
package speech

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf16"

	"dotatrainer/internal/hidewin"
)

// The script answers every line at once with one line: "ok", or for !voices the languages
// of the installed voices, for !lang whether a voice for it exists ("ok" or "none"), and for
// !busy whether it is still talking ("yes" or "no"). Speaking is asynchronous so that !cancel
// can cut a line short for something urgent. Voices Windows 10 and 11 add from Settings,
// Russian included, are only visible to WinRT, so that speaks when SAPI has no voice.
const script = `
[Console]::InputEncoding = [System.Text.Encoding]::UTF8
Add-Type -AssemblyName System.Speech
$s = New-Object System.Speech.Synthesis.SpeechSynthesizer
function Voices { $s.GetInstalledVoices() | Where-Object { $_.Enabled } }
$w = $null
try {
  Add-Type -AssemblyName System.Runtime.WindowsRuntime
  $null = [Windows.Media.SpeechSynthesis.SpeechSynthesizer,Windows.Media.SpeechSynthesis,ContentType=WindowsRuntime]
  $null = [Windows.Media.Playback.MediaPlayer,Windows.Media.Playback,ContentType=WindowsRuntime]
  $null = [Windows.Media.Core.MediaSource,Windows.Media.Core,ContentType=WindowsRuntime]
  $asTask = [System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object { $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation` + "`" + `1' } | Select-Object -First 1
  $w = New-Object Windows.Media.SpeechSynthesis.SpeechSynthesizer
  $p = New-Object Windows.Media.Playback.MediaPlayer
} catch { $w = $null }
function WinVoices { if ($w) { [Windows.Media.SpeechSynthesis.SpeechSynthesizer]::AllVoices } }
function Lang($tag) { $tag.Split('-')[0].ToLower() }
$useWin = $false
$preferWin = $false
$started = [DateTime]::MinValue
while ($true) {
  $line = [Console]::In.ReadLine()
  if ($line -eq $null) { break }
  $reply = 'ok'
  if ($line.StartsWith('!rate ')) {
    $r = [int]$line.Substring(6); $s.Rate = $r
    if ($w) { $w.Options.SpeakingRate = [Math]::Max(0.5, [Math]::Pow(3, $r / 10)) }
  }
  elseif ($line.StartsWith('!volume ')) { $vol = [int]$line.Substring(8); $s.Volume = $vol; if ($w) { $p.Volume = $vol / 100 } }
  elseif ($line -eq '!prefer winrt') { $preferWin = [bool]$w }
  elseif ($line -eq '!voices') {
    $langs = @(Voices | ForEach-Object { $_.VoiceInfo.Culture.TwoLetterISOLanguageName }) + @(WinVoices | ForEach-Object { Lang $_.Language })
    $reply = ($langs | Select-Object -Unique) -join ','
  }
  elseif ($line.StartsWith('!lang ')) {
    $want = $line.Substring(6)
    $v = Voices | Where-Object { $_.VoiceInfo.Culture.TwoLetterISOLanguageName -eq $want } | Select-Object -First 1
    $wv = WinVoices | Where-Object { (Lang $_.Language) -eq $want } | Select-Object -First 1
    if ($v -and -not ($preferWin -and $wv)) { $s.SelectVoice($v.VoiceInfo.Name); $useWin = $false }
    elseif ($wv) { $w.Voice = $wv; $useWin = $true }
    else { $reply = 'none' }
  }
  elseif ($line -eq '!busy') {
    $talking = $s.State -eq 'Speaking'
    if ($useWin) { $talking = ([DateTime]::Now - $started).TotalMilliseconds -lt 500 -or @('Opening', 'Buffering', 'Playing') -contains $p.PlaybackSession.PlaybackState.ToString() }
    if ($talking) { $reply = 'yes' } else { $reply = 'no' }
  }
  elseif ($line -eq '!cancel') { $s.SpeakAsyncCancelAll(); if ($w) { $p.Pause() } }
  elseif ($useWin) {
    $t = $asTask.MakeGenericMethod([Windows.Media.SpeechSynthesis.SpeechSynthesisStream]).Invoke($null, @($w.SynthesizeTextToStreamAsync($line)))
    $null = $t.Wait(-1)
    $p.Source = [Windows.Media.Core.MediaSource]::CreateFromStream($t.Result, $t.Result.ContentType)
    $p.Play()
    $started = [DateTime]::Now
  }
  else { $null = $s.SpeakAsync($line) }
  [Console]::Out.WriteLine($reply)
  [Console]::Out.Flush()
}
`

const (
	maxAge       = 8 * time.Second
	maxAgeUrgent = 12 * time.Second
	queueSize    = 6
)

type utterance struct {
	text     string
	fallback string // the English line, for when there is no voice for lang
	lang     string
	urgent   bool
	at       time.Time
}

// Backends.
const (
	sapi   = "sapi"
	spd    = "spd-say"
	espeak = "espeak"
)

type Speaker struct {
	exe     string
	backend string
	lang    string
	log     *slog.Logger

	mu     sync.Mutex
	queue  []utterance
	rate   int
	wake   chan struct{}
	closed bool

	cmd   *exec.Cmd
	stdin io.WriteCloser
	acks  *bufio.Scanner

	// voices are the languages Windows has voices for, learned when the process starts.
	voices  map[string]bool
	recheck bool
	gen     int // processes started, so the loop knows to set the voice up again

	lastText string // the last line said, and when, so it isn't said twice in a row
	lastAt   time.Time

	piper *piperEngine // natural voices on Linux, once downloaded
}

// repeatGap is how soon the same line may be said again.
const repeatGap = 6 * time.Second

// pollEvery is how often a line being spoken is checked for being done, or for something
// urgent that should cut it short.
const pollEvery = 150 * time.Millisecond

// urgentWaiting reports whether an urgent line is queued behind the one being said.
func (s *Speaker) urgentWaiting() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queue) > 0 && s.queue[0].urgent
}

func (s *Speaker) said(text string) {
	s.mu.Lock()
	s.lastText, s.lastAt = text, time.Now()
	s.mu.Unlock()
}

// Available finds a way to speak on this machine: Windows speech first, since it works from
// WSL too, then the Linux speech tools.
func Available() (string, bool) {
	for _, name := range []string{"powershell.exe", "spd-say", "espeak-ng", "espeak"} {
		if exe, err := exec.LookPath(name); err == nil {
			return exe, true
		}
	}
	return "", false
}

func backendOf(exe string) string {
	switch base := strings.ToLower(filepath.Base(exe)); {
	case exe == "":
		return ""
	case strings.HasPrefix(base, "spd-say"):
		return spd
	case strings.HasPrefix(base, "espeak"):
		return espeak
	}
	return sapi
}

func New(exe string, rate int, log *slog.Logger) *Speaker {
	s := &Speaker{exe: exe, backend: backendOf(exe), lang: "en", rate: rate, log: log, wake: make(chan struct{}, 1)}
	go s.loop()
	return s
}

// UsePiper speaks with Piper from now on, in the languages models has voices for; other
// languages still go to the Linux speech tool, if there is one.
func (s *Speaker) UsePiper(exe string, models map[string]string, player []string, tmp string) {
	e := &piperEngine{exe: exe, models: models, player: player, tmp: tmp, procs: map[string]*piperProc{}}
	s.mu.Lock()
	old := s.piper
	s.piper = e
	s.mu.Unlock()
	if old != nil {
		old.close()
	}
}

func (s *Speaker) piperEngine() *piperEngine {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.piper
}

// Name says what is speaking, for the dashboard.
func (s *Speaker) Name() string {
	if s.piperEngine() != nil {
		return "Piper natural voices"
	}
	switch s.backend {
	case spd:
		return "speech-dispatcher"
	case espeak:
		return "eSpeak NG"
	case "":
		return "Piper natural voices"
	}
	return "Windows speech"
}

// SetLanguage picks the voice language on Linux; Windows uses the system voice.
func (s *Speaker) SetLanguage(lang string) {
	s.mu.Lock()
	s.lang = lang
	s.mu.Unlock()
}

func (s *Speaker) Say(text string, urgent bool) {
	s.mu.Lock()
	lang := s.lang
	s.mu.Unlock()
	s.SayIn(lang, text, "", urgent)
}

// SayIn speaks text in lang. When the computer has no voice for lang, it speaks fallback, the
// English line, instead: an English voice reads Russian letters as nothing, only the digits.
func (s *Speaker) SayIn(lang, text, fallback string, urgent bool) {
	text = strings.Join(strings.Fields(text), " ")
	fallback = strings.Join(strings.Fields(fallback), " ")
	if text == "" {
		return
	}
	s.mu.Lock()
	for _, q := range s.queue {
		if q.text == text {
			s.mu.Unlock()
			return // already waiting to be said
		}
	}
	if text == s.lastText && time.Since(s.lastAt) < repeatGap {
		s.mu.Unlock()
		return
	}
	u := utterance{text: text, fallback: fallback, lang: lang, urgent: urgent, at: time.Now()}
	if urgent {
		i := 0
		for i < len(s.queue) && s.queue[i].urgent {
			i++
		}
		s.queue = append(s.queue[:i], append([]utterance{u}, s.queue[i:]...)...)
	} else {
		s.queue = append(s.queue, u)
	}
	if len(s.queue) > queueSize {
		s.queue = s.queue[:queueSize]
	}
	s.mu.Unlock()
	s.signal()
}

func (s *Speaker) SetRate(rate int) {
	s.mu.Lock()
	s.rate = rate
	s.mu.Unlock()
}

func (s *Speaker) Close() {
	s.mu.Lock()
	s.closed = true
	e := s.piper
	s.mu.Unlock()
	if e != nil {
		e.close()
	}
	s.signal()
}

func (s *Speaker) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Speaker) loop() {
	appliedRate, appliedLang, appliedGen := 1<<30, "", 0
	if s.backend == sapi {
		// Learn the installed voices up front, so the dashboard can warn about a missing one.
		s.hasVoice("_")
	}
	for range s.wake {
		s.mu.Lock()
		recheck := s.recheck
		s.recheck = false
		s.mu.Unlock()
		if recheck && s.backend == sapi {
			s.stop()
			s.hasVoice("_")
		}
		for {
			s.mu.Lock()
			if s.closed {
				s.mu.Unlock()
				s.stop()
				return
			}
			if len(s.queue) == 0 {
				s.mu.Unlock()
				break
			}
			u := s.queue[0]
			s.queue = s.queue[1:]
			rate, lang := s.rate, u.lang
			s.mu.Unlock()

			limit := maxAge
			if u.urgent {
				limit = maxAgeUrgent
			}
			if time.Since(u.at) > limit {
				continue
			}
			if e := s.piperEngine(); e != nil {
				if text, voice, ok := piperLine(e, u); ok {
					s.said(u.text)
					if err := s.sayPiper(e, text, rate, voice, u.urgent); err != nil {
						s.log.Warn("piper failed", "err", err)
					}
					continue
				}
			}
			if s.backend == "" {
				continue // nothing to speak with until Piper is downloaded
			}
			if s.backend != sapi {
				s.said(u.text)
				if err := s.sayOnce(u.text, rate, lang, u.urgent); err != nil {
					s.log.Warn("speech failed", "err", err)
				}
				continue
			}
			if s.cmd == nil {
				if err := s.start(); err != nil {
					s.log.Warn("speech unavailable", "err", err)
					continue
				}
			}
			if s.gen != appliedGen {
				// A new process starts with the default voice and rate.
				appliedRate, appliedLang, appliedGen = 1<<30, "", s.gen
			}
			if rate != appliedRate {
				if _, err := s.send(fmt.Sprintf("!rate %d", rate)); err != nil {
					s.log.Warn("speech unavailable", "err", err)
					continue
				}
				appliedRate = rate
			}
			text := u.text
			if lang != "en" && !hasLetters(text, lang) {
				lang = "en" // an English line, like the voice test, in a Russian app
			}
			if !s.hasVoice(lang) {
				if u.fallback == "" {
					// Reading it would only say the numbers; the line is on screen anyway.
					continue
				}
				lang, text = "en", u.fallback
			}
			if lang != appliedLang {
				if reply, err := s.send("!lang " + lang); err == nil && reply == "ok" {
					appliedLang = lang
				}
			}
			s.said(u.text)
			if _, err := s.send(text); err != nil {
				s.log.Warn("speech failed", "err", err)
				appliedRate, appliedLang = 1<<30, ""
				continue
			}
			s.waitSpoken(u.urgent)
		}
	}
}

// waitSpoken waits for Windows to finish the line, cutting it short when something urgent
// is queued behind a line that isn't.
func (s *Speaker) waitSpoken(urgent bool) {
	for {
		time.Sleep(pollEvery)
		if !urgent && s.urgentWaiting() {
			s.send("!cancel")
			return
		}
		if reply, err := s.send("!busy"); err != nil || reply != "yes" {
			return
		}
	}
}

// send writes one line to the speech process and returns its one-line reply.
func (s *Speaker) send(line string) (string, error) {
	if s.cmd == nil {
		if err := s.start(); err != nil {
			return "", err
		}
	}
	if _, err := io.WriteString(s.stdin, line+"\n"); err != nil {
		s.stop()
		return "", err
	}
	if !s.acks.Scan() {
		s.stop()
		return "", errors.New("speech process exited")
	}
	return strings.TrimSpace(s.acks.Text()), nil
}

// hasVoice reports whether there is a voice for lang. Linux speech tools carry their own
// languages; Windows depends on the voices installed, which are asked for once.
func (s *Speaker) hasVoice(lang string) bool {
	if s.backend != sapi || lang == "" || lang == "en" {
		return true
	}
	s.mu.Lock()
	known := s.voices
	s.mu.Unlock()
	if known == nil {
		known = map[string]bool{}
		if reply, err := s.send("!voices"); err == nil {
			for _, l := range strings.Split(reply, ",") {
				known[strings.TrimSpace(l)] = true
			}
		}
		s.mu.Lock()
		s.voices = known
		s.mu.Unlock()
	}
	return known[lang]
}

// hasLetters reports whether text is written in lang's alphabet, which for Russian means it
// has Cyrillic letters in it.
func hasLetters(text, lang string) bool {
	if lang != "ru" {
		return true
	}
	for _, r := range text {
		if unicode.Is(unicode.Cyrillic, r) {
			return true
		}
	}
	return false
}

// Languages lists the languages this machine can speak, once speech has started. Nil means
// it isn't known yet, or that any language works.
// Recheck asks Windows for its voices again, after one was installed.
func (s *Speaker) Recheck() {
	s.mu.Lock()
	s.recheck = true
	s.mu.Unlock()
	s.signal()
}

func (s *Speaker) Languages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.backend != sapi || s.voices == nil {
		return nil
	}
	var out []string
	for l := range s.voices {
		out = append(out, l)
	}
	return out
}

func (s *Speaker) start() error {
	cmd := exec.Command(s.exe, "-NoProfile", "-NonInteractive", "-EncodedCommand", EncodePowerShell(script))
	hidewin.Apply(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	s.cmd, s.stdin, s.acks = cmd, stdin, bufio.NewScanner(stdout)
	s.gen++
	return nil
}

func (s *Speaker) stop() {
	if s.cmd == nil {
		return
	}
	s.stdin.Close()
	done := make(chan struct{})
	go func() { s.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		s.cmd.Process.Kill()
	}
	s.cmd = nil
	s.mu.Lock()
	s.voices = nil
	s.mu.Unlock()
}

// sayOnce speaks one line with a Linux speech tool and waits for it to finish, so lines
// don't talk over each other; something urgent queued behind a line that isn't cuts it short.
func (s *Speaker) sayOnce(text string, rate int, lang string, urgent bool) error {
	var args []string
	switch s.backend {
	case spd:
		// spd-say rates run from -100 to 100; the trainer's from -10 to 10.
		args = []string{"-w", "-r", strconv.Itoa(rate * 10), "-l", lang, "--", text}
	case espeak:
		args = []string{"-s", strconv.Itoa(175 + rate*15), "-v", lang, "--", text}
	}
	cmd := exec.Command(s.exe, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	for {
		select {
		case err := <-done:
			return err
		case <-tick.C:
			if urgent || !s.urgentWaiting() {
				continue
			}
			cmd.Process.Kill()
			if s.backend == spd {
				// speech-dispatcher keeps playing what it was given; -C stops it.
				exec.Command(s.exe, "-C").Run()
			}
			<-done
			return nil
		}
	}
}

// piperLine is what Piper says for u and in which language's voice, or false when it has no
// voice for it.
func piperLine(e *piperEngine, u utterance) (string, string, bool) {
	switch {
	case u.lang != "en" && !hasLetters(u.text, u.lang):
		return u.text, "en", e.has("en") // an English line, like the voice test, in a Russian app
	case e.has(u.lang):
		return u.text, u.lang, true
	case u.fallback != "":
		return u.fallback, "en", e.has("en")
	}
	return "", "", false
}

// sayPiper speaks one line with Piper and waits for it to finish; something urgent queued
// behind a line that isn't cuts it short.
func (s *Speaker) sayPiper(e *piperEngine, text string, rate int, lang string, urgent bool) error {
	wav, err := e.synth(lang, text, rate)
	if err != nil {
		return err
	}
	defer os.Remove(wav)
	if !urgent && s.urgentWaiting() {
		return nil
	}
	return s.play(append(slices.Clone(e.player), wav), urgent)
}

// play runs a command that speaks or plays one line, killing it when something urgent waits.
func (s *Speaker) play(args []string, urgent bool) error {
	cmd := exec.Command(args[0], args[1:]...)
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	for {
		select {
		case err := <-done:
			return err
		case <-tick.C:
			if urgent || !s.urgentWaiting() {
				continue
			}
			cmd.Process.Kill()
			<-done
			return nil
		}
	}
}

// EncodePowerShell encodes a script for -EncodedCommand, which expects base64 of UTF-16LE.
func EncodePowerShell(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, len(u)*2)
	for i, c := range u {
		b[2*i], b[2*i+1] = byte(c), byte(c>>8)
	}
	return base64.StdEncoding.EncodeToString(b)
}
