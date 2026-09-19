package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
)

var toolsDir atomic.Value // string: the trainer's data folder

// SetToolsDir says where the trainer may install the AI command-line tools.
func SetToolsDir(dir string) { toolsDir.Store(dir) }

func say(progress func(string), line string) {
	if progress != nil {
		progress(line)
	}
}

func getJSON(ctx context.Context, url string, into any) error {
	body, err := get(ctx, url)
	if err != nil {
		return err
	}
	defer body.Close()
	return json.NewDecoder(io.LimitReader(body, 8<<20)).Decode(into)
}

func get(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return resp.Body, nil
}

// download saves a file, checking it against the expected SHA-256 as it arrives when the
// publisher gives one.
func download(ctx context.Context, url string, to io.Writer, wantSHA string, progress func(string)) (int64, error) {
	body, err := get(ctx, url)
	if err != nil {
		return 0, err
	}
	defer body.Close()
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(to, sum), &counter{r: body, progress: progress})
	if err != nil {
		return 0, err
	}
	if wantSHA != "" && !strings.EqualFold(hex.EncodeToString(sum.Sum(nil)), wantSHA) {
		return 0, errors.New("the download didn't match the checksum its publisher gave, so it wasn't used")
	}
	return n, nil
}

// counter reports every 20 MB, so a slow download still looks alive.
type counter struct {
	r        io.Reader
	progress func(string)
	n        int64
	said     int64
}

func (c *counter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	if c.n-c.said >= 20<<20 {
		c.said = c.n
		say(c.progress, fmt.Sprintf("  %d MB…", c.n>>20))
	}
	return n, err
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
