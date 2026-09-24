package server

import (
	"net/http"
	"strings"

	"gourdian/internal/ai/secrets"
)

// stratzKey is the token's name among the stored secrets, beside the AI providers' keys.
const stratzKey = "stratz"

func (s *Server) stratzKeyMasked() string {
	if key, _ := s.keys.Get(stratzKey); key != "" {
		return secrets.Mask(key)
	}
	return ""
}

// handleStratzKey stores the player's STRATZ token; an empty one removes it.
func (s *Server) handleStratzKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key string `json:"key"`
	}
	if err := readJSON(w, r, 8<<10, &body); err != nil {
		http.Error(w, `send {"key": "..."}`, http.StatusBadRequest)
		return
	}
	// Copied from stratz.com/api, the token sometimes brings its "Bearer " along.
	key := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(body.Key), "Bearer "))
	if err := s.keys.Set(stratzKey, key); err != nil {
		http.Error(w, "couldn't store the token: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.counters.Forget()
	s.log.Info("STRATZ token updated", "set", key != "")
	s.publishSettings()
	writeJSON(w, s.settingsResponse())
}
