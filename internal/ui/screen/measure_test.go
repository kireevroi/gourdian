package screen

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	xdraw "golang.org/x/image/draw"

	"gourdian/internal/game/vpk"
)

// The measurements behind the constants in this package, and the check that reading a
// portrait off the screen works against the art Dota really draws. They need an installed
// copy of the game, so they skip unless DOTA points at one:
//
//	DOTA="/path/to/dota 2 beta" go test ./internal/screen -run TestRead -v
var variantSuffix = regexp.MustCompile(`_(alt\d*|persona\d+)$`)

type art struct {
	hero string // the hero it belongs to
	name string // the style, which may be an arcana or a persona
	img  image.Image
}

func gameArt(t *testing.T) ([]art, Table, map[string]int) {
	dota := os.Getenv("DOTA")
	if dota == "" {
		t.Skip("set DOTA to an installed copy of the game")
	}
	archive, err := vpk.Open(filepath.Join(dota, "game", "dota", "pak01_dir.vpk"))
	if err != nil {
		t.Fatal(err)
	}
	var arts []art
	for _, e := range archive.Find("panorama/images/heroes/npc_dota_hero_") {
		name := strings.TrimSuffix(filepath.Base(e.Path), ".vtex_c")
		name = strings.TrimSuffix(strings.TrimSuffix(name, "_png"), "_psd")
		hero := variantSuffix.ReplaceAllString(name, "")
		if _, ok := Known()[hero]; !ok {
			continue
		}
		raw, err := archive.Read(e)
		if err != nil {
			t.Fatal(err)
		}
		img, err := vpk.Texture(raw)
		if err != nil {
			continue
		}
		arts = append(arts, art{hero: hero, name: name, img: img})
	}
	ids := map[string]int{}
	heroes := make([]string, 0, len(Known()))
	for hero := range Known() {
		heroes = append(heroes, hero)
	}
	sort.Strings(heroes)
	for i, hero := range heroes {
		ids[hero] = i + 1
	}
	return arts, TableFor(ids), ids
}

// paint draws a portrait into a cell the way the game does: framed in the team's colour, with
// a health bar over it, and a pixel of drift between one frame and the next.
func paint(a art, cellW int, rng *rand.Rand) (*image.RGBA, image.Rectangle) {
	cellH := cellW * 9 / 16
	shot := image.NewRGBA(image.Rect(0, 0, cellW+40, cellH+40))
	draw.Draw(shot, shot.Bounds(), image.NewUniform(color.RGBA{18, 20, 26, 255}), image.Point{}, draw.Src)
	dx, dy := rng.IntN(3)-1, rng.IntN(3)-1
	cell := image.Rect(20+dx, 20+dy, 20+dx+cellW, 20+dy+cellH)
	draw.Draw(shot, cell, image.NewUniform(color.RGBA{190, 60, 50, 255}), image.Point{}, draw.Src)
	in := cell.Inset(2)
	xdraw.ApproxBiLinear.Scale(shot, in, a.img, a.img.Bounds(), xdraw.Src, nil)
	hp := image.Rect(in.Min.X, in.Max.Y-2, in.Max.X, in.Max.Y)
	draw.Draw(shot, hp, image.NewUniform(color.RGBA{40, 200, 60, 255}), image.Point{}, draw.Src)
	// The calibration is a pixel off from where the portrait actually landed.
	return shot, image.Rect(20, 20, 20+cellW, 20+cellH)
}

// TestReadingTheGamesOwnArt is the measurement that matters: every portrait Dota draws, read
// back out of a bar. Arcanas and personas must come back as the hero they are, and nothing
// may ever come back as the wrong hero.
func TestReadingTheGamesOwnArt(t *testing.T) {
	arts, table, ids := gameArt(t)
	variants := 0
	for _, a := range arts {
		if a.hero != a.name {
			variants++
		}
	}
	fmt.Printf("%d portraits of %d heroes, %d of them arcanas, personas or alternate styles\n", len(arts), len(ids), variants)
	for _, cellW := range []int{40, 48, 60, 76} {
		for _, frames := range []int{1, 20} {
			read, wrong := 0, 0
			var missed []string
			for _, a := range arts {
				rng := rand.New(rand.NewPCG(uint64(len(a.name)), 1))
				got, bad := 0, false
				for range frames {
					shot, cell := paint(a, cellW, rng)
					if id, ok := table.Match(Of(shot, cell)); ok {
						if id == ids[a.hero] {
							got = id
						} else {
							bad = true
						}
					}
				}
				switch {
				case bad:
					wrong++
				case got != 0:
					read++
				default:
					missed = append(missed, a.name)
				}
			}
			fmt.Printf("cell %3dpx over %2d frames: read %3d/%d  WRONG %d\n", cellW, frames, read, len(arts), wrong)
			if frames == 20 && cellW >= 60 && len(missed) > 0 {
				fmt.Printf("   not read: %s\n", strings.Join(missed, ", "))
			}
			if wrong > 0 {
				t.Errorf("%d portraits read as the wrong hero at %dpx", wrong, cellW)
			}
		}
	}
}
