package overlay

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"gourdian/internal/config"
	"gourdian/internal/hud"
)

// painter draws the HUD in pure Go with the same layout as the Windows GDI one. Linux uses it,
// since there is no GDI there; the image is premultiplied RGBA, what X11's 32-bit visuals take.
type painter struct {
	scale      float64
	width      int
	layout     config.OverlaySettings
	big, small font.Face
}

// newPainter sizes the HUD for base, the screen's scale, and the player's layout.
func newPainter(base float64, o config.OverlaySettings) (*painter, error) {
	p := &painter{scale: base * float64(max(o.HUDScale, config.MinHUDScale)) / 100, layout: o}
	p.width = p.px(float64(o.HUDWidth))
	var err error
	if p.big, err = face(gobold.TTF, float64(p.px(22))); err != nil {
		return nil, err
	}
	if p.small, err = face(gomedium.TTF, float64(p.px(17))); err != nil {
		return nil, err
	}
	return p, nil
}

func face(ttf []byte, px float64) (font.Face, error) {
	f, err := opentype.Parse(ttf)
	if err != nil {
		return nil, err
	}
	return opentype.NewFace(f, &opentype.FaceOptions{Size: px, DPI: 72, Hinting: font.HintingFull})
}

func (p *painter) px(v float64) int { return int(v * p.scale) }

// wrap breaks text into lines no wider than width, splitting words only when one is too wide.
func wrap(f font.Face, text string, width int) []string {
	limit := fixed.I(width)
	var lines []string
	for _, para := range strings.Split(text, "\n") {
		line := ""
		for _, word := range strings.Fields(para) {
			next := word
			if line != "" {
				next = line + " " + word
			}
			if font.MeasureString(f, next) <= limit {
				line = next
				continue
			}
			if line != "" {
				lines = append(lines, line)
			}
			line = ""
			for _, r := range word {
				if font.MeasureString(f, line+string(r)) > limit && line != "" {
					lines = append(lines, line)
					line = ""
				}
				line += string(r)
			}
		}
		lines = append(lines, line)
	}
	return lines
}

func lineHeight(f font.Face) int { return f.Metrics().Height.Ceil() }

type paintBlock struct {
	lines  []hud.Line
	wraps  [][]string
	accent color.RGBA
	face   font.Face
	height int
}

// paint draws the view and returns an image exactly as tall as the HUD.
func (p *painter) paint(v View, editing bool) *image.RGBA {
	pad, gap, bar := p.px(hudPad), p.px(hudGap), p.px(hudBar)
	textW := p.width - bar - 2*pad

	var blocks []*paintBlock
	if v.Banner != "" {
		blocks = append(blocks, &paintBlock{lines: []hud.Line{{Text: v.Banner, Kind: hud.KindMuted}}, accent: kindColor(hud.KindMuted), face: p.small})
	}
	if v.Alert != nil {
		a := *v.Alert
		if v.More > 0 {
			a.Text += fmt.Sprintf("  (+%d)", v.More)
		}
		blocks = append(blocks, &paintBlock{lines: []hud.Line{a}, accent: kindColor(v.Alert.Kind), face: p.big})
	}
	if len(v.Rows) > 0 {
		blocks = append(blocks, &paintBlock{lines: v.Rows, accent: hudLine, face: p.small})
	}
	content := 0
	for _, b := range blocks {
		b.height = 2 * pad
		for i, l := range b.lines {
			w := wrap(b.face, l.Text, textW)
			b.wraps = append(b.wraps, w)
			b.height += len(w) * lineHeight(b.face)
			if i > 0 {
				b.height += gap
			}
		}
		content += b.height + p.px(hudSpacing)
	}
	total := content
	if editing {
		total = max(content+p.px(editBottom), p.px(editMin))
	}
	img := image.NewRGBA(image.Rect(0, 0, p.width, max(total, 1)))
	if editing {
		fill(img, img.Bounds(), hudRow, 170)
	}

	bg := uint32(p.layout.HUDBackground) * 255 / 100
	y := 0
	for _, b := range blocks {
		fillRound(img, image.Rect(0, y, p.width, y+b.height), p.px(hudRadius), hudPanel, bg)
		fill(img, image.Rect(0, y+p.px(hudBarInset), bar, y+b.height-p.px(hudBarInset)), b.accent, 255)
		ty := y + pad
		for i, l := range b.lines {
			for _, line := range b.wraps[i] {
				p.text(img, line, bar+pad, ty, b.face, kindColor(l.Kind), p.layout.HUDShadow)
				ty += lineHeight(b.face)
			}
			ty += gap
		}
		y += b.height + p.px(hudSpacing)
	}
	if editing {
		p.frame(img)
	}
	if o := p.layout.HUDOpacity; o > 0 && o < 100 {
		fade(img, uint32(o)*255/100)
	}
	return img
}

// doneRect is the Done button while the layout is edited, in an image h pixels tall.
func (p *painter) doneRect(h int) image.Rectangle {
	return image.Rect(p.width-p.px(96), h-p.px(44), p.width-p.px(12), h-p.px(12))
}

