package vpk

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"strings"
	"testing"
)

// buildTexture creates a mock .vtex_c payload.
// If makeInvalidDataBlock is true, it makes the data block smaller than 28 bytes.
func buildTexture(width, height uint16, format uint8, pixels []byte, makeInvalidDataBlock bool) []byte {
	var buf bytes.Buffer

	// 1. DATA block content (must be at least 28 bytes for description)
	dataBlockSize := 28
	if makeInvalidDataBlock {
		dataBlockSize = 20
	}
	dataBlock := make([]byte, dataBlockSize)
	if !makeInvalidDataBlock {
		binary.LittleEndian.PutUint16(dataBlock[20:22], width)
		binary.LittleEndian.PutUint16(dataBlock[22:24], height)
		dataBlock[26] = format
	}

	// Calculate sizes
	headerSize := uint32(16 + 12 + len(dataBlock))

	// Write 16 byte main header
	binary.Write(&buf, binary.LittleEndian, headerSize) // size
	binary.Write(&buf, binary.LittleEndian, uint32(0))  // dummy
	binary.Write(&buf, binary.LittleEndian, uint32(8))  // blockOffset (8 means it starts at byte 16)
	binary.Write(&buf, binary.LittleEndian, uint32(1))  // blockCount (1 block)

	// Block Table entry (12 bytes)
	buf.WriteString("DATA")
	binary.Write(&buf, binary.LittleEndian, uint32(8))              // off
	binary.Write(&buf, binary.LittleEndian, uint32(len(dataBlock))) // length

	// The data block itself starts at byte 20
	buf.Write(dataBlock)

	// Pixels immediately follow at `headerSize`
	buf.Write(pixels)

	return buf.Bytes()
}

func buildInvalidBlockTable() []byte {
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, uint32(16)) // size
	binary.Write(&buf, binary.LittleEndian, uint32(0))  // dummy
	binary.Write(&buf, binary.LittleEndian, uint32(0))  // blockOffset
	binary.Write(&buf, binary.LittleEndian, uint32(5))  // blockCount (5*12 = 60 bytes, but file will be shorter)
	return buf.Bytes()
}

func buildOffsetLargerThanFile() []byte {
	raw := buildTexture(1, 1, formatBGRA, []byte{0, 0, 0, 0}, false)
	// Make size larger than actual file length
	binary.LittleEndian.PutUint32(raw[0:4], uint32(len(raw)+10))
	return raw
}

func TestTextureErrors(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
		err  string
	}{
		{
			name: "smaller than 16 bytes",
			raw:  make([]byte, 10),
			err:  "at least 16 bytes",
		},
		{
			name: "block table runs past end",
			raw:  buildInvalidBlockTable(),
			err:  "runs past its end",
		},
		{
			name: "missing description",
			raw:  buildTexture(1, 1, formatBGRA, nil, true), // makeInvalidDataBlock=true
			err:  "no description of itself",
		},
		{
			name: "zero width",
			raw:  buildTexture(0, 1, formatBGRA, []byte{0, 0, 0, 0}, false),
			err:  "says it is 0x1",
		},
		{
			name: "size larger than file",
			raw:  buildOffsetLargerThanFile(),
			err:  "says it is",
		},
		{
			name: "unsupported format",
			raw:  buildTexture(1, 1, 99, []byte{0, 0, 0, 0}, false),
			err:  "isn't one this knows",
		},
		{
			name: "insufficient bytes for BGRA",
			raw:  buildTexture(2, 2, formatBGRA, []byte{0, 0, 0, 0}, false),
			err:  "needs 16 bytes, got 4",
		},
		{
			name: "insufficient bytes for DXT5",
			raw:  buildTexture(4, 4, formatDXT5, []byte{0, 0, 0, 0}, false),
			err:  "needs 16 bytes of DXT5, got 4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Texture(tt.raw)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.err) {
				t.Errorf("expected error containing %q, got %q", tt.err, err.Error())
			}
		})
	}
}

