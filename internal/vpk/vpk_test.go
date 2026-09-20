package vpk

import (
	"bytes"
	"encoding/binary"
	"os"
	"path"
	"path/filepath"
	"slices"
	"testing"
)

// build writes a small pack the way Valve's tools lay one out, so the reader can be checked
// without an installed game.
func build(t *testing.T, dir string, files map[string][]byte) string {
	t.Helper()
	// Everything goes in one numbered archive beside the directory.
	var body bytes.Buffer
	type where struct{ offset, length uint32 }
	at := map[string]where{}
	for _, name := range slices.Sorted(maps(files)) {
		at[name] = where{uint32(body.Len()), uint32(len(files[name]))}
		body.Write(files[name])
	}

	// The tree groups by extension, then folder, then name.
	var tree bytes.Buffer
	byExt := map[string]map[string][]string{}
	for name := range files {
		// A pack spells its paths with slashes whatever system it was made on, so these are
		// taken apart with path and not with filepath.
		folder, base := path.Split(name)
		folder = path.Clean(folder)
		ext := path.Ext(base)[1:]
		stem := base[:len(base)-len(ext)-1]
		if byExt[ext] == nil {
			byExt[ext] = map[string][]string{}
		}
		byExt[ext][folder] = append(byExt[ext][folder], stem)
	}
	for _, ext := range slices.Sorted(maps(byExt)) {
		tree.WriteString(ext)
		tree.WriteByte(0)
		for _, folder := range slices.Sorted(maps(byExt[ext])) {
			tree.WriteString(folder)
			tree.WriteByte(0)
			for _, stem := range byExt[ext][folder] {
				tree.WriteString(stem)
				tree.WriteByte(0)
				w := at[folder+"/"+stem+"."+ext]
				binary.Write(&tree, binary.LittleEndian, uint32(0)) // CRC
				binary.Write(&tree, binary.LittleEndian, uint16(0)) // preload bytes
				binary.Write(&tree, binary.LittleEndian, uint16(1)) // archive 001
				binary.Write(&tree, binary.LittleEndian, w.offset)
				binary.Write(&tree, binary.LittleEndian, w.length)
				binary.Write(&tree, binary.LittleEndian, uint16(0xffff))
			}
			tree.WriteByte(0)
		}
		tree.WriteByte(0)
	}
	tree.WriteByte(0)

	var head bytes.Buffer
	binary.Write(&head, binary.LittleEndian, uint32(signature))
	binary.Write(&head, binary.LittleEndian, uint32(2))
	binary.Write(&head, binary.LittleEndian, uint32(tree.Len()))
	binary.Write(&head, binary.LittleEndian, [4]uint32{uint32(body.Len()), 0, 0, 0})

	dirPath := filepath.Join(dir, "pak01_dir.vpk")
	if err := os.WriteFile(dirPath, append(head.Bytes(), tree.Bytes()...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pak01_001.vpk"), body.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return dirPath
}

// maps is the keys of a map, for sorting.
func maps[V any](m map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

func TestReadingAPack(t *testing.T) {
	files := map[string][]byte{
		"panorama/images/heroes/one.vtex_c": []byte("first hero"),
		"panorama/images/heroes/two.vtex_c": bytes.Repeat([]byte("x"), 300),
		"scripts/npc/units.txt":             []byte("some script"),
	}
	path := build(t, t.TempDir(), files)
	a, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Entries()) != len(files) {
		t.Fatalf("read %d entries, want %d", len(a.Entries()), len(files))
	}
	heroes := a.Find("panorama/images/heroes/")
	if len(heroes) != 2 {
		t.Fatalf("found %d hero files: %+v", len(heroes), heroes)
	}
	for _, e := range a.Entries() {
		got, err := a.Read(e)
		if err != nil {
			t.Fatalf("%s: %v", e.Path, err)
		}
		if want := files[e.Path]; !bytes.Equal(got, want) {
			t.Errorf("%s read %d bytes, want %d", e.Path, len(got), len(want))
		}
	}
}

func TestRefusingSomethingThatIsNotAPack(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "pak01_dir.vpk")
	if err := os.WriteFile(fake, []byte("not a pack at all, really"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(fake); err == nil {
		t.Error("a file that isn't a pack was accepted")
	}
	if _, err := Open(filepath.Join(dir, "missing_dir.vpk")); err == nil {
		t.Error("a pack that isn't there was accepted")
	}
}

// A truncated pack must be refused rather than read as far as it goes.
func TestRefusingATruncatedPack(t *testing.T) {
	dir := t.TempDir()
	path := build(t, dir, map[string][]byte{"a/b.txt": []byte("hello")})
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw[:len(raw)-6], 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Error("a truncated pack was accepted")
	}
}
