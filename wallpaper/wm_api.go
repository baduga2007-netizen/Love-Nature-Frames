package wallpaper

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

// WMType — тип WM по переменным окружения/процессам.
type WMType int

const (
	WMUnknown WMType = iota
	WMSway
	WMNiri
	WMHyprland
)

// DetectWM — определение WM.
func DetectWM() WMType {
	if _, ok := os.LookupEnv("SWAYSOCK"); ok {
		return WMSway
	}
	if _, ok := os.LookupEnv("NIRI_SOCKET"); ok {
		return WMNiri
	}
	if _, ok := os.LookupEnv("HYPRLAND_INSTANCE_SIGNATURE"); ok {
		return WMHyprland
	}
	return WMUnknown
}

// WM — утилита управления WM.
type WM struct {
	wm WMType
}

// NewWM — создание WM.
func NewWM() *WM { return &WM{wm: DetectWM()} }

// Workspace — структура воркспейса.
type Workspace struct {
	Num      int    `json:"num"`
	Focused  bool   `json:"focused,omitempty"`
	Viewport string `json:"viewport,omitempty"`
	Output   string `json:"output,omitempty"`
}

// RunE — выполнение команды.
func (w *WM) RunE(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// Spawn — запуск процесса через WM.
func (w *WM) Spawn(command string) error {
	switch w.wm {
	case WMSway:
		return w.RunE("swaymsg", "exec", "--", command)
	case WMNiri:
		return w.Niri("action", "spawn", command)
	case WMHyprland:
		return w.RunE("hyprctl", "dispatch", "exec", "--", command)
	default:
		return w.RunE("setsid", command)
	}
}

// Niri — вызов niri.
func (w *WM) Niri(args ...string) error {
	cmd := exec.Command("niri", args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

// WorkspaceCreate — создание воркспейса.
func (w *WM) WorkspaceCreate(name string) error {
	switch w.wm {
	case WMSway:
		return w.RunE("swaymsg", "workspace", "number", name)
	case WMHyprland:
		return w.RunE("hyprctl", "dispatch", "workspace", name)
	case WMNiri:
		return w.Niri("action", "create-workspace", name)
	default:
		return nil
	}
}

// WorkspaceSwitch — переключение воркспейса.
func (w *WM) WorkspaceSwitch(name string) error {
	switch w.wm {
	case WMSway:
		return w.RunE("swaymsg", "workspace", name)
	case WMHyprland:
		return w.RunE("hyprctl", "dispatch", "workspace", name)
	case WMNiri:
		return w.Niri("action", "focus-workspace", name)
	default:
		return nil
	}
}

// WorkspaceRemove — удаление воркспейса.
func (w *WM) WorkspaceRemove(name string) error {
	switch w.wm {
	case WMSway:
		return w.RunE("swaymsg", "workspace", "empty", name)
	case WMHyprland:
		return w.RunE("hyprctl", "dispatch", "workspace", name)
	case WMNiri:
		return w.Niri("action", "destroy-workspace", name)
	default:
		return nil
	}
}

// GoToWorkspace — переход к соседнему воркспейсу.
func (w *WM) GoToWorkspace(direction int) error {
	switch w.wm {
	case WMSway:
		p := "next"
		if direction < 0 {
			p = "prev"
		}
		return w.RunE("swaymsg", "workspace", p)
	case WMHyprland:
		p := "e+1"
		if direction < 0 {
			p = "e-1"
		}
		return w.RunE("hyprctl", "dispatch", "workspace", p)
	case WMNiri:
		a := "focus-workspace-up"
		if direction < 0 {
			a = "focus-workspace-down"
		}
		return w.Niri("action", a)
	default:
		return nil
	}
}

// Screenshot — скриншот экрана.
func (w *WM) Screenshot(out string) error {
	return w.RunE("grim", out)
}

// ScreenshotGeometry — скриншот с геометрией.
func (w *WM) ScreenshotGeometry(out, geometry string) error {
	return exec.Command("grim", "-g", geometry, out).Run()
}

// ScreenshotWindow — скриншот окна.
func (w *WM) ScreenshotWindow(out string) error {
	switch w.wm {
	case WMSway:
		return w.RunE("swaymsg", "-t", "get_tree")
	case WMHyprland:
		return w.RunE("hyprctl", "-j", "activewindow")
	default:
		return exec.Command("grim", out).Run()
	}
}

// Lock — блокировка экрана.
func (w *WM) Lock() {
	switch w.wm {
	case WMSway:
		if _, err := exec.LookPath("hyprlock"); err == nil {
			exec.Command("hyprlock").Run()
		} else if _, err := exec.LookPath("swaylock"); err == nil {
			exec.Command("setsid", "swaylock", "-f", "-c", "#2e3440").Run()
		} else {
			if _, err := exec.LookPath("loginctl"); err == nil {
				exec.Command("loginctl", "lock-session").Run()
			}
		}
	case WMHyprland:
		if _, err := exec.LookPath("hyprlock"); err == nil {
			exec.Command("hyprlock").Run()
		}
	case WMNiri:
		if _, err := exec.LookPath("swaylock"); err == nil {
			exec.Command("setsid", "swaylock", "-f", "-c", "#2e3440").Run()
		}
	}
}

// Logout — завершение сеанса.
func (w *WM) Logout() {
	switch w.wm {
	case WMSway:
		_ = w.RunE("swaymsg", "exit")
	case WMHyprland:
		_ = w.RunE("hyprctl", "dispatch", "exit")
	case WMNiri:
		_ = w.Niri("action", "quit", "--skip-confirmation")
	}
}

// Restart — перезапуск WM.
func (w *WM) Restart() {
	switch w.wm {
	case WMSway:
		_ = w.RunE("swaymsg", "restart")
	case WMHyprland:
		_ = w.RunE("hyprctl", "dispatch", "restart")
	case WMNiri:
		_ = w.Niri("action", "restart")
	}
}

// GetWorkspaceList — получение списка воркспейсов.
func (w *WM) GetWorkspaceList() ([]Workspace, error) {
	switch w.wm {
	case WMSway:
		return w.getSwayWorkspaces()
	case WMHyprland:
		return w.getHyprlandWorkspaces()
	case WMNiri:
		return w.getNiriWorkspaces()
	default:
		return nil, nil
	}
}

// getSwayWorkspaces — получение воркспейсов в sway.
func (w *WM) getSwayWorkspaces() ([]Workspace, error) {
	cmd := exec.Command("swaymsg", "-t", "get_workspaces")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var data []Workspace
	if err := json.Unmarshal(out, &data); err != nil {
		return nil, err
	}
	return data, nil
}

// getHyprlandWorkspaces — получение воркспейсов в Hyprland.
func (w *WM) getHyprlandWorkspaces() ([]Workspace, error) {
	cmd := exec.Command("hyprctl", "-j", "workspaces")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var data []Workspace
	if err := json.Unmarshal(out, &data); err != nil {
		return nil, err
	}
	return data, nil
}

// getNiriWorkspaces — получение воркспейсов в Niri.
func (w *WM) getNiriWorkspaces() ([]Workspace, error) {
	cmd := exec.Command("niri", "msg", "--json", "workspaces")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var data []Workspace
	if err := json.Unmarshal(out, &data); err != nil {
		return nil, err
	}
	return data, nil
}
