package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"dotatrainer/internal/config"
	"dotatrainer/internal/speech"
)

// naturalVoice is the state of Piper's voices on a Linux desktop, for the dashboard.
type naturalVoice struct {
	State  string              `json:"state"` // ready, missing, installing, failed, no_player
	Text   string              `json:"text,omitempty"`
	DoneMB int64               `json:"done_mb,omitempty"`
	SizeMB int64               `json:"size_mb,omitempty"`
	Player string              `json:"player,omitempty"`
	Folder string              `json:"folder"`
	Voices []speech.PiperVoice `json:"voices"`
	Chosen map[string]string   `json:"chosen"`
}

type piperState struct {
	mu          sync.Mutex
	busy        bool
	failed      string
	done, total int64  // MB downloaded so far
	applied     string // the voices and player last given to the speaker
}

type voiceProgress struct {
	DoneMB int64 `json:"done_mb"`
	SizeMB int64 `json:"size_mb"`
}

func (s *Server) piperHome() string { return filepath.Join(s.workDir, "piper") }

// chosenVoice is the voice picked for lang, or its default.
func chosenVoice(set config.Settings, lang string) string {
	if id := set.PiperVoices[lang]; id != "" {
		return id
	}
	return speech.DefaultPiperVoice(lang)
}

func installedVoice(home, id string) bool {
	for _, v := range speech.PiperCatalog(home) {
		if v.ID == id {
			return v.Installed
		}
	}
	return false
}

// usePiper hands the speaker whichever of the chosen voices are downloaded, English and the
// trainer's language.
func (s *Server) usePiper() {
	if !nativeLinux() || s.speaker == nil {
		return
	}
	set, home := s.cfg.Settings(), s.piperHome()
	bin, player := speech.PiperBinary(home), speech.Player()
	models := map[string]string{}
	for _, lang := range []string{"en", set.Language} {
		if id := chosenVoice(set, lang); installedVoice(home, id) {
			models[lang] = filepath.Join(home, "voices", id+".onnx")
		}
	}
	if bin == "" || player == nil || len(models) == 0 {
		return
	}
	key := fmt.Sprint(bin, player, models)
	s.piper.mu.Lock()
	same := s.piper.applied == key
	s.piper.applied = key
	s.piper.mu.Unlock()
	if same {
		return
	}
	tmp := filepath.Join(os.TempDir(), "dotatrainer-voice")
	os.MkdirAll(tmp, 0o755)
	s.speaker.UsePiper(bin, models, player, tmp)
	s.log.Info("speaking with Piper", "voices", models, "player", player[0])
}

// ensurePiper downloads the chosen voice for the trainer's language when tips are spoken and
// it's missing, the first time it's needed.
func (s *Server) ensurePiper() {
	set := s.cfg.Settings()
	if !nativeLinux() || s.speaker == nil || set.Voice != config.VoiceSystem || speech.Player() == nil {
		return
	}
	home := s.piperHome()
	if speech.PiperBinary(home) != "" && installedVoice(home, chosenVoice(set, set.Language)) {
		s.usePiper()
		return
	}
	s.piper.mu.Lock()
	failed := s.piper.failed != ""
	s.piper.mu.Unlock()
	if !failed {
		s.installPiper()
	}
}

// installPiper downloads Piper and the chosen voice for the trainer's language in the background.
func (s *Server) installPiper() bool {
	s.piper.mu.Lock()
	if s.piper.busy {
		s.piper.mu.Unlock()
		return false
	}
	s.piper.busy, s.piper.failed, s.piper.done, s.piper.total = true, "", 0, 0
	s.piper.mu.Unlock()
	set := s.cfg.Settings()
	voices := []string{chosenVoice(set, set.Language)}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		s.log.Info("downloading Piper voice", "voices", voices, "into", s.piperHome())
		s.hub.publish("settings", s.settingsResponse())
		last := time.Time{}
		err := speech.InstallPiper(ctx, s.piperHome(), voices, func(p speech.Progress) {
			if time.Since(last) < time.Second && p.Done < p.Total {
				return
			}
			last = time.Now()
			s.piper.mu.Lock()
			s.piper.done, s.piper.total = p.Done>>20, p.Total>>20
			s.piper.mu.Unlock()
			s.hub.publish("voice_progress", voiceProgress{DoneMB: p.Done >> 20, SizeMB: p.Total >> 20})
		})
		s.piper.mu.Lock()
		s.piper.busy = false
		if err != nil {
			s.piper.failed = err.Error()
		}
		s.piper.mu.Unlock()
		if err != nil {
			s.log.Warn("Piper download failed", "err", err)
			s.hub.publish("voice_install", voiceStatus{State: "failed", Text: "The natural voice didn't download: " + err.Error()})
		} else {
			s.usePiper()
			s.hub.publish("voice_install", voiceStatus{State: "done", Text: "The natural voice is ready."})
		}
		s.hub.publish("settings", s.settingsResponse())
	}()
	return true
}

func (s *Server) naturalVoiceStatus() *naturalVoice {
	if !nativeLinux() {
		return nil
	}
	set, home := s.cfg.Settings(), s.piperHome()
	nv := &naturalVoice{Folder: home, Voices: speech.PiperCatalog(home),
		Chosen: map[string]string{"en": chosenVoice(set, "en"), set.Language: chosenVoice(set, set.Language)}}
	player := speech.Player()
	if player != nil {
		nv.Player = filepath.Base(player[0])
	}
	s.piper.mu.Lock()
	busy, failed := s.piper.busy, s.piper.failed
	nv.DoneMB, nv.SizeMB = s.piper.done, s.piper.total
	s.piper.mu.Unlock()
	switch {
	case player == nil:
		nv.State = "no_player"
	case busy:
		nv.State = "installing"
	case speech.PiperBinary(home) != "" && installedVoice(home, chosenVoice(set, set.Language)):
		nv.State = "ready"
	case failed != "":
		nv.State, nv.Text = "failed", failed
	default:
		nv.State = "missing"
	}
	return nv
}

// piperChecks are the setup checks for spoken tips on a Linux desktop.
func (s *Server) piperChecks(set config.Settings) []setupCheck {
	nv := s.naturalVoiceStatus()
	if nv == nil || set.Voice != config.VoiceSystem {
		return nil
	}
	name := nv.Chosen[set.Language]
	for _, v := range nv.Voices {
		if v.ID == name {
			name = v.Name
		}
	}
	switch nv.State {
	case "no_player":
		return []setupCheck{{Label: "Nothing can play the natural voice",
			Detail: "Install PipeWire (pw-play) or alsa-utils (aplay), then restart the trainer. Until then tips use speech-dispatcher or eSpeak, if installed."}}
	case "installing":
		return []setupCheck{{Label: "Downloading the natural voice", Detail: fmt.Sprintf("%d of %d MB", nv.DoneMB, nv.SizeMB)}}
	case "failed":
		return []setupCheck{{Label: "The natural voice didn't download", Detail: nv.Text, Fix: "install_voice"}}
	case "missing":
		return []setupCheck{{Label: "The natural voice isn't downloaded yet", Detail: "Piper and one voice, about 90 MB.", Fix: "install_voice"}}
	}
	return []setupCheck{{Label: "Tips are spoken with Piper's natural voice", Detail: strings.TrimSpace(name), OK: true}}
}
