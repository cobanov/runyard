package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"io"
	"strconv"
	"strings"
)

// Sample images are megabytes each and a grid shows dozens: ?w= on a media URL serves a JPEG no wider than one of
// these, made on first use and kept beside the original (so it goes when the run is deleted).
var thumbWidths = []int{320, 640, 1280}

// thumbWidth is the smallest kept width that is at least w, or 0 when w asks for none or for more than the largest.
func thumbWidth(w int) int {
	for _, t := range thumbWidths {
		if w > 0 && w <= t {
			return t
		}
	}
	return 0
}

func thumbKey(key string, width int) string { return key + ".w" + strconv.Itoa(width) + ".jpg" }

var thumbSlots = make(chan struct{}, 2) // decoding a 4K sample takes 50 MB; two at a time is enough

// thumb returns the key of the image at key scaled down to width, making it if needed. ok is false when there is
// nothing smaller to serve: not an image this can read, already narrow enough, or too large to decode safely.
func (s *srv) thumb(ctx context.Context, key string, width int) (string, bool) {
	small := thumbKey(key, width)
	if f, err := s.store.open(ctx, small); err == nil {
		f.Close()
		return small, true
	}
	if ext := key[strings.LastIndex(key, ".")+1:]; ext != "png" && ext != "jpg" && ext != "jpeg" { // a GIF is a video here, and must keep moving
		return "", false
	}
	thumbSlots <- struct{}{}
	defer func() { <-thumbSlots }()
	f, err := s.store.open(ctx, key)
	if err != nil {
		return "", false
	}
	defer f.Close()
	var head bytes.Buffer
	cfg, _, err := image.DecodeConfig(io.TeeReader(f, &head))
	if err != nil || cfg.Width <= width || cfg.Width*cfg.Height > 40_000_000 {
		return "", false
	}
	src, _, err := image.Decode(io.MultiReader(&head, f))
	if err != nil {
		return "", false
	}
	var out bytes.Buffer
	if jpeg.Encode(&out, shrink(src, width), &jpeg.Options{Quality: 82}) != nil {
		return "", false
	}
	if s.store.put(context.WithoutCancel(ctx), small, &out, int64(out.Len())) != nil {
		return "", false
	}
	return small, true
}

// shrink scales src down to width by averaging the source pixels under each new one, on white where it is
// transparent (JPEG has no alpha).
func shrink(src image.Image, width int) image.Image {
	b := src.Bounds()
	flat := image.NewRGBA(b)
	draw.Draw(flat, b, image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(flat, b, src, b.Min, draw.Over)
	height := max(1, b.Dy()*width/b.Dx())
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		y0, y1 := y*b.Dy()/height, max((y+1)*b.Dy()/height, y*b.Dy()/height+1)
		for x := range width {
			x0, x1 := x*b.Dx()/width, max((x+1)*b.Dx()/width, x*b.Dx()/width+1)
			var r, g, bl, n int
			for sy := y0; sy < y1; sy++ {
				row := flat.Pix[sy*flat.Stride+x0*4 : sy*flat.Stride+x1*4]
				for i := 0; i < len(row); i += 4 {
					r, g, bl, n = r+int(row[i]), g+int(row[i+1]), bl+int(row[i+2]), n+1
				}
			}
			dst.SetRGBA(x, y, color.RGBA{uint8(r / n), uint8(g / n), uint8(bl / n), 255})
		}
	}
	return dst
}
