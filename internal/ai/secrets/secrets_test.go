package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetGetDelete(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir)
	if v, err := s.Get("openai"); v != "" || err != nil {
		t.Fatalf("empty store = %q, %v", v, err)
	}
	if err := s.Set("openai", "sk-test-1234567890"); err != nil {
		t.Fatal(err)
	}
	if v, _ := Open(dir).Get("openai"); v != "sk-test-1234567890" {
		t.Fatalf("got %q", v)
	}
	s.Set("openai", "")
	if v, _ := s.Get("openai"); v != "" {
		t.Fatal("empty value should delete the key")
	}
	data, _ := os.ReadFile(filepath.Join(dir, fileName))
	if strings.Contains(string(data), "sk-test") {
		t.Fatal("key stored in plain text")
	}
}

func TestMask(t *testing.T) {
	if got := Mask("sk-ant-api03-abcdefghijkl"); got != "sk-…ijkl" {
		t.Fatalf("mask = %q", got)
	}
	if got := Mask("short"); got != "…rt" {
		t.Fatalf("mask = %q", got)
	}
}
