// Package vpk reads Valve's pack files, which is where Dota keeps the art it draws. The
// trainer uses it at build time (cmd/portraits) to learn what every hero portrait looks
// like, including the arcana and persona variants that never reach the web.
package vpk

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const signature = 0x55aa1234

// inArchive marks an entry whose bytes sit in the directory file itself rather than in one of
// the numbered archives beside it.
const inArchive = 0x7fff

// Entry is one file in the pack.
type Entry struct {
	Path    string
	archive uint16
	offset  uint32
	length  uint32
	preload []byte
}

// Archive is an opened pack: the directory it was read from, and everything in it.
type Archive struct {
	dir     string
	base    string // the directory file with "_dir.vpk" taken off
	body    []byte // for entries stored in the directory file itself
	entries []Entry
}

// Open reads the directory of a pack, such as .../game/dota/pak01_dir.vpk. Only the directory
// is read; the bytes of a file are fetched when it is asked for.
func Open(path string) (*Archive, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	r := &reader{buf: raw}
	if r.u32() != signature {
		return nil, fmt.Errorf("%s is not a VPK", filepath.Base(path))
	}
	version := r.u32()
	treeSize := r.u32()
	if version != 1 && version != 2 {
		return nil, fmt.Errorf("VPK version %d isn't one this knows", version)
	}
	if version == 2 {
		r.skip(16) // file data, archive and signature section sizes
	}
	treeAt := r.pos
	a := &Archive{dir: path, base: strings.TrimSuffix(path, "_dir.vpk")}
	// Entries stored in the directory file follow the tree.
	if end := treeAt + int(treeSize); end <= len(raw) {
		a.body = raw[end:]
	}
	for {
		ext := r.str()
		if ext == "" {
			break
		}
		for {
			folder := r.str()
			if folder == "" {
				break
			}
			for {
				name := r.str()
				if name == "" {
					break
				}
				r.skip(4) // CRC
				preload := int(r.u16())
				e := Entry{archive: r.u16(), offset: r.u32(), length: r.u32()}
				r.skip(2) // terminator
				if preload > 0 {
					e.preload = r.bytes(preload)
				}
				e.Path = folder + "/" + name + "." + ext
				if folder == " " { // the pack spells "no folder" as a single space
					e.Path = name + "." + ext
				}
				a.entries = append(a.entries, e)
			}
		}
	}
	if r.err != nil {
		return nil, fmt.Errorf("reading %s: %w", filepath.Base(path), r.err)
	}
	return a, nil
}

// Entries lists every file in the pack.
func (a *Archive) Entries() []Entry { return a.entries }

// Find lists the files whose path has this prefix.
func (a *Archive) Find(prefix string) []Entry {
	var out []Entry
	for _, e := range a.entries {
		if strings.HasPrefix(e.Path, prefix) {
			out = append(out, e)
		}
	}
	return out
}

// Read returns one file's bytes.
func (a *Archive) Read(e Entry) ([]byte, error) {
	if e.length == 0 {
		return bytes.Clone(e.preload), nil
	}
	var from []byte
	if e.archive == inArchive {
		from = a.body
	} else {
		raw, err := os.ReadFile(fmt.Sprintf("%s_%03d.vpk", a.base, e.archive))
		if err != nil {
			return nil, err
		}
		from = raw
	}
	end := int(e.offset) + int(e.length)
	if int(e.offset) > len(from) || end > len(from) {
		return nil, fmt.Errorf("%s runs past the end of its archive", e.Path)
	}
	return append(bytes.Clone(e.preload), from[e.offset:end]...), nil
}

// reader walks a little-endian buffer, remembering the first thing that went wrong so each
// read doesn't have to be checked.
type reader struct {
	buf []byte
	pos int
	err error
}

func (r *reader) bytes(n int) []byte {
	if r.err != nil || r.pos+n > len(r.buf) {
		r.fail()
		return make([]byte, n)
	}
	out := r.buf[r.pos : r.pos+n]
	r.pos += n
	return out
}

func (r *reader) fail() {
	if r.err == nil {
		r.err = fmt.Errorf("the file ends sooner than it says it does")
	}
}

func (r *reader) skip(n int)  { r.bytes(n) }
func (r *reader) u16() uint16 { return binary.LittleEndian.Uint16(r.bytes(2)) }
func (r *reader) u32() uint32 { return binary.LittleEndian.Uint32(r.bytes(4)) }

// str reads a NUL-terminated string.
func (r *reader) str() string {
	if r.err != nil {
		return ""
	}
	end := bytes.IndexByte(r.buf[r.pos:], 0)
	if end < 0 {
		r.fail()
		return ""
	}
	s := string(r.buf[r.pos : r.pos+end])
	r.pos += end + 1
	return s
}
