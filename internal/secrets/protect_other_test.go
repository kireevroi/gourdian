//go:build !windows

package secrets

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Under WSL the data folder is the Windows app's, whose keys only Windows can decrypt: reading
// one must say so, not hand out the encrypted bytes as the key.
func TestAWindowsKeyIsntReadAsPlainText(t *testing.T) {
	dir := t.TempDir()
	sealed := append(append([]byte{}, dpapiHeader...), "encrypted for a Windows account"...)
	data := `{"anthropic": "` + base64.StdEncoding.EncodeToString(sealed) + `"}`
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := Open(dir).Get("anthropic")
	if !errors.Is(err, ErrWindowsOnly) || key != "" {
		t.Fatalf("got %q, %v; want ErrWindowsOnly", key, err)
	}
}
