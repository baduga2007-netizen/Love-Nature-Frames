package wallpaper

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// ErrNoWallpaper — ошибка отсутствия обоев.
var ErrNoWallpaper = fmt.Errorf("no wallpaper set")

// Manager — менеджер обоев.
type Manager struct {
	mu         sync.RWMutex
	cfg        *Config
	wm         *WM
	current    string
	cacheRoot  string
	transition TransitionConfig
}

// NewManager — создание менеджера обоев.
func NewManager(cfg *Config) (*Manager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.CacheDir == "" {
		cfg.CacheDir = "cache"
	}
	if err := os.MkdirAll(cfg.CacheDir, 0755); err != nil {
		return nil, err
	}
	if len(cfg.Transitions) == 0 {
		cfg.Transitions = []TransitionConfig{
			{
				Name:        "fade",
				Type:        "fade",
				Duration:    cfg.TransitionDuration,
				Easing:      "ease-in-out",
				Custom:      false,
			},
		}
	}
	m := &Manager{
		cfg:        cfg,
		wm:         NewWM(),
		cacheRoot:  cfg.CacheDir,
		transition: cfg.Transitions[0],
	}
	return m, nil
}

// SetTransition — установка перехода.
func (m *Manager) SetTransition(t TransitionConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.transition = t
}

// Transition — получение текущего перехода.
func (m *Manager) Transition() TransitionConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.transition
}

// SetCurrent — установка текущего обоев.
func (m *Manager) SetCurrent(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.current = path
}

// Current — получение текущего обоев.
func (m *Manager) Current() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

// ClearCurrent — сброс текущих обоев.
func (m *Manager) ClearCurrent() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.current = ""
}

// SetWallpaper — установка обоев из файла.
func (m *Manager) SetWallpaper(path string) error {
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		path = abs
	}
	if err := validateWallpaperFile(path); err != nil {
		return err
	}
	if err := m.setWallpaperInternal(path); err != nil {
		return err
	}
	m.SetCurrent(path)
	return nil
}

// SetWallpaperData — установка обоев из данных.
func (m *Manager) SetWallpaperData(data []byte) error {
	tmp, err := os.CreateTemp(m.cacheRoot, "wallpaper-*.png")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return m.SetWallpaper(tmp.Name())
}

// LoadWallpaper — загрузка текущих обоев в виде []byte.
func (m *Manager) LoadWallpaper() ([]byte, error) {
	p := m.Current()
	if p == "" {
		return nil, fmt.Errorf("wallpaper not set: %w", ErrNoWallpaper)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func validateWallpaperFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("path %q is a directory", path)
	}
	if info.Size() == 0 {
		return fmt.Errorf("empty file")
	}
	return nil
}

func (m *Manager) setWallpaperInternal(path string) error {
	t := m.Transition()
	if t.Custom && t.CustomScript != "" {
		if err := m.runCustomTransition(path); err != nil {
			return fmt.Errorf("custom transition: %w", err)
		}
	}
	return nil
}

func (m *Manager) runCustomTransition(path string) error {
	return nil
}
