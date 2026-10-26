package main

import (
	"fmt"
	"image"
	"testing"
)

func benchScene(b *testing.B, name string, w, h int, groups ...string) {
	var cfg *Config
	for _, c := range panelConfig() {
		if c.Namespace == "lnf-"+name {
			cfg = c
		}
	}
	if cfg == nil {
		b.Fatalf("нет сцены %s", name)
	}
	sc := NewScene(cfg)
	for _, g := range groups {
		sc.groups[g] = true
	}
	sc.PollOnce()
	sc.RefreshText()
	for _, l := range cfg.Layers {
		if sc.shown(l) {
			l.appearT = 1
		}
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	sc.Render(img, float64(w), float64(h))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sc.Render(img, float64(w), float64(h))
	}
}

func BenchmarkAppsRender(b *testing.B)   { benchScene(b, "apps", 520, 1080, "apps") }
func BenchmarkCookieRender(b *testing.B) { benchScene(b, "cookie", 340, 360, "fan") }
func BenchmarkShotRender(b *testing.B)   { benchScene(b, "pop", 384, 384, "shot") }

// BenchmarkHeavyRender — синтетический «жирный» конфиг: сотни слоёв
// с текстом и скруглёнными плашками на полноэкранной поверхности.
func BenchmarkHeavyRender(b *testing.B) {
	cfg := newConfig("heavy")
	cfg.W, cfg.H = 1920, 1080
	cfg.BG, cfg.FontPath = col("#101010ff"), uiFont()
	cfg.FontSize = 12
	for i := 0; i < 300; i++ {
		l := newLayer(fmt.Sprintf("l%d", i))
		l.Radius, l.FG = 8, col("#203040ff")
		l.W, l.H = 120, 32
		l.X, l.Y = float64(i%16)*120, float64(i/16)*36
		l.Text, l.TextFG, l.HasTextFG = fmt.Sprintf("слой %d", i), col("#d0d0d0ff"), true
		l.FontSize = 12
		cfg.Layers = append(cfg.Layers, l)
	}
	sc := NewScene(cfg)
	img := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	sc.Render(img, 1920, 1080)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sc.Render(img, 1920, 1080)
	}
}