func (p *painter) hint(width int) string {
	return editHint(p.layout, func(s string) bool { return font.MeasureString(p.small, s) <= fixed.I(width) })
}

// frame outlines the HUD while editing and adds the hint and the Done button.
func (p *painter) frame(img *image.RGBA) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	b := p.px(2)
	for _, r := range []image.Rectangle{image.Rect(0, 0, w, b), image.Rect(0, h-b, w, h), image.Rect(0, 0, b, h), image.Rect(w-b, 0, w, h)} {
		fill(img, r, hudAccent, 255)
	}
	d := p.doneRect(h)
	lh := lineHeight(p.small)
	ty := d.Min.Y + (d.Dy()-lh)/2
	p.text(img, p.hint(d.Min.X-p.px(20)), p.px(12), ty, p.small, kindColor(hud.KindText), true)
	fillRound(img, d, p.px(6), hudAccent, 255)
	label := "Done"
	x := d.Min.X + (d.Dx()-font.MeasureString(p.small, label).Ceil())/2
	p.text(img, label, x, ty, p.small, kindColor(hud.KindText), false)
}

// text draws one line with its top at y, with a dark 1px shadow down and right when shadow is set.
func (p *painter) text(img *image.RGBA, s string, x, y int, f font.Face, col color.RGBA, shadow bool) {
	if s == "" {
		return
	}
	w := font.MeasureString(f, s).Ceil() + 2
	h := lineHeight(f) + 2
	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	d := font.Drawer{Dst: mask, Src: image.Opaque, Face: f, Dot: fixed.P(0, f.Metrics().Ascent.Ceil())}
	d.DrawString(s)
	at := func(mx, my int) uint32 {
		if mx < 0 || my < 0 || mx >= w || my >= h {
			return 0
		}
		return uint32(mask.Pix[my*mask.Stride+mx])
	}
	for my := range h + 1 {
		for mx := range w + 1 {
			if shadow {
				if a := max(at(mx-1, my-1), at(mx-1, my), at(mx, my-1)); a > 0 {
					blend(img, x+mx, y+my, color.RGBA{0, 0, 0, 255}, a*200/255)
				}
			}
			if a := at(mx, my); a > 0 {
				blend(img, x+mx, y+my, col, a)
			}
		}
	}
}

// blend draws col with alpha a (0-255) over one pixel of a premultiplied image.
func blend(img *image.RGBA, x, y int, col color.RGBA, a uint32) {
	if a == 0 || !(image.Point{x, y}.In(img.Rect)) {
		return
	}
	i := img.PixOffset(x, y)
	px := img.Pix[i : i+4 : i+4]
	inv := 255 - a
	px[0] = byte((uint32(col.R)*a + uint32(px[0])*inv) / 255)
	px[1] = byte((uint32(col.G)*a + uint32(px[1])*inv) / 255)
	px[2] = byte((uint32(col.B)*a + uint32(px[2])*inv) / 255)
	px[3] = byte(a + uint32(px[3])*inv/255)
}

func fill(img *image.RGBA, r image.Rectangle, col color.RGBA, a uint32) {
	r = r.Intersect(img.Rect)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			blend(img, x, y, col, a)
		}
	}
}

// fillRound fills a rectangle with anti-aliased rounded corners.
func fillRound(img *image.RGBA, r image.Rectangle, radius int, col color.RGBA, a uint32) {
	rad := float64(radius)
	c := r.Intersect(img.Rect)
	for y := c.Min.Y; y < c.Max.Y; y++ {
		for x := c.Min.X; x < c.Max.X; x++ {
			cx, cy := -1.0, -1.0
			switch {
			case x < r.Min.X+radius:
				cx = float64(r.Min.X+radius) - float64(x) - 0.5
			case x >= r.Max.X-radius:
				cx = float64(x) + 0.5 - float64(r.Max.X-radius)
			}
			switch {
			case y < r.Min.Y+radius:
				cy = float64(r.Min.Y+radius) - float64(y) - 0.5
			case y >= r.Max.Y-radius:
				cy = float64(y) + 0.5 - float64(r.Max.Y-radius)
			}
			cover := 1.0
			if cx > 0 && cy > 0 {
				cover = min(max(rad+0.5-math.Hypot(cx, cy), 0), 1)
			}
			blend(img, x, y, col, uint32(float64(a)*cover))
		}
	}
}

// fade scales the whole premultiplied image by a (0-255), the HUD's overall opacity.
func fade(img *image.RGBA, a uint32) {
	for i, v := range img.Pix {
		img.Pix[i] = byte(uint32(v) * a / 255)
	}
}

// savePaintedPNG writes a painted HUD with its transparency.
func savePaintedPNG(path string, img *image.RGBA) error {
	out := image.NewNRGBA(img.Rect)
	draw.Draw(out, out.Rect, img, image.Point{}, draw.Src)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, out)
}
