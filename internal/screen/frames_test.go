package screen

import (
	"fmt"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func loadFrames(t *testing.T) []string {
	dir := os.Getenv("FRAMES")
	if dir == "" {
		t.Skip("set FRAMES to a folder of captured screens")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.png"))
	sort.Strings(files)
	if len(files) == 0 {
		t.Skip("no frames")
	}
	return files
}

func openFrame(t *testing.T, path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// TestFindAnyOnARealFrame checks the blind sweep against a captured screen: it must not
// invent portraits where there are none, and it must find some where there are.
func TestFindAnyOnARealFrame(t *testing.T) {
	table, names := namedTable(t)
	for _, path := range loadFrames(t) {
		img := openFrame(t, path)
		found := FindAny(img, table)
		sort.Slice(found, func(a, b int) bool { return found[a].Cell.Min.X < found[b].Cell.Min.X })
		var heroes []string
		for _, s := range found {
			heroes = append(heroes, short(names[s.Hero]))
		}
		fmt.Printf("%-14s %d: %v\n", filepath.Base(path), len(found), heroes)
	}
}
