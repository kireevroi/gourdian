package server

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
)

// StartRecording saves every following GSI payload to a new file in the data folder.
func (s *Server) StartRecording() (string, error) { return s.rec.StartManual(s.cfg.Get().Token) }

func (s *Server) StopRecording() error { return s.rec.Stop() }

func (s *Server) handleRecording(w http.ResponseWriter, r *http.Request) {
	var body struct {
		On bool `json:"on"`
	}
	if err := readJSON(w, r, 1<<10, &body); err != nil {
		http.Error(w, `send {"on": true} or {"on": false}`, http.StatusBadRequest)
		return
	}
	var err error
	if body.On {
		_, err = s.StartRecording()
	} else {
		err = s.StopRecording()
	}
	if err != nil {
		s.failed(w, http.StatusInternalServerError, "couldn't "+map[bool]string{true: "start", false: "stop"}[body.On]+" recording", err)
		return
	}
	resp := s.settingsResponse()
	s.hub.publish("settings", resp)
	writeJSON(w, resp)
}

func (s *Server) handleRecordings(w http.ResponseWriter, r *http.Request) {
	files, _ := filepath.Glob(filepath.Join(s.rec.Dir, "*.jsonl*"))
	var out []recordingInfo
	for _, f := range files {
		if fi, err := os.Stat(f); err == nil && fi.Size() > 100 {
			out = append(out, recordingInfo{Name: filepath.Base(f), Size: fi.Size(), Modified: fi.ModTime()})
		}
	}
	slices.SortFunc(out, func(a, b recordingInfo) int { return b.Modified.Compare(a.Modified) })
	writeJSON(w, out)
}
