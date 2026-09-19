package server

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"slices"
	"time"

	"gourdian/internal/hidewin"
	"gourdian/internal/speech"
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
	if nativeLinux() {
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
	if !s.voiceBusy.CompareAndSwap(false, true) {
		http.Error(w, "the voice is already being installed", http.StatusConflict)
		return
	}
	s.spawn(func(context.Context) { s.installVoice(ps, lang, locale) })
	writeJSON(w, voiceStatus{State: "running", Text: "Windows is asking for permission to add the voice."})
}

func (s *Server) installVoice(ps, lang, locale string) {
	defer s.voiceBusy.Store(false)
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
			s.hub.publish("settings", s.settingsResponse())
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
	if nativeLinux() {
		s.usePiper()
		writeJSON(w, s.settingsResponse())
		return
	}
	s.recheckVoices(s.cfg.Settings().Language)
	writeJSON(w, s.settingsResponse())
}
