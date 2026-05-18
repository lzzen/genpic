package api

import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"strings"
)

// thumbMaxEdge is the longer side of generated JPEG previews (list / cards).
const thumbMaxEdge = 480

var jpegEncodeOpts = &jpeg.Options{Quality: 78}

func decodeRasterImage(b []byte, mime string) (image.Image, error) {
	if len(b) < 8 {
		return nil, fmt.Errorf("empty image")
	}
	m := strings.ToLower(strings.TrimSpace(mime))
	switch m {
	case "image/jpeg", "image/jpg":
		return jpeg.Decode(bytes.NewReader(b))
	case "image/png":
		return png.Decode(bytes.NewReader(b))
	case "image/gif":
		return gif.Decode(bytes.NewReader(b))
	case "image/webp":
		return nil, fmt.Errorf("webp preview not supported without extra decoder")
	}
	// Sniff when MIME missing or unknown.
	if b[0] == 0xff && b[1] == 0xd8 {
		return jpeg.Decode(bytes.NewReader(b))
	}
	if len(b) >= 6 && string(b[0:6]) == "GIF87a" || string(b[0:6]) == "GIF89a" {
		return gif.Decode(bytes.NewReader(b))
	}
	if len(b) >= 8 && b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G' {
		return png.Decode(bytes.NewReader(b))
	}
	if len(b) >= 12 && string(b[0:4]) == "RIFF" && string(b[8:12]) == "WEBP" {
		return nil, fmt.Errorf("webp preview not supported without extra decoder")
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err == nil {
		return img, nil
	}
	img2, err2 := jpeg.Decode(bytes.NewReader(b))
	if err2 == nil {
		return img2, nil
	}
	return nil, fmt.Errorf("decode image: png: %v; jpeg: %v", err, err2)
}

func resizeToMaxEdge(src image.Image, maxDim int) *image.RGBA {
	if maxDim < 32 {
		maxDim = 32
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	if w <= maxDim && h <= maxDim {
		out := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				out.Set(x, y, src.At(x+b.Min.X, y+b.Min.Y))
			}
		}
		return out
	}
	var nw, nh int
	if w >= h {
		nw = maxDim
		nh = max(1, (h*maxDim)/w)
	} else {
		nh = maxDim
		nw = max(1, (w*maxDim)/h)
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy := b.Min.Y + y*h/nh
		for x := 0; x < nw; x++ {
			sx := b.Min.X + x*w/nw
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

// writeJPEGThumbFile decodes image bytes and writes a downscaled JPEG to dstPath.
func writeJPEGThumbFile(srcBytes []byte, mime string, dstPath string) error {
	src, err := decodeRasterImage(srcBytes, mime)
	if err != nil {
		return err
	}
	small := resizeToMaxEdge(src, thumbMaxEdge)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, small, jpegEncodeOpts); err != nil {
		return err
	}
	tmp := dstPath + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dstPath)
}

// encodeJPEGThumbToBytes returns a JPEG preview (same dimensions/quality as artifact thumbs).
func encodeJPEGThumbToBytes(srcBytes []byte, mime string) ([]byte, error) {
	src, err := decodeRasterImage(srcBytes, mime)
	if err != nil {
		return nil, err
	}
	small := resizeToMaxEdge(src, thumbMaxEdge)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, small, jpegEncodeOpts); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
