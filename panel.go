package main

// ============================================================
// LNF · «Печенька» — единственный виджет рабочего стола.
//
// Material 3, чистые формы, одна плавающая поверхность в правом нижнем
// углу — не приклеенная к краю панель, а виджет с отступами от краёв.
// Корпус — настоящая печенька: семь круглых бугорков, контур взят готовым
// SVG-путём (см. cookieOutline), а не волной из косинусов.
//
// До раскрытия на виджете: время, дата, ресурсы, бейдж воркспейса и
// по нижней дуге — блокировка, сон, выход и выключение.
// Клик по корпусу раскрывает веер действий слева сверху: приложения,
// буфер обмена, скриншот, обои. Каждое действие — своя карточка слева
// от виджета; «Приложения» живут отдельной панелью у левого края, с поиском.
//
// Скриншот: задержка 0/3/5/10 с и зона (весь экран / область / окно).
// Выбор лежит в ~/.cache/lnf/shot.{sec,zone}, снимает shot.sh.
//
// Клик в любой точке печеньки — корпус, тарелка, время, дата, ресурсы —
// раскрывает веер: подписи тоже переключают группу, а не пропускают клик.
// ============================================================

import (
	"fmt"
	"image/color"
	"math"
	"os"
	"strings"
	"time"
)

// ------------------------------------------------------------
// Помощники конфига
// ------------------------------------------------------------

func uiFont() string {
	for _, p := range []string{
		"/usr/share/fonts/TTF/JetBrainsMonoNerdFont-Regular.ttf",
		"/usr/share/fonts/TTF/JetBrainsMonoNerdFontMono-Regular.ttf",
		"/usr/share/fonts/jetbrains-mono-nerd/JetBrainsMonoNerdFont-Regular.ttf",
		"/usr/share/fonts/ttf/jetbrains-mono-nerd/JetBrainsMonoNerdFont-Regular.ttf",
		"/usr/share/fonts/JetBrainsMonoNerdFont-Regular.ttf",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return defaultFont()
}

func col(s string) color.NRGBA {
	c, err := parseColor(s)
	if err != nil {
		panic(err)
	}
	return c
}

func ms(v float64) float64 { return v / 1000 }

func newConfig(name string) *Config {
	ns := "lnf"
	if name != "" {
		ns = "lnf-" + name
	}
	return &Config{
		Title:     "LNF",
		W:         200,
		H:         40,
		Layer:     "top",
		Anchor:    anchorTop,
		Namespace: ns,
		Exclusive: -1,
		FontPath:  defaultFont(),
		FontSize:  14,
	}
}

// pathShape превращает строку «M/L/C/Z» в готовые команды пути.
func pathShape(d string) []pathCmd {
	c, err := parsePath(d)
	if err != nil {
		panic(err)
	}
	return c
}

// cookieOutline — контур печеньки: семь круглых бугорков.
// Координаты в исходном квадрате 100×100, центр (50, 49.02).
const cookieOutline = `M 50.00,4.00
C 56.01,4.00 61.75,11.75 67.16,14.36
C 72.58,16.97 82.21,16.62 85.96,21.32
C 89.71,26.02 87.23,35.33 88.57,41.20
C 89.91,47.06 96.18,54.37 94.85,60.24
C 93.51,66.10 84.68,69.96 80.93,74.67
C 77.18,79.37 75.38,88.84 69.96,91.44
C 64.54,94.05 56.01,89.56 50.00,89.56
C 43.99,89.56 35.46,94.05 30.04,91.44
C 24.62,88.84 22.82,79.37 19.07,74.67
C 15.32,69.96 6.49,66.10 5.15,60.24
C 3.82,54.37 10.09,47.06 11.43,41.20
C 12.77,35.33 10.29,26.02 14.04,21.32
C 17.79,16.62 27.42,16.97 32.84,14.36
C 38.25,11.75 43.99,4.00 50.00,4.00 Z`

// cookiePath переводит контур печеньки в нормированные координаты слоя
// ([-1..1] — границы прямоугольника слоя).
func cookiePath() []pathCmd {
	const (
		cx = 50.0
		cy = 49.02
		k  = 46.2 // половина большей стороны исходного квадрата
	)
	cmds := pathShape(cookieOutline)
	for i := range cmds {
		for j := range cmds[i].pts {
			cmds[i].pts[j][0] = (cmds[i].pts[j][0] - cx) / k
			cmds[i].pts[j][1] = (cmds[i].pts[j][1] - cy) / k
		}
	}
	return cmds
}

// ------------------------------------------------------------
// Палитра M3 (matugen) + оливковый фолбэк
// ------------------------------------------------------------

type m3 struct {
	surface, low, container, high, outline,
	onSurface, onVariant,
	primary, secondary, errorC color.NRGBA
}

func loadPalette() m3 {
	file := func() string {
		if cd, err := os.UserCacheDir(); err == nil {
			return cd + "/lnf/palette"
		}
		return os.TempDir() + "/lnf/palette"
	}()
	olive := m3{
		surface:   col("#162714FF"),
		low:       col("#181D18FF"),
		container: col("#1C211CFF"),
		high:      col("#262B26FF"),
		outline:   col("#414941FF"),
		onSurface: col("#DFE4DCFF"),
		onVariant: col("#C1C9BFFF"),
		primary:   col("#97D5A5FF"),
		secondary: col("#B7CCB8FF"),
		errorC:    col("#FFB4ABFF"),
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return olive
	}
	p := olive
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		c, err := parseColor(strings.TrimSpace(v))
		if err != nil {
			continue
		}
		switch k {
		case "bg0":
			p.container = c
		case "bg1":
			p.low = c
		case "bg2":
			p.high = c
		case "bg3":
			p.outline = c
		case "fg0":
			p.onSurface = c
		case "fg1":
			p.onVariant = c
		case "accent":
			p.primary = c
		case "accent2":
			p.secondary = c
		case "danger":
			p.errorC = c
		}
	}
	return p
}

