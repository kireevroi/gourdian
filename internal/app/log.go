package app

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// newLogger writes to the console and to trainer.log in the data folder, keeping the previous run's log.
func newLogger(dir string) (*slog.Logger, func()) {
	console := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	logs := filepath.Join(dir, "logs")
	os.MkdirAll(logs, 0o755)
	path := filepath.Join(logs, "trainer.log")
	os.Rename(path, filepath.Join(logs, "trainer.prev.log"))
	f, err := os.Create(path)
	if err != nil {
		return slog.New(console), func() {}
	}
	// The file comes first: without a console, writes to stderr fail and MultiWriter stops there.
	w := io.MultiWriter(f, os.Stderr)
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo})), func() { f.Close() }
}
