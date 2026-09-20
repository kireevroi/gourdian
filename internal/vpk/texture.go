package vpk

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
)

// Source 2 stores a compiled texture as a small resource header, a table of blocks, and then
// the picture itself straight after the header. Only the three ways Dota keeps a hero
// portrait are understood here; anything else is refused rather than guessed at.
const (
	formatDXT5    = 2
	formatPNGRGBA = 16
	formatBGRA    = 28
)

// Texture decodes a compiled texture (.vtex_c) into a picture.
func Texture(raw []byte) (image.Image, error) {
	if len(raw) < 16 {
		return nil, fmt.Errorf("a texture is at least 16 bytes, this is %d", len(raw))
	}
	size := binary.LittleEndian.Uint32(raw[0:4])
	blockOffset := binary.LittleEndian.Uint32(raw[8:12])
	blockCount := binary.LittleEndian.Uint32(raw[12:16])
	at := 8 + int(blockOffset)
	var data []byte
	for range blockCount {
		if at+12 > len(raw) {
			return nil, fmt.Errorf("the texture's block table runs past its end")
		}
		kind := string(raw[at : at+4])
		off := int(binary.LittleEndian.Uint32(raw[at+4 : at+8]))
		length := int(binary.LittleEndian.Uint32(raw[at+8 : at+12]))
		if kind == "DATA" && at+4+off+length <= len(raw) {
			data = raw[at+4+off : at+4+off+length]
		}
		at += 12
	}
	if len(data) < 28 {
		return nil, fmt.Errorf("the texture has no description of itself")
	}
	width := int(binary.LittleEndian.Uint16(data[20:22]))
	height := int(binary.LittleEndian.Uint16(data[22:24]))
	format := data[26]
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("the texture says it is %dx%d", width, height)
	}
	// The picture follows the resource, whose length the header gives.
	if int(size) > len(raw) {
		return nil, fmt.Errorf("the texture says it is %d bytes, it is %d", size, len(raw))
	}
	pixels := raw[size:]

	switch format {
	case formatPNGRGBA:
		return png.Decode(bytes.NewReader(pixels))
	case formatBGRA:
		return fromBGRA(pixels, width, height)
	case formatDXT5:
		return fromDXT5(pixels, width, height)
	}
	return nil, fmt.Errorf("texture format %d isn't one this knows", format)
}

func fromBGRA(pixels []byte, width, height int) (image.Image, error) {
	if len(pixels) < width*height*4 {
		return nil, fmt.Errorf("a %dx%d picture needs %d bytes, got %d", width, height, width*height*4, len(pixels))
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := range width * height {
		b, g, r, a := pixels[i*4], pixels[i*4+1], pixels[i*4+2], pixels[i*4+3]
		img.Pix[i*4], img.Pix[i*4+1], img.Pix[i*4+2], img.Pix[i*4+3] = r, g, b, a
	}
	return img, nil
}

// fromDXT5 decodes BC3: every four by four block of pixels is eight bytes of alpha and eight
// of colour, each a pair of endpoints and an index per pixel into the shades between them.
func fromDXT5(pixels []byte, width, height int) (image.Image, error) {
	cols, rows := (width+3)/4, (height+3)/4
	if need := cols * rows * 16; len(pixels) < need {
		return nil, fmt.Errorf("a %dx%d picture needs %d bytes of DXT5, got %d", width, height, need, len(pixels))
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for by := range rows {
		for bx := range cols {
			block := pixels[(by*cols+bx)*16:][:16]
			var alpha [8]uint8
			alpha[0], alpha[1] = block[0], block[1]
			if alpha[0] > alpha[1] {
				for i := 1; i < 7; i++ {
					alpha[i+1] = uint8((int(alpha[0])*(7-i) + int(alpha[1])*i) / 7)
				}
			} else {
				for i := 1; i < 5; i++ {
					alpha[i+1] = uint8((int(alpha[0])*(5-i) + int(alpha[1])*i) / 5)
				}
				alpha[6], alpha[7] = 0, 255
			}
			// Six bytes hold sixteen three-bit indices, least significant first.
			var alphaBits uint64
			for i := range 6 {
				alphaBits |= uint64(block[2+i]) << (8 * i)
			}

			c0 := binary.LittleEndian.Uint16(block[8:10])
			c1 := binary.LittleEndian.Uint16(block[10:12])
			var colour [4][3]uint8
			colour[0] = rgb565(c0)
			colour[1] = rgb565(c1)
			for i := range 3 {
				colour[2][i] = uint8((2*int(colour[0][i]) + int(colour[1][i])) / 3)
				colour[3][i] = uint8((int(colour[0][i]) + 2*int(colour[1][i])) / 3)
			}
			colourBits := binary.LittleEndian.Uint32(block[12:16])

			for y := range 4 {
				for x := range 4 {
					px, py := bx*4+x, by*4+y
					if px >= width || py >= height {
						continue
					}
					n := y*4 + x
					c := colour[(colourBits>>(2*n))&3]
					i := img.PixOffset(px, py)
					img.Pix[i], img.Pix[i+1], img.Pix[i+2] = c[0], c[1], c[2]
					img.Pix[i+3] = alpha[(alphaBits>>(3*n))&7]
				}
			}
		}
	}
	return img, nil
}

// rgb565 widens a packed colour to eight bits a channel, spreading the top bits down so white
// stays white.
func rgb565(v uint16) [3]uint8 {
	r, g, b := uint8(v>>11)&0x1f, uint8(v>>5)&0x3f, uint8(v)&0x1f
	return [3]uint8{r<<3 | r>>2, g<<2 | g>>4, b<<3 | b>>2}
}
