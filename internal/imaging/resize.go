package imaging

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
)

// ErrUnsupportedImage means the bytes are not a JPEG, PNG or GIF this build can decode.
var ErrUnsupportedImage = errors.New("the file is not a JPEG, PNG or GIF image")

// maxPixels guards the decoder against decompression bombs.
const maxPixels = 25_000_000

// FitJPEG decodes an uploaded picture and returns it as a JPEG that fits the
// card frame (1080×1440): proportions are kept, a larger picture is scaled down
// and a smaller one is left alone. Transparency is flattened onto white.
func FitJPEG(data []byte) ([]byte, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedImage, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxPixels {
		return nil, fmt.Errorf("%w: unsupported dimensions %dx%d", ErrUnsupportedImage, cfg.Width, cfg.Height)
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedImage, err)
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if w > cardWidth || h > cardHeight {
		scale := min(float64(cardWidth)/float64(w), float64(cardHeight)/float64(h))
		dw = max(1, int(float64(w)*scale+0.5))
		dh = max(1, int(float64(h)*scale+0.5))
	}

	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	if dw == w && dh == h {
		draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Over)
	} else {
		areaScale(dst, src)
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 88}); err != nil {
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}
	return out.Bytes(), nil
}

// areaScale shrinks src into dst by averaging every source pixel that falls in
// a destination pixel (box filter), compositing over the white already in dst.
func areaScale(dst *image.RGBA, src image.Image) {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dw, dh := dst.Bounds().Dx(), dst.Bounds().Dy()
	for y := 0; y < dh; y++ {
		y0, y1 := y*sh/dh, (y+1)*sh/dh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < dw; x++ {
			x0, x1 := x*sw/dw, (x+1)*sw/dw
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, bl, a, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					pr, pg, pb, pa := src.At(b.Min.X+sx, b.Min.Y+sy).RGBA()
					r += uint64(pr)
					g += uint64(pg)
					bl += uint64(pb)
					a += uint64(pa)
					n++
				}
			}
			// Premultiplied averages; add the white that shows through.
			r, g, bl, a = r/n, g/n, bl/n, a/n
			inv := 0xffff - a
			dst.SetRGBA(x, y, color.RGBA{
				R: uint8((r + inv) >> 8),
				G: uint8((g + inv) >> 8),
				B: uint8((bl + inv) >> 8),
				A: 0xff,
			})
		}
	}
}
