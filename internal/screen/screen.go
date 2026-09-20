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

// A signature is a small colour thumbnail: the mean colour of each block of a portrait.
//
// Colour is what tells Dota's heroes apart. The obvious tool here would be a perceptual hash
// of the kind used to find near-duplicate photographs, which compares the brightness of
// neighbouring pixels, but the portraits are all dark and composed alike, so at the size the
// game draws them in the bar it misread a third of the roster. The same measurement over all
// 126 portraits gives this thumbnail no wrong answers at all.
const (
	sigW, sigH = 12, 7
	sigLen     = sigW * sigH * 3
)

// Inset is how much of a portrait's edge is thrown away before it is read, as a fraction of
// its size. Dota frames each portrait in its team's colour and paints a health bar along it,
// and none of that says which hero it is.
const Inset = 0.14

// Ratio is how much closer the best match must be than the runner-up for it to be believed,
// as a fraction of the runner-up's distance. It is what turns a doubtful reading into no
// reading: a slot nobody has picked looks a little like every hero, and the trainer would
// rather say nothing than name the wrong one. Measured over every portrait at every size the
// bar is drawn at, 0.7 never once gave a wrong answer.
//
// TODO: tune. Measured against portraits painted into a bar by the tests, not against frames
// from a real client.
const Ratio = 0.7

// Signature is one portrait's thumbnail, red, green and blue for each block in turn.
type Signature [sigLen]uint8

// Of reads the signature of the part of img inside r.
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

// Distance is how far apart two signatures are: the squared difference over every colour of
// every block. Only the order of these matters, never the number itself.
func (s Signature) Distance(other Signature) int {
	total := 0
	for i := range s {
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

// inset shrinks a rectangle by a fraction of its own size on every side.
func inset(r image.Rectangle, by float64) image.Rectangle {
	dx, dy := int(float64(r.Dx())*by), int(float64(r.Dy())*by)
	in := image.Rect(r.Min.X+dx, r.Min.Y+dy, r.Max.X-dx, r.Max.Y-dy)
	if in.Empty() {
		return r
	}
	return in
}

// Table is what the portraits of the heroes look like, ready to be matched against. A hero
// has an entry for every portrait the game draws them with: the base art and any arcana,
// persona or alternate style.
type Table map[int][]Signature

// Add reads a hero's portrait into the table.
func (t Table) Add(heroID int, portrait image.Image) { t.add(heroID, Of(portrait, portrait.Bounds())) }

func (t Table) add(heroID int, s Signature) { t[heroID] = append(t[heroID], s) }

// Match is the hero whose portrait is closest, or nothing when no hero stands clearly apart
// from the next one. Answering "don't know" costs a frame; answering wrongly costs trust.
//
// The runner-up is the closest portrait of a different hero, not simply the second closest
// picture: two styles of the same hero sitting near each other is agreement, not doubt.
func (t Table) Match(s Signature) (heroID int, ok bool) {
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
