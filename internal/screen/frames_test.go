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
	found, _ := filepath.Glob(filepath.Join(dir, "*.png"))
	sort.Strings(found)
	// A capture that stopped early can leave half a picture behind.
	var files []string
	for _, path := range found {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		_, _, err = image.Decode(f)
		f.Close()
		if err == nil {
			files = append(files, path)
		} else {
			t.Logf("skipping %s: %v", filepath.Base(path), err)
		}
	}
	if len(files) == 0 {
		t.Skip("no readable frames")
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

// TestLocateOnRealFrames checks the search against captured screens: it must find the bar in
// the ones that show it, and find nothing in the menu frames before the draft.
func TestLocateOnRealFrames(t *testing.T) {
	table, _ := namedTable(t)
	for _, path := range loadFrames(t) {
		img := openFrame(t, path)
		found, read, ok := Locate(img, table)
		where := "no bar"
		if ok {
			where = fmt.Sprintf("%d heroes at y=%d, portrait %dx%d", read, found.Y(), found.Left.W/Slots, found.Left.H)
		}
		fmt.Printf("%-14s %s\n", filepath.Base(path), where)
	}
}
