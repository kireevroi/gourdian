// Package secrets keeps API keys in the data folder, encrypted for the current Windows user
// with DPAPI, so the file is useless if copied to another account or machine.
package secrets

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

const fileName = "secrets.json"

// ErrWindowsOnly is a key the Windows app encrypted, read by a build that isn't on Windows
// (under WSL the data folder is shared): only Windows can decrypt it.
var ErrWindowsOnly = errors.New("this key was saved by the Windows app and only Windows can read it; enter it again here")

// dpapiHeader starts every blob Windows' CryptProtectData makes: version 1, then the id of its
// default provider.
var dpapiHeader = []byte{0x01, 0x00, 0x00, 0x00, 0xd0, 0x8c, 0x9d, 0xdf, 0x01, 0x15, 0xd1, 0x11, 0x8c, 0x7a, 0x00, 0xc0, 0x4f, 0xc2, 0x97, 0xeb}

func isDPAPI(sealed []byte) bool { return bytes.HasPrefix(sealed, dpapiHeader) }

type Store struct {
	path string
	mu   sync.Mutex
}

func Open(dir string) *Store { return &Store{path: filepath.Join(dir, fileName)} }

func (s *Store) load() (map[string]string, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	return m, json.Unmarshal(data, &m)
}

// Get returns the secret, or "" when none is stored.
func (s *Store) Get(name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil || m[name] == "" {
		return "", err
	}
	sealed, err := base64.StdEncoding.DecodeString(m[name])
	if err != nil {
		return "", err
	}
	plain, err := unprotect(sealed)
	return string(plain), err
}

// Set stores a secret; an empty value removes it.
func (s *Store) Set(name, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return err
	}
	if value == "" {
		delete(m, name)
	} else {
		sealed, err := protect([]byte(value))
		if err != nil {
			return err
		}
		m[name] = base64.StdEncoding.EncodeToString(sealed)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Mask shows enough of a key to recognise it: "sk-…abcd".
func Mask(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "…" + key[len(key)-min(2, len(key)):]
	}
	return key[:3] + "…" + key[len(key)-4:]
}
