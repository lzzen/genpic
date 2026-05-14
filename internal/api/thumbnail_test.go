package api

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteJPEGThumbFile_smoke(t *testing.T) {
	const size = 800
	im := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			im.SetRGBA(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var enc bytes.Buffer
	if err := png.Encode(&enc, im); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "0_thumb.jpg")
	if err := writeJPEGThumbFile(enc.Bytes(), "image/png", out); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() > 200_000 {
		t.Fatalf("thumb unexpectedly large: %d bytes", fi.Size())
	}
}