// ------------------------------------------------------------
// Геометрия виджета
// ------------------------------------------------------------
const (
	surfaceW, surfaceH = 340.0, 360.0
	cookieBox          = 190.0
	cookieCX           = 223.0
	cookieCY           = 243.0
	fanR               = 128.0
	fanBtn             = 54.0
	popW, popH         = 384.0, 384.0

	animFast = 160.0
	animMid  = 220.0
	animSlow = 300.0
)

func panelConfig() []*Config {
	pal := loadPalette()
	onPrimary := pal.surface

	// ============================================================
	// 1. Печенька: корпус, бейдж воркспейса, капсула сессии, веер
	// ============================================================
	cw := newConfig("cookie")
	cw.W, cw.H = surfaceW, surfaceH
	cw.Anchor = anchorBottom | anchorRight
	cw.Margin = [4]int32{0, 22, 10, 0}
	// Печенька живёт на bottom-слое: обычное окно swayfx всегда рисуется
	// поверх неё, в любом состоянии (свёрнута/раскрыта).
	cw.Layer = "bottom"
	cw.FontPath = uiFont()
	cw.FontSize = 12
	addC := func(l *Layer) { cw.Layers = append(cw.Layers, l) }

	// put — поставить слой по центру (cx, cy), привязка к правому нижнему углу.
	put := func(l *Layer, cx, cy, w, h float64) {
		l.Anchor, l.HasAnchor = anchorBottom|anchorRight, true
		l.W, l.H = w, h
		l.Margin = [4]float64{
			0,
			surfaceW - w - (cx - w/2),
			surfaceH - h - (cy - h/2),
			0,
		}
	}
	// polar — точка на окружности вокруг центра печеньки (угол вверх = 90).
	polar := func(deg, r float64) (float64, float64) {
		a := deg * math.Pi / 180
		return cookieCX + r*math.Cos(a), cookieCY - r*math.Sin(a)
	}

	// Корпус: настоящая печенька, tonal-заливка M3.
	body := newLayer("cookie-body")
	body.Cmds = cookiePath()
	body.FG, body.HoverFG, body.HasHover = pal.low, pal.container, true
	body.ClickScale = 0.03
	body.ClickDecay = 7
	body.Toggle = "fan"
	body.Appear = ms(animSlow)
	put(body, cookieCX, cookieCY, cookieBox, cookieBox)
	addC(body)

	// Тарелка — внутренний круг, на котором читается текст.
	// Печенька несимметрична по вертикали (центр площади ниже центра
	// прямоугольника на ~4 px), поэтому тарелку и подписи опускаем на 8 px
	// от геометрического центра: так они читаются по центру корпуса.
	// Больше нельзя — нижние бугорки и кнопки сессии.
	dish := newLayer("cookie-dish")
	dish.Radius, dish.FG = 46, pal.high
	dish.Toggle = "fan"
	put(dish, cookieCX, cookieCY-6, 92, 92)
	addC(dish)

	// Время, дата, ресурсы. Клик по любой из подписей — тоже раскрытие веера.
	clock := newLayer("cookie-clock")
	clock.Text, clock.TextFG, clock.HasTextFG = "{time:%H:%M}", pal.onSurface, true
	clock.TextAlign = "center"
	clock.FontSize = 28
	clock.Toggle = "fan"
	put(clock, cookieCX, cookieCY-22, 100, 36)
	addC(clock)

	date := newLayer("cookie-date")
	date.Text, date.TextFG, date.HasTextFG = "{time:%a %d %b}", pal.onVariant, true
	date.TextAlign = "center"
	date.FontSize = 10
	date.Toggle = "fan"
	put(date, cookieCX, cookieCY+4, 100, 14)
	addC(date)

	res := newLayer("cookie-res")
	res.Text, res.TextFG, res.HasTextFG = "{poll:sys}", pal.onVariant, true
	res.TextAlign = "center"
	res.FontSize = 9
	res.Toggle = "fan"
	put(res, cookieCX, cookieCY+18, 100, 12)
	addC(res)

	// Бейдж воркспейса — на верхнем правом бугорке.
	wsX, wsY := polar(38.6, 74)
	ws := newLayer("cookie-ws")
	ws.Radius, ws.FG = 18, pal.primary
	ws.HoverFG, ws.HasHover = pal.secondary, true
	ws.Text, ws.TextFG, ws.HasTextFG = "{poll:wsnum}", onPrimary, true
	ws.TextAlign = "center"
	ws.FontSize = 11
	ws.HoverScale = 1.08
	ws.ClickScale = 0.1
	ws.ClickExec = "$HOME/LNF/scripts/wm_control.sh ws-down"
	ws.WheelUp = "$HOME/LNF/scripts/wm_control.sh ws-down"
	ws.WheelDown = "$HOME/LNF/scripts/wm_control.sh ws-up"
	put(ws, wsX, wsY, 36, 36)
	addC(ws)

	// Сессия и питание — прямо на печеньке, по нижней дуге внутри контура.
	// Отдельной капсулы нет: виджет — один круг.
	sessions := []struct {
		name, glyph, cmd string
		danger           bool
	}{
		{"s-lock", "\uf023", "$HOME/LNF/scripts/wm_control.sh lock", false},
		{"s-suspend", "\uf186", "$HOME/LNF/scripts/power_control.sh suspend", false},
		{"s-logout", "\uf08b", "$HOME/LNF/scripts/wm_control.sh logout", false},
		{"s-off", "\uf011", "$HOME/LNF/scripts/power_control.sh poweroff", true},
	}
	for i, s := range sessions {
		l := newLayer(s.name)
		l.Radius, l.FG = 17, pal.low
		l.HoverFG, l.HasHover = pal.high, true
		l.Text, l.TextFG, l.HasTextFG = s.glyph, pal.onSurface, true
		l.TextAlign = "center"
		l.FontSize = 15
		l.HoverScale = 1.08
		l.ClickScale = 0.1
		l.ClickExec = s.cmd
		l.Appear = ms(animMid)
		if s.danger {
			l.TextHover, l.HasTextHi = pal.errorC, true
		} else {
			l.TextHover, l.HasTextHi = pal.primary, true
		}
		cx, cy := polar(219+float64(i)*34, 66)
		put(l, cx, cy, 34, 34)
		addC(l)
	}

	// Веер: четыре действия по дуге слева от корпуса, вылетают из центра.
	fan := []struct{ name, glyph, group string }{
		{"fan-apps", "\uf009", "apps"},
		{"fan-clip", "\uf0ea", "clip"},
		{"fan-shot", "\uf030", "shot"},
		{"fan-wall", "\uf03e", "walls"},
	}
	for i, f := range fan {
		l := newLayer(f.name)
		l.Radius, l.FG = fanBtn/2, pal.high
		l.HoverFG, l.HasHover = pal.primary, true
		l.Text, l.TextFG, l.HasTextFG = f.glyph, pal.onSurface, true
		l.TextHover, l.HasTextHi = onPrimary, true
		l.TextAlign = "center"
		l.FontSize = 20
		l.HoverScale = 1.08
		l.ClickScale = 0.12
		l.Toggle = f.group
		l.ShowWhen = "fan"
		l.Appear = ms(animMid + float64(i)*25)
		cx, cy := polar(96+float64(i)*28, fanR)
		l.AppearDX, l.AppearDY = cookieCX-cx, cookieCY-cy // вылет из корпуса
		put(l, cx, cy, fanBtn, fanBtn)
		addC(l)
	}

	cw.Polls = []*Poll{
		{Name: "wsnum", Command: "$HOME/LNF/scripts/workspace_status.sh | awk '{print $2}'", Interval: time.Second},
		{Name: "sys", Command: "$HOME/LNF/scripts/sys_resources.sh", Interval: 3 * time.Second},
	}

	// ============================================================
	// 2. Карточки действий: буфер, скриншот, обои — слева от виджета
	// ============================================================
	pop := newConfig("pop")
	pop.W, pop.H = popW, popH
	pop.Anchor = anchorBottom | anchorRight
	pop.Margin = [4]int32{0, 236, 10, 0}
	// Карточки действий — тоже под окнами: если окно накрыло виджет,
	// оно накроет и раскрытые карточки.
	pop.Layer = "bottom"
	pop.FontPath = uiFont()
	pop.FontSize = 12
	addP := func(l *Layer) { pop.Layers = append(pop.Layers, l) }

	// pput — позиция слоя от левого верхнего угла поверхности.
	pput := func(l *Layer, x, y, w, h float64) {
		l.Anchor, l.HasAnchor = anchorLeft|anchorTop, true
		l.W, l.H = w, h
		l.Margin = [4]float64{y, 0, 0, x}
	}

	// drawer — общий вид карточки: фон, заголовок, крестик закрытия.
	// Крестик — 48×48, чтобы в него не приходилось целиться.
	drawer := func(group, title string, h float64) {
		bg := newLayer(group + "-card")
		bg.Radius, bg.FG = 30, pal.high
		bg.ShowWhen = group
		bg.Appear, bg.AppearDY = ms(animMid), 18
		pput(bg, 12, 12, 360, h)
		addP(bg)

		t := newLayer(group + "-title")
		t.Text, t.TextFG, t.HasTextFG = title, pal.onSurface, true
		t.FontSize = 15
		t.TextPad = 2
		t.ShowWhen = group
		t.Appear, t.AppearDY = ms(animMid), 14
		pput(t, 32, 30, 250, 28)
		addP(t)

		x := newLayer(group + "-close")
		x.Radius, x.FG = 24, pal.low
		x.HoverFG, x.HasHover = pal.container, true
		x.Text, x.TextFG, x.HasTextFG = "\uf00d", pal.onVariant, true
		x.TextHover, x.HasTextHi = pal.onSurface, true
		x.TextAlign = "center"
		x.FontSize = 18
		x.HoverScale = 1.06
		x.ClickScale = 0.1
		x.Toggle = group
		x.ShowWhen = group
		x.Appear, x.AppearDY = ms(animMid), 14
		pput(x, 312, 22, 48, 48)
		addP(x)
	}

	// Буфер обмена: «очистить всё» + записи, ПКМ удаляет запись.
	drawer("clip", "Буфер обмена", 360)
	clipMenu := newLayer("clip-menu")
	clipMenu.MenuCmd = "$HOME/LNF/scripts/clipboard_history.sh"
	clipMenu.RowH = 40
	clipMenu.Pad = 12
	clipMenu.FontSize = 12
	clipMenu.FG, clipMenu.TextFG, clipMenu.HasTextFG = pal.low, pal.onSurface, true
	clipMenu.TextHover, clipMenu.HasTextHi = pal.primary, true
	clipMenu.HoverShift = [2]float64{3, 0}
	clipMenu.ShowWhen = "clip"
	clipMenu.Appear, clipMenu.AppearDY = ms(animMid), 12
	pput(clipMenu, 28, 84, 328, 264)
	addP(clipMenu)

	// Обои: каталог картинок миниатюрами + пара утилит (utils.sh).
	drawer("walls", "Обои", 360)
	wallMenu := newLayer("walls-menu")
	wallMenu.MenuCmd = "$HOME/LNF/scripts/utils_wallpaper.sh"
	wallMenu.RowH = 44
	wallMenu.Pad = 12
	wallMenu.FontSize = 12
	wallMenu.FG, wallMenu.TextFG, wallMenu.HasTextFG = pal.low, pal.onSurface, true
	wallMenu.TextHover, wallMenu.HasTextHi = pal.primary, true
	wallMenu.HoverShift = [2]float64{3, 0}
	wallMenu.ShowWhen = "walls"
	wallMenu.Appear, wallMenu.AppearDY = ms(animMid), 12
	pput(wallMenu, 28, 84, 328, 264)
	addP(wallMenu)

	// Скриншот: задержка, зона, спуск. Карточка ниже — контента меньше.
	drawer("shot", "Скриншот", 292)

	shotState := newLayer("shot-state")
	shotState.Text = "{poll:shotsec}  ·  {poll:shotzone}"
	shotState.TextFG, shotState.HasTextFG = pal.onVariant, true
	shotState.FontSize = 11
	shotState.TextPad = 2
	shotState.ShowWhen = "shot"
	shotState.Appear, shotState.AppearDY = ms(animMid), 14
	pput(shotState, 32, 80, 320, 18)
	addP(shotState)

	setSec := func(sec int) string {
		return fmt.Sprintf(`d="${XDG_CACHE_HOME:-$HOME/.cache}/lnf"; mkdir -p "$d"; `+
			`printf '%%d с' %d > "$d/shot.sec"; "$HOME/LNF/scripts/lnf_fifo.sh" set shotsec '%d с'`,
			sec, sec)
	}
	setZone := func(key, label string) string {
		return fmt.Sprintf(`d="${XDG_CACHE_HOME:-$HOME/.cache}/lnf"; mkdir -p "$d"; `+
			`printf '%%s' '%s' > "$d/shot.zone"; "$HOME/LNF/scripts/lnf_fifo.sh" set shotzone '%s'`,
			key, label)
	}
	chip := func(name, label, action string, x, y, w, h float64) {
		l := newLayer(name)
		l.Radius, l.FG = h/2, pal.low
		l.HoverFG, l.HasHover = pal.container, true
		l.Text, l.TextFG, l.HasTextFG = label, pal.onSurface, true
		l.TextHover, l.HasTextHi = pal.primary, true
		l.TextAlign = "center"
		l.FontSize = 12
		l.HoverScale = 1.05
		l.ClickScale = 0.08
		l.ClickExec = action
		l.ShowWhen = "shot"
		l.Appear, l.AppearDY = ms(animMid), 14
		pput(l, x, y, w, h)
		addP(l)
	}

	secs := []struct {
		name, label string
		sec         int
	}{
		{"shot-0", "0 с", 0},
		{"shot-3", "3 с", 3},
		{"shot-5", "5 с", 5},
		{"shot-10", "10 с", 10},
	}
	for i, s := range secs {
		chip(s.name, s.label, setSec(s.sec), 32+float64(i)*82, 108, 74, 42)
	}

	zones := []struct{ name, label, key string }{
		{"zone-full", "Весь экран", "full"},
		{"zone-area", "Область", "zone"},
		{"zone-win", "Окно", "window"},
	}
	for i, z := range zones {
		chip(z.name, z.label, setZone(z.key, z.label), 32+float64(i)*108, 162, 104, 42)
	}

	goShot := newLayer("shot-go")
	goShot.Radius, goShot.FG = 24, pal.primary
	goShot.HoverFG, goShot.HasHover = pal.secondary, true
	goShot.Text, goShot.TextFG, goShot.HasTextFG = "Снять", onPrimary, true
	goShot.TextAlign = "center"
	goShot.FontSize = 14
	goShot.HoverScale = 1.03
	goShot.ClickScale = 0.06
	goShot.ClickExec = "$HOME/LNF/scripts/screenshot_zone.sh"
	goShot.ShowWhen = "shot"
	goShot.Appear, goShot.AppearDY = ms(animMid), 14
	pput(goShot, 32, 220, 320, 48)
	addP(goShot)

	pop.Polls = []*Poll{
		{Name: "shotsec", Interval: time.Hour,
			Command: `cat "${XDG_CACHE_HOME:-$HOME/.cache}/lnf/shot.sec" 2>/dev/null || printf '0 с'`},
		{Name: "shotzone", Interval: time.Hour,
			Command: `z=$(cat "${XDG_CACHE_HOME:-$HOME/.cache}/lnf/shot.zone" 2>/dev/null); ` +
				`case "$z" in zone) printf 'Область';; window) printf 'Окно';; *) printf 'Весь экран';; esac`},
	}

	// ============================================================
	// 3. Приложения — панель у левого края, с поиском
	// ============================================================
	apps := newConfig("apps")
	apps.W, apps.H = 520, 0
	apps.Anchor = anchorLeft | anchorTop | anchorBottom
	apps.Margin = [4]int32{12, 0, 12, 12}
	apps.Layer = "overlay"
	apps.Keyboard = 1
	apps.FontPath = uiFont()
	apps.FontSize = 12
	addA := func(l *Layer) { apps.Layers = append(apps.Layers, l) }

	acard := newLayer("apps-card")
	acard.Radius, acard.FG = 30, pal.surface
	acard.Anchor, acard.HasAnchor = anchorLeft|anchorRight|anchorTop|anchorBottom, true
	acard.ShowWhen = "apps"
	acard.Appear, acard.AppearDX = ms(animMid), -30
	addA(acard)

	atitle := newLayer("apps-title")
	atitle.Text, atitle.TextFG, atitle.HasTextFG = "Приложения", pal.onSurface, true
	atitle.TextPad = 2
	atitle.FontSize = 15
	atitle.ShowWhen = "apps"
	atitle.Appear, atitle.AppearDX = ms(animMid), -24
	pput(atitle, 28, 28, 300, 28)
	addA(atitle)

	aclose := newLayer("apps-close")
	aclose.Radius, aclose.FG = 24, pal.low
	aclose.HoverFG, aclose.HasHover = pal.high, true
	aclose.Text, aclose.TextFG, aclose.HasTextFG = "\uf00d", pal.onVariant, true
	aclose.TextHover, aclose.HasTextHi = pal.onSurface, true
	aclose.TextAlign = "center"
	aclose.FontSize = 18
	aclose.HoverScale = 1.06
	aclose.ClickScale = 0.1
	aclose.Toggle = "apps"
	aclose.ShowWhen = "apps"
	aclose.Appear, aclose.AppearDX = ms(animMid), -24
	pput(aclose, 444, 18, 48, 48)
	addA(aclose)

	apill := newLayer("apps-pill")
	apill.Radius, apill.FG = 24, pal.low
	apill.ShowWhen = "apps"
	apill.Appear, apill.AppearDX = ms(animMid), -24
	pput(apill, 24, 62, 472, 48)
	addA(apill)

	aicon := newLayer("apps-icon")
	aicon.Text, aicon.TextFG, aicon.HasTextFG = "\uf002", pal.onVariant, true
	aicon.TextAlign = "center"
	aicon.FontSize = 16
	aicon.ShowWhen = "apps"
	aicon.Appear, aicon.AppearDX = ms(animMid), -24
	pput(aicon, 40, 62, 24, 48)
	addA(aicon)

	asearch := newLayer("apps-search")
	asearch.Text, asearch.TextFG, asearch.HasTextFG = "{poll:search}", pal.onSurface, true
	asearch.TextPad = 32
	asearch.FontSize = 13
	asearch.Search = true
	asearch.ShowWhen = "apps"
	asearch.Appear, asearch.AppearDX = ms(animMid), -24
	pput(asearch, 48, 62, 440, 48)
	addA(asearch)

	amenu := newLayer("apps-menu")
	amenu.MenuCmd = "$HOME/LNF/scripts/app_launcher.sh"
	amenu.Search = true
	amenu.RowH = 46
	amenu.Pad = 14
	amenu.FontSize = 12
	amenu.FG, amenu.TextFG, amenu.HasTextFG = pal.low, pal.onSurface, true
	amenu.TextHover, amenu.HasTextHi = pal.primary, true
	amenu.HoverShift = [2]float64{4, 0}
	amenu.Anchor, amenu.HasAnchor = anchorLeft|anchorRight|anchorTop|anchorBottom, true
	amenu.Margin = [4]float64{122, 24, 24, 24}
	amenu.ShowWhen = "apps"
	amenu.Appear, amenu.AppearDX = ms(animSlow), -16
	addA(amenu)

	// ============================================================
	// 4. OSD громкости сверху и баннер сообщений слева снизу
	// ============================================================
	osd := newConfig("osd")
	osd.W, osd.H = 300, 52
	osd.Anchor = anchorTop
	osd.Margin = [4]int32{18, 0, 0, 0}
	osd.FontPath = uiFont()
	addO := func(l *Layer) { osd.Layers = append(osd.Layers, l) }

	obg := newLayer("osd-bg")
	obg.Radius, obg.FG = 26, pal.high
	obg.Anchor, obg.HasAnchor = anchorLeft|anchorRight|anchorTop|anchorBottom, true
	obg.Flash, obg.FlashFor = "vol", 1400*time.Millisecond
	obg.Appear, obg.AppearDY = ms(animFast), -16
	addO(obg)

	otx := newLayer("osd-text")
	otx.Text, otx.TextFG, otx.HasTextFG = "{poll:vol}", pal.onSurface, true
	otx.TextAlign = "center"
	otx.Anchor, otx.HasAnchor = anchorLeft|anchorRight|anchorTop|anchorBottom, true
	otx.FontSize = 14
	otx.Flash, otx.FlashFor = "vol", 1400*time.Millisecond
	otx.Appear, otx.AppearDY = ms(animFast), -16
	addO(otx)

	msg := newConfig("msg")
	msg.W, msg.H = 340, 56
	msg.Anchor = anchorBottom | anchorLeft
	msg.Margin = [4]int32{0, 0, 24, 16}
	msg.FontPath = uiFont()
	addM := func(l *Layer) { msg.Layers = append(msg.Layers, l) }

	mbg := newLayer("msg-bg")
	mbg.Radius, mbg.FG = 16, pal.high
	mbg.Anchor, mbg.HasAnchor = anchorLeft|anchorRight|anchorTop|anchorBottom, true
	mbg.Flash, mbg.FlashFor = "notify", 3200*time.Millisecond
	mbg.Appear, mbg.AppearDX = ms(animMid), -20
	addM(mbg)

	macc := newLayer("msg-accent")
	macc.Radius, macc.FG = 2, pal.primary
	macc.Anchor, macc.HasAnchor = anchorLeft|anchorTop|anchorBottom, true
	macc.W = 4
	macc.Margin = [4]float64{12, 0, 12, 12}
	macc.Flash, macc.FlashFor = "notify", 3200*time.Millisecond
	macc.Appear, macc.AppearDX = ms(animMid), -20
	addM(macc)

	mtx := newLayer("msg-text")
	mtx.Text, mtx.TextFG, mtx.HasTextFG = "{poll:notify}", pal.onSurface, true
	mtx.TextPad = 18
	mtx.Anchor, mtx.HasAnchor = anchorLeft|anchorRight|anchorTop|anchorBottom, true
	mtx.Margin = [4]float64{8, 16, 8, 24}
	mtx.FontSize = 10
	mtx.Flash, mtx.FlashFor = "notify", 3200*time.Millisecond
	mtx.Appear, mtx.AppearDX = ms(animMid), -20
	addM(mtx)

	return []*Config{cw, pop, apps, osd, msg}
}
