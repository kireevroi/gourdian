package server

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"slices"
	"time"

	"gourdian/internal/sys/config"
	"gourdian/internal/sys/hidewin"
	"gourdian/internal/sys/platform"
	"gourdian/internal/ui/speech"
)

// voiceLocales are the Windows speech packs for the languages the trainer speaks besides English.
var voiceLocales = map[string]string{"ru": "ru-RU"}

type voiceStatus struct {
	State string `json:"state"` // running, done or failed
	Text  string `json:"text"`
}

// handleVoiceInstall adds Windows' voice for the trainer's language. Windows asks for an
// administrator's OK first; when that doesn't work, Windows Settings opens at the speech page.
func (s *Server) handleVoiceInstall(w http.ResponseWriter, r *http.Request) {
	if platform.LinuxDesktop() {
		if !s.installPiper() {
			http.Error(w, "the natural voice is already downloading", http.StatusConflict)
			return
		}
		writeJSON(w, voiceStatus{State: "running", Text: "Downloading the natural voice."})
		return
	}
	lang := s.cfg.Settings().Language
	locale, ok := voiceLocales[lang]
	ps, err := exec.LookPath("powershell.exe")
	if !ok || err != nil || s.speaker == nil {
		http.Error(w, "voices can only be added here on Windows", http.StatusBadRequest)
		return
	}
	if !s.voice.windows.CompareAndSwap(false, true) {
		http.Error(w, "the voice is already being installed", http.StatusConflict)
		return
	}
	s.bg.Go(func(context.Context) { s.installVoice(ps, lang, locale) })
	writeJSON(w, voiceStatus{State: "running", Text: "Windows is asking for permission to add the voice."})
}

func (s *Server) installVoice(ps, lang, locale string) {
	defer s.voice.windows.Store(false)
	s.hub.publish("voice_install", voiceStatus{State: "running", Text: "Installing the Russian voice. Windows downloads it, which can take a few minutes."})
	elevated := fmt.Sprintf(`$p = Start-Process powershell.exe -Verb RunAs -Wait -PassThru -WindowStyle Hidden `+
		`-ArgumentList '-NoProfile','-Command','Add-WindowsCapability -Online -Name Language.TextToSpeech~~~%s~0.0.1.0'; exit $p.ExitCode`, locale)
	cmd := exec.Command(ps, "-NoProfile", "-NonInteractive", "-EncodedCommand", speech.EncodePowerShell(elevated))
	hidewin.Apply(cmd)
	err := cmd.Run()
	if s.recheckVoices(lang) {
		s.log.Info("voice installed", "lang", lang)
		s.hub.publish("voice_install", voiceStatus{State: "done", Text: "The Russian voice is installed. Tips are spoken in Russian now."})
		return
	}
	s.log.Warn("voice install didn't add a voice", "lang", lang, "err", err)
	settings := exec.Command("explorer.exe", "ms-settings:speech")
	hidewin.Apply(settings)
	_ = settings.Start()
	s.hub.publish("voice_install", voiceStatus{State: "failed",
		Text: "Windows didn't add the voice. Windows Settings is open at Speech: click Add voices, choose Russian, then press Check again."})
}

// recheckVoices asks Windows for its voices again and reports whether lang has one now.
func (s *Server) recheckVoices(lang string) bool {
	s.speaker.Recheck()
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(250 * time.Millisecond) {
		if langs := s.speaker.Languages(); langs != nil {
			found := slices.Contains(langs, lang)
			s.publishSettings()
			return found
		}
	}
	return false
}

func (s *Server) handleVoiceRecheck(w http.ResponseWriter, r *http.Request) {
	if s.speaker == nil {
		http.Error(w, "nothing speaks on this machine", http.StatusBadRequest)
		return
	}
	if platform.LinuxDesktop() {
		s.usePiper()
		writeJSON(w, s.settingsResponse())
		return
	}
	s.recheckVoices(s.cfg.Settings().Language)
	writeJSON(w, s.settingsResponse())
}

func (s *Server) handleVoiceTest(w http.ResponseWriter, r *http.Request) {
	set := s.cfg.Settings()
	if set.Voice == config.VoiceSystem && s.speaker != nil {
		english := "Gourdian voice check. Power rune in 15 seconds."
		if set.Language == "ru" {
			s.speaker.SayIn("ru", "Проверка голоса. Руна силы через 15 секунд.", english, true)
		} else {
			s.speaker.Say(english, true)
		}
	}
	writeJSON(w, map[string]string{"voice": set.Voice})
}
