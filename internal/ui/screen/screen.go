// Package screen recognises Dota's hero portraits in a picture of the player's own screen.
// It reads pixels the game has already drawn for the player and nothing else: no game memory,
// no injection, and nothing the player can't see for themselves.
package screen

import (
	"encoding/hex"
	"fmt"
	"image"

	xdraw "golang.org/x/image/draw"
)

const (
	sigW, sigH = 12, 7
	sigLen     = sigW * sigH * 3
	compared   = sigW * Compared * 3
)

// Inset trims the team-colour frame and health bar off each portrait.
const Inset = 0.14

// Compared is how many top rows of the thumbnail are matched: in ranked games the rank
// banner and flag cover the rows below, and matching them read empty slots as Faceless Void.
const Compared = 3

// Ratio is the most the best match may be of the runner-up. Over two ranked drafts 0.7 let
// empty slots settle as Faceless Void and below 0.5 real picks went unread.
const Ratio = 0.6

// LeastContrast rejects flat, unpicked slots (about 132; portraits run 1400 and up).
const LeastContrast = 600

const levelTo = 96

type Signature [sigLen]uint8

// Level evens out brightness over the compared rows only, so a pale banner can't dim them.
func (s Signature) Level() Signature {
	sum := 0
	for _, v := range s[:compared] {
		sum += int(v)
	}
	mean := sum / compared
	if mean == 0 {
		return s
	}
	var out Signature
	for i, v := range s {
		out[i] = uint8(min(int(v)*levelTo/mean, 255))
	}
	return out
}

func Of(img image.Image, r image.Rectangle) Signature {
	small := image.NewRGBA(image.Rect(0, 0, sigW, sigH))
	xdraw.ApproxBiLinear.Scale(small, small.Bounds(), img, inset(r, Inset), xdraw.Src, nil)
	var s Signature
	n := 0
	for y := range sigH {
		for x := range sigW {
			i := small.PixOffset(x, y)
			s[n], s[n+1], s[n+2] = small.Pix[i], small.Pix[i+1], small.Pix[i+2]
			n += 3
		}
	}
	return s
}

func (s Signature) Distance(other Signature) int {
	total := 0
	for i := range compared {
		d := int(s[i]) - int(other[i])
		total += d * d
	}
	return total
}

func (s Signature) MarshalText() ([]byte, error) {
	out := make([]byte, hex.EncodedLen(len(s)))
	hex.Encode(out, s[:])
	return out, nil
}

func (s *Signature) UnmarshalText(text []byte) error {
	raw := make([]byte, hex.DecodedLen(len(text)))
	if _, err := hex.Decode(raw, text); err != nil {
		return err
	}
	if len(raw) != len(s) {
		return fmt.Errorf("a portrait signature is %d bytes, got %d", len(s), len(raw))
	}
	copy(s[:], raw)
	return nil
}

func inset(r image.Rectangle, by float64) image.Rectangle {
	dx, dy := int(float64(r.Dx())*by), int(float64(r.Dy())*by)
	in := image.Rect(r.Min.X+dx, r.Min.Y+dy, r.Max.X-dx, r.Max.Y-dy)
	if in.Empty() {
		return r
	}
	return in
}

// Table holds every portrait of each hero, arcana and persona styles included.
type Table map[int][]Signature

func (t Table) Add(heroID int, portrait image.Image) { t.add(heroID, Of(portrait, portrait.Bounds())) }

func (t Table) add(heroID int, s Signature) { t[heroID] = append(t[heroID], s.Level()) }

// Match names the closest hero only when it stands clearly apart from the closest other hero.
func (t Table) Match(s Signature) (heroID int, ok bool) {
	if s.Contrast() < LeastContrast {
		return 0, false
	}
	s = s.Level()
	best, second := -1, -1
	for id, arts := range t {
		near := -1
		for _, known := range arts {
			if d := s.Distance(known); near < 0 || d < near {
				near = d
			}
		}
		switch {
		case near < 0:
		case best < 0 || near < best:
			best, second, heroID = near, best, id
		case second < 0 || near < second:
			second = near
		}
	}
	if best < 0 || second < 0 || float64(best) > Ratio*float64(second) {
		return 0, false
	}
	return heroID, true
}

func (s Signature) Contrast() int {
	sum := 0
	for _, v := range s {
		sum += int(v)
	}
	mean := sum / len(s)
	total := 0
	for _, v := range s {
		d := int(v) - mean
		total += d * d
	}
	return total / len(s)
}
