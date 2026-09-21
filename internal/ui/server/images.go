package server

import (
	"context"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const imageCDN = "https://cdn.cloudflare.steamstatic.com/"

// handleImage serves hero and item art from a disk cache, fetching from Steam's CDN on first use.
func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	p := path.Clean("/" + r.PathValue("path"))[1:]
	ext := path.Ext(p)
	if !strings.HasPrefix(p, "apps/dota2/images/") || (ext != ".png" && ext != ".jpg" && ext != ".webp") {
		http.NotFound(w, r)
		return
	}
	local := filepath.Join(s.cacheDir, "img", filepath.FromSlash(p))
	if _, err := os.Stat(local); err != nil {
		if err := fetchImage(r.Context(), imageCDN+p, local); err != nil {
			http.Error(w, "image unavailable", http.StatusBadGateway)
			return
		}
	}
	w.Header().Set("Cache-Control", "public, max-age=604800")
	http.ServeFile(w, r, local)
}

func fetchImage(ctx context.Context, url, dest string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &os.PathError{Op: "fetch", Path: url, Err: os.ErrNotExist}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, io.LimitReader(resp.Body, 4<<20)); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}
