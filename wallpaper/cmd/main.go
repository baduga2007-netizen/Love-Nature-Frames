package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"lnf/wallpaper"
)

func main() {
	cacheDir := filepath.Join(os.Getenv("HOME"), "LNF", "wallpaper", "cache")
	cfg := wallpaper.DefaultConfig()
	cfg.CacheDir = cacheDir

	mgr, err := wallpaper.NewManager(cfg)
	if err != nil {
		log.Fatalf("wallpaper.NewManager: %v", err)
	}

	target := findImage()
	if target == "" {
		target = makeTestImage(cacheDir)
	}

	if err := mgr.SetWallpaper(target); err != nil {
		log.Fatalf("SetWallpaper(%q): %v", target, err)
	}

	fmt.Printf("applied wallpaper: %s\n", mgr.Current())
}

func findImage() string {
	dirs := []string{
		filepath.Join(os.Getenv("HOME"), "LNF", "wallpaper"),
		filepath.Join(os.Getenv("HOME"), "Изображения"),
		filepath.Join(os.Getenv("HOME"), "Pictures"),
		filepath.Join(os.Getenv("HOME"), "Pictures", "Wallpapers"),
	}
	for _, dir := range dirs {
		matches, err := filepath.Glob(filepath.Join(dir, "*"))
		if err != nil {
			continue
		}
		for _, m := range matches {
			if strings.HasSuffix(m, ".png") || strings.HasSuffix(m, ".jpg") || strings.HasSuffix(m, ".jpeg") || strings.HasSuffix(m, ".webp") {
				if info, err := os.Stat(m); err == nil && info.Size() > 0 {
					return m
				}
			}
		}
	}
	return ""
}

func makeTestImage(cacheDir string) string {
	const data = "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00\x90wS\xde\x00\x00\x00\x0cIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82"
	p := filepath.Join(cacheDir, "test-wallpaper.png")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		log.Fatalf("MkdirAll(%q): %v", cacheDir, err)
	}
	if err := os.WriteFile(p, []byte(data), 0644); err != nil {
		log.Fatalf("WriteFile(%q): %v", p, err)
	}
	return p
}
