package main

import (
	"image"
	"image/color"
	"testing"
)

func solid(w, h int, c color.NRGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i+0] = c.R
		img.Pix[i+1] = c.G
		img.Pix[i+2] = c.B
		img.Pix[i+3] = 0xFF
	}
	return img
}

func TestWPBlendInto(t *testing.T) {
	a := solid(4, 4, color.NRGBA{10, 20, 30, 255})
	b := solid(4, 4, color.NRGBA{110, 220, 250, 255})
	dst := image.NewRGBA(image.Rect(0, 0, 4, 4))

	wpBlendInto(a, b, 0, dst)
	if dst.Pix[0] != 10 || dst.Pix[1] != 20 || dst.Pix[2] != 30 {
		t.Fatalf("t=0 даёт %v, ждали a", dst.Pix[:4])
	}
	wpBlendInto(a, b, 1, dst)
	if dst.Pix[0] != 110 || dst.Pix[1] != 220 || dst.Pix[2] != 250 {
		t.Fatalf("t=1 даёт %v, ждали b", dst.Pix[:4])
	}
	wpBlendInto(a, b, 0.5, dst)
	// (10+110)/2, (20+220)/2, (30+250)/2 — с допуском на округление
	for i, want := range []uint8{60, 120, 140} {
		if got := dst.Pix[i]; got < want-1 || got > want+1 {
			t.Fatalf("t=0.5 канал %d = %d, ждали ~%d", i, got, want)
		}
	}
	if dst.Pix[3] != 0xFF {
		t.Fatalf("альфа не непрозрачная: %d", dst.Pix[3])
	}
}

func TestWPBlendIntoNil(t *testing.T) {
	b := solid(2, 2, color.NRGBA{7, 8, 9, 255})
	dst := image.NewRGBA(image.Rect(0, 0, 2, 2))
	wpBlendInto(nil, b, 0.4, dst) // без паники и с копией b
	if dst.Pix[0] != 7 {
		t.Fatalf("при nil-источнике ждали копию b, получили %d", dst.Pix[0])
	}
}

func TestWPCoverSize(t *testing.T) {
	src := solid(400, 100, color.NRGBA{1, 2, 3, 255})
	out := wpCover(src, 200, 300)
	if out.Bounds().Dx() != 200 || out.Bounds().Dy() != 300 {
		t.Fatalf("cover дал %v, ждали 200x300", out.Bounds())
	}
	if out.Pix[3] != 0xFF {
		t.Fatal("cover оставил прозрачные пиксели")
	}
}

func TestWPGenerateSize(t *testing.T) {
	for _, name := range []string{"sage", "moss", "dew"} {
		img := wpGenerate(name, 64, 48)
		if img.Bounds().Dx() != 64 || img.Bounds().Dy() != 48 {
			t.Fatalf("%s: размер %v", name, img.Bounds())
		}
		if img.Pix[3] != 0xFF {
			t.Fatalf("%s: прозрачный фон", name)
		}
	}
}