func TestTextureBGRA(t *testing.T) {
	// 2x2 BGRA texture = 4 pixels = 16 bytes
	pixels := []byte{
		255, 0, 0, 255, // Blue, Green, Red, Alpha -> Blue pixel (b=255, g=0, r=0, a=255)
		0, 255, 0, 255, // Green pixel
		0, 0, 255, 255, // Red pixel
		0, 0, 0, 128, // Transparent black
	}
	raw := buildTexture(2, 2, formatBGRA, pixels, false)
	img, err := Texture(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rgba, ok := img.(*image.RGBA)
	if !ok {
		t.Fatalf("expected *image.RGBA, got %T", img)
	}

	// Verify dimensions
	if rgba.Bounds().Dx() != 2 || rgba.Bounds().Dy() != 2 {
		t.Errorf("expected 2x2, got %dx%d", rgba.Bounds().Dx(), rgba.Bounds().Dy())
	}

	// Verify pixel 0 (Blue) -> img stores as RGBA so it should be R=0, G=0, B=255, A=255
	if rgba.Pix[0] != 0 || rgba.Pix[1] != 0 || rgba.Pix[2] != 255 || rgba.Pix[3] != 255 {
		t.Errorf("expected blue, got %v", rgba.Pix[0:4])
	}
}

func TestTexturePNG(t *testing.T) {
	// Let's create a minimal 1x1 PNG image
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Pix = []byte{255, 128, 64, 255} // RGBA

	var buf bytes.Buffer
	err := png.Encode(&buf, img)
	if err != nil {
		t.Fatalf("failed to encode mock png: %v", err)
	}

	raw := buildTexture(1, 1, formatPNGRGBA, buf.Bytes(), false)
	outImg, err := Texture(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	bounds := outImg.Bounds()
	if bounds.Dx() != 1 || bounds.Dy() != 1 {
		t.Errorf("expected 1x1, got %dx%d", bounds.Dx(), bounds.Dy())
	}

	r, g, b, a := outImg.At(0, 0).RGBA()
	// Go's color.RGBA() returns values 0-65535, so 255 -> 65535, 128 -> ~32896, 64 -> ~16448
	if r>>8 != 255 || g>>8 != 128 || b>>8 != 64 || a>>8 != 255 {
		t.Errorf("expected 255, 128, 64, 255, got %d %d %d %d", r>>8, g>>8, b>>8, a>>8)
	}
}

func TestTextureDXT5(t *testing.T) {
	// A single DXT5 block is 16 bytes for a 4x4 area.
	// The first 8 bytes encode alpha.
	// alpha[0]=255, alpha[1]=0
	// 6 bytes of indices (0 = use alpha[0])
	alphaBlock := []byte{255, 0, 0, 0, 0, 0, 0, 0}

	// The next 8 bytes encode color.
	// c0, c1 (2 bytes each in RGB565)
	// c0 = 0xFFFF (White)
	// c1 = 0x0000 (Black)
	// 4 bytes of indices (0 = use c0)
	colorBlock := []byte{0xFF, 0xFF, 0x00, 0x00, 0, 0, 0, 0}

	pixels := append(alphaBlock, colorBlock...)
	raw := buildTexture(4, 4, formatDXT5, pixels, false)

	img, err := Texture(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rgba, ok := img.(*image.RGBA)
	if !ok {
		t.Fatalf("expected *image.RGBA, got %T", img)
	}

	if rgba.Bounds().Dx() != 4 || rgba.Bounds().Dy() != 4 {
		t.Errorf("expected 4x4, got %dx%d", rgba.Bounds().Dx(), rgba.Bounds().Dy())
	}

	// First pixel should be white (255,255,255) with alpha 255
	if rgba.Pix[0] != 255 || rgba.Pix[1] != 255 || rgba.Pix[2] != 255 || rgba.Pix[3] != 255 {
		t.Errorf("expected white with full alpha, got %v", rgba.Pix[0:4])
	}

	// Now let's craft a YCoCg block to test the looksYCoCg logic.
	alphaBlockYCoCg := []byte{255, 0, 0x08, 0, 0, 0, 0, 0}
	colorBlockYCoCg := []byte{0x00, 0x00, 0x00, 0x00, 0, 0, 0, 0}
	pixelsYCoCg := append(alphaBlockYCoCg, colorBlockYCoCg...)

	rawYCoCg := buildTexture(4, 4, formatDXT5, pixelsYCoCg, false)
	imgYCoCg, err := Texture(rawYCoCg)
	if err != nil {
		t.Fatalf("unexpected error YCoCg: %v", err)
	}

	rgbaYCoCg, ok := imgYCoCg.(*image.RGBA)
	if !ok {
		t.Fatalf("expected *image.RGBA, got %T", imgYCoCg)
	}

	if rgbaYCoCg.Bounds().Dx() != 4 || rgbaYCoCg.Bounds().Dy() != 4 {
		t.Errorf("expected 4x4, got %dx%d", rgbaYCoCg.Bounds().Dx(), rgbaYCoCg.Bounds().Dy())
	}

	// Test alpha[0] <= alpha[1] path
	alphaBlockReverse := []byte{0, 255, 0, 0, 0, 0, 0, 0}
	pixelsReverse := append(alphaBlockReverse, colorBlock...)
	rawReverse := buildTexture(4, 4, formatDXT5, pixelsReverse, false)
	_, err = Texture(rawReverse)
	if err != nil {
		t.Fatalf("unexpected error reverse alpha: %v", err)
	}
}
