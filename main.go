// LNF — LoveNatureFrames.
//
// Декларативный фреймворк виджетов для рабочего стола (wlr-layer-shell).
// Описание панели — обычный Go-код (см. panel.go): движок рисует слои
// прямо в разделяемую память компоновщика. Никакого GL, ни cgo: чистый Go.
//
//	./lnf                       — запустить виджет (конфиг в panel.go)
//	./lnf.sh apps|clip|utils|power|notify ТЕКСТ — команда панели (FIFO)
//	LNF_DUMP=out.png ./lnf      — отрендерить сцены в PNG, без компоновщика
//	LNF_TRACE=1 ./lnf           — печатать протокол Wayland
//	LNF_STATS=1 ./lnf           — счётчики: кадры, что анимируется, куча
//	LNF_DEBUG=1 ./lnf           — печатать сводку по сценам
//
// Отладка анимаций без мыши (тоже только для проверки):
//
//	LNF_SPIN=1.5 LNF_DUMP=o.png ./lnf — вращение спустя 1.5 с после клика
//	LNF_SPIN_ONCE=1 ./lnf             — кликнуть по всем click.spin сразу
//	LNF_MOUSE=30,18 ./lnf             — будто курсор стоит в точке x,y
//
//	go test ./...            — проверка, что анимации успокаиваются
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"math"
	"os"
	"os/exec"
	"path"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// ============================================================
// Модель сцены
// ============================================================

type pathCmd struct {
	op  byte // M, L, C, Z
	pts [3][2]float64
}

type Layer struct {
	Name string
	Cmds []pathCmd

	Style   string // fill (обводка пока не сделана)
	StrokeW float64
	Radius  float64 // если задан (и нет Cmds) — рисовать скруглённый прямоугольник, пиксели

	// геометрия: либо x/y/w/h, либо якоря
	X, Y, W, H float64
	HasAnchor  bool
	Anchor     uint32
	Margin     [4]float64 // top, right, bottom, left

	FG       color.NRGBA
	HoverFG  color.NRGBA
	HasHover bool

	// анимации
	HoverScale float64
	HoverShift [2]float64
	HoverRot   float64 // наклон при наведении, градусы
	Rotate     float64 // наклон всегда, градусы
	ClickScale float64
	ClickDecay float64
	Appear     float64 // длительность появления, сек
	AppearDX   float64 // из какого сдвига выезжает при появлении (пиксели)
	AppearDY   float64

	// вращение по клику: через SpinDelay секунд крутится SpinDur секунд
	SpinDur   float64
	SpinDelay float64
	SpinTurns float64

	Follow string // слой, вдоль которого ползём
	Axis   string // x | y
	Pad    float64

	ClickExec  string
	MiddleExec string
	RightExec  string
	WheelUp    string
	WheelDown  string

	// текст
	Text      string // шаблон (раскрывается в shown)
	TextAlign string // left | center | right
	FontSize  float64
	TextPad   float64
	TextFG    color.NRGBA
	TextHover color.NRGBA
	HasTextFG bool
	HasTextHi bool

	// рантайм
	cur        color.NRGBA
	textCur    color.NRGBA
	scale      float64
	hoverScale float64
	shift      [2]float64
	angle      float64     // текущий базовый наклон (rotate + hover.rotate)
	why        string      // что сейчас анимируется (для LNF_STATS)
	spinT      float64     // сколько уже крутится
	spinOn     bool        // крутится ли сейчас
	tmp        *image.RGBA // временный буфер для повёрнутого текста
	center     float64
	centerInit bool
	hovered    bool
	pressed    bool
	appearT    float64
	shown      string

	// раскрывающиеся группы и меню-список
	ShowWhen string        // слой виден, только когда включена группа с этим именем
	Toggle   string        // по клику переключаем группу с этим именем
	Flash    string        // всплывающий OSD: показываем на FlashFor после изменения опроса
	FlashFor time.Duration // сколько держать
	MenuCmd  string        // вывод команды — пункты: «подпись<TAB>команда<TAB>значок»
	Search   bool          // список фильтруется строкой поиска сцены
	RowH     float64       // высота пункта меню
	DX, DY   float64       // смещение поверх якорей
	scroll   int           // прокрутка меню (рантайм)

	// сетка-плитки (лаунчер приложений): значок сверху, подпись снизу
	Grid      bool
	Cols      int     // максимум столбцов (0 — по ширине CellW)
	CellW     float64 // ширина ячейки; 0 — растянуть Cols на всю ширину
	CellH     float64 // высота ячейки (по умолчанию 120)
	IconSize  float64 // сторона значка (по умолчанию 64)
	TopPad    float64 // отступ значка сверху ячейки (по умолчанию 16)
	EmptyText string  // что показать, когда список пуст (сетка)

	// индикатор воркспейсов: точки, активная растягивается в «капсулу».
	// DotsPoll — имя опроса, который печатает "N A" (всего / активный).
	Dots       bool
	DotsPoll   string
	DotSize    float64 // диаметр обычной точки (по умолчанию 7)
	DotGap     float64 // зазор между точками (по умолчанию 9)
	DotActiveW float64 // ширина активной точки (по умолчанию 24)
	DotFG      color.NRGBA
	DotActive  color.NRGBA

	// числовой индикатор воркспейсов: плитки с номерами вместо точек
	DotNums        bool
	DotNumFG       color.NRGBA
	DotNumActiveFG color.NRGBA

	dotsInit   bool
	dotsVal    string
	dotsActive int
	dotsCur    []float64
	dotsTarget []float64
}

// menuItem — одна строка меню-списка.
// alt — «вторичное» действие (ПКМ по строке): например, удалить запись.
// col — разобранный цвет один раз при parseMenu: раньше он снова
// прогонялся через parseColor на каждом пункте в каждом кадре.
type menuItem struct {
	label, cmd, icon, color, alt string
	col                          color.NRGBA
}

// menuStore — разобранные пункты по слою. Пишем из горутины, читаем в кадре,
// поэтому храним иммутабельный срез в sync.Map.
var menuStore sync.Map // *Layer -> []menuItem

// iconCache — уменьшенные иконки приложений: путь+размер -> *image.RGBA
// (nil означает «не смогли прочитать, больше не пытаться»).
var iconCache sync.Map

func parseMenu(out string) []menuItem {
	var items []menuItem
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\t")
		it := menuItem{label: f[0]}
		if len(f) > 1 {
			it.cmd = strings.TrimSpace(f[1])
		}
		if len(f) > 2 {
			it.icon = strings.TrimSpace(f[2])
		}
		if len(f) > 3 {
			it.color = strings.TrimSpace(f[3])
		}
		if len(f) > 4 {
			it.alt = strings.TrimSpace(f[4])
		}
		it.col, _ = parseColor(it.color)
		if it.label == "" {
			continue
		}
		items = append(items, it)
	}
	return items
}

type Poll struct {
	Name     string
	Command  string
	Interval time.Duration
}

type Config struct {
	Title string

	W, H float64 // 0 = пусть компоновщик решает (растянуть по якорям)
	BG   color.NRGBA

	Layer     string // background | bottom | top | overlay
	Anchor    uint32
	Margin    [4]int32 // top, right, bottom, left
	Namespace string
	Exclusive int32

	FontPath string
	FontSize float64

	Keyboard int // >0 — поверхность берёт клавиатуру, пока открыт поиск

	GroupsOn []string // группы, раскрытые при старте (обычно пусто)

	Polls  []*Poll
	Layers []*Layer
}

// ============================================================
// Слои и сцены
// ============================================================

// newLayer — слой со значениями по умолчанию (панель их переопределяет).
func newLayer(name string) *Layer {
	return &Layer{
		Name: name, Style: "fill", StrokeW: 1,
		FG: color.NRGBA{255, 255, 255, 255}, TextAlign: "left",
		scale: 1, hoverScale: 1, appearT: 1, SpinTurns: 6,
	}
}

const (
	anchorTop    uint32 = 1
	anchorBottom uint32 = 2
	anchorLeft   uint32 = 4
	anchorRight  uint32 = 8
)

func parseColor(s string) (color.NRGBA, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "none" || s == "transparent" {
		return color.NRGBA{}, nil
	}
	if !strings.HasPrefix(s, "#") {
		return color.NRGBA{}, fmt.Errorf("цвет не в формате #rgb: %q", s)
	}
	h := s[1:]
	if len(h) == 3 || len(h) == 4 {
		e := make([]byte, 0, 8)
		for i := 0; i < len(h); i++ {
			e = append(e, h[i], h[i])
		}
		h = string(e)
	}
	if len(h) != 6 && len(h) != 8 {
		return color.NRGBA{}, fmt.Errorf("цвет не в формате #rrggbb: %q", s)
	}
	v, err := strconv.ParseUint(h, 16, 64)
	if err != nil {
		return color.NRGBA{}, fmt.Errorf("цвет не в формате #rrggbb: %q", s)
	}
	if len(h) == 6 {
		return color.NRGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}, nil
	}
	return color.NRGBA{uint8(v >> 24), uint8(v >> 16), uint8(v >> 8), uint8(v)}, nil
}

// --- SVG-путь: M / L / C / Z, координаты в нормированном виде [-1..1] ---

func parsePath(s string) ([]pathCmd, error) {
	toks := tokenizePath(s)
	var cmds []pathCmd
	i := 0
	next := func() (float64, float64, error) {
		if i+1 >= len(toks) {
			return 0, 0, fmt.Errorf("путь оборван")
		}
		x, err1 := strconv.ParseFloat(toks[i], 64)
		y, err2 := strconv.ParseFloat(toks[i+1], 64)
		if err1 != nil || err2 != nil {
			return 0, 0, fmt.Errorf("в пути плохое число: %q %q", toks[i], toks[i+1])
		}
		i += 2
		return x, y, nil
	}
	for i < len(toks) {
		c := toks[i]
		i++
		switch c {
		case "M", "L":
			x, y, err := next()
			if err != nil {
				return nil, err
			}
			cmds = append(cmds, pathCmd{op: c[0], pts: [3][2]float64{{x, y}}})
		case "C":
			var p [3][2]float64
			for k := 0; k < 3; k++ {
				x, y, err := next()
				if err != nil {
					return nil, err
				}
				p[k] = [2]float64{x, y}
			}
			cmds = append(cmds, pathCmd{op: 'C', pts: p})
		case "Z", "z":
			cmds = append(cmds, pathCmd{op: 'Z'})
		default:
			return nil, fmt.Errorf("в пути неизвестная команда %q", c)
		}
	}
	if len(cmds) == 0 {
		return nil, fmt.Errorf("пустой shape")
	}
	return cmds, nil
}

func tokenizePath(s string) []string {
	var toks []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			toks = append(toks, b.String())
			b.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ' ' || c == ',' || c == '\t' || c == '\n' || c == '\r':
			flush()
		case (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z'):
			flush()
			toks = append(toks, string(c))
		case c == '-' || c == '+':
			flush()
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	flush()
	return toks
}

// ============================================================
// Сцена: раскладка, растеризация, текст, взаимодействие
// ============================================================

type Scene struct {
	cfg    *Config
	byName map[string]*Layer

	W, H float64 // размер поверхности (приходит от компоновщика)

	MouseX, MouseY float64
	InSurface      bool

	raster *vector.Rasterizer
	last   time.Time

	wantTime bool

	spinMu sync.Mutex
	spinQ  []*Layer // сюда горутина откладывает «пора крутиться»

	wheelAt time.Time
	pressed *Layer

	// OnChange дёргается, когда снаружи (опросы, меню) пришли новые данные
	OnChange func()
	// OnGroup — клик по кнопке просит переключить группу во всех сценах,
	// где она есть (в отличие от локального toggle): ящик может лежать на
	// другой поверхности, и кнопка должна открыть его именно там.
	OnGroup func(name string, mode int)
	// OnWake просто будит цикл, не требуя перерисовки: часы тикают раз в
	// секунду, а текст меняется раз в минуту — лишние кадры ни к чему.
	OnWake func()

	// groups — раскрывашки: слой с show=имя виден, только когда groups[имя]
	groups map[string]bool

	query string // строка поиска (для слоёв с Search)

	// pollDeps — имена опросов, от которых зависит сцена: текст-шаблоны,
	// точки воркспейсов, всплывающие OSD. По ним адресно будим нужные
	// поверхности, не перерисовывая весь «жирный» конфиг.
	pollDeps map[string]bool

	// textGen/textMin — последнее состояние, для которого раскрывали
	// шаблоны. RefreshText вызывается на каждое событие ввода, но строка
	// меняется только при смене опроса или минуты.
	textGen uint64
	textMin int64

	rectBuf [][4]int // переиспользуемый буфер InputRects
}

func NewScene(cfg *Config) *Scene {
	s := &Scene{
		cfg: cfg, byName: map[string]*Layer{},
		W: cfg.W, H: cfg.H, groups: map[string]bool{},
		pollDeps: map[string]bool{},
	}
	if s.W <= 0 {
		s.W = 1280
	}
	if s.H <= 0 {
		s.H = 40
	}
	s.raster = vector.NewRasterizer(1, 1)
	for _, g := range cfg.GroupsOn {
		s.groups[g] = true
	}
	for _, l := range cfg.Layers {
		l.cur = l.FG
		if l.HasTextFG {
			l.textCur = l.TextFG
		} else {
			l.textCur = l.FG
		}
		s.byName[l.Name] = l
		l.shown = s.expand(l.Text)
		if !s.shown(l) {
			l.appearT = 0 // скрытое не мелькает на старте
		}
		if strings.Contains(l.Text, "{time:") {
			s.wantTime = true
		}
		for _, p := range templatePolls(l.Text) {
			s.pollDeps[p] = true
		}
		if l.DotsPoll != "" {
			s.pollDeps[l.DotsPoll] = true
		}
		if l.Flash != "" {
			s.pollDeps[l.Flash] = true
		}
	}
	s.textGen = pollGen.Load()
	s.textMin = time.Now().Unix() / 60
	return s
}

func (s *Scene) Resize(w, h float64) {
	if w > 0 {
		s.W = w
	}
	if h > 0 {
		s.H = h
	}
}

// --- раскладка ------------------------------------------------------

// layout считает прямоугольник слоя: по якорям либо по абсолютным x/y.
func (s *Scene) layout(l *Layer) (x, y, w, h float64) {
	w, h = l.W, l.H
	if !l.HasAnchor {
		return l.X + l.DX, l.Y + l.DY, w, h
	}
	switch {
	case l.Anchor&anchorLeft != 0 && l.Anchor&anchorRight != 0:
		x = l.Margin[3]
		w = s.W - l.Margin[1] - l.Margin[3]
	case l.Anchor&anchorLeft != 0:
		x = l.Margin[3]
	case l.Anchor&anchorRight != 0:
		x = s.W - w - l.Margin[1]
	default:
		x = (s.W - w) / 2
	}
	switch {
	case l.Anchor&anchorTop != 0 && l.Anchor&anchorBottom != 0:
		y = l.Margin[0]
		h = s.H - l.Margin[0] - l.Margin[2]
	case l.Anchor&anchorTop != 0:
		y = l.Margin[0]
	case l.Anchor&anchorBottom != 0:
		y = s.H - h - l.Margin[2]
	default:
		y = (s.H - h) / 2
	}
	x += l.DX
	y += l.DY
	if l.AppearDX != 0 || l.AppearDY != 0 {
		k := 1 - ease(l.appearT) // во время появления слой ползёт из сдвига в 0
		x += l.AppearDX * k
		y += l.AppearDY * k
	}
	return
}

// --- опросы (логика) -------------------------------------------------

// Значения опросов общие на весь процесс: команда одна, а читать её
// может сколько угодно поверхностей. Одна и та же команда не запускается
// дважды, даже если её объявили в двух секциях.
var (
	pollValues  sync.Map // имя -> последний вывод
	pollStarted sync.Map // имя -> уже запущен
	pollAt      sync.Map // имя -> int64 (UnixNano последнего изменения)
	pollGen     atomic.Uint64
	clockOnce   sync.Once

	scenesMu     sync.Mutex
	activeScenes []*Scene // все сцены процесса: адресные будильники опросов
)

// wake будит цикл поверхности, если ей нужна перерисовка.
func (s *Scene) wake() {
	if s.OnChange != nil {
		s.OnChange()
	} else if s.OnWake != nil {
		s.OnWake()
	}
}

// templatePolls достаёт имена опросов из шаблона «{poll:name}» / «{name}».
func templatePolls(tpl string) []string {
	var out []string
	for {
		i := strings.IndexByte(tpl, '{')
		if i < 0 {
			return out
		}
		j := strings.IndexByte(tpl[i:], '}')
		if j < 0 {
			return out
		}
		key := tpl[i+1 : i+j]
		tpl = tpl[i+j+1:]
		if strings.HasPrefix(key, "time:") {
			continue
		}
		key = strings.TrimPrefix(key, "poll:")
		if key != "" {
			out = append(out, key)
		}
	}
}

// broadcastPoll будит только те сцены, которые действительно зависят от
// изменившегося опроса. Один опрос на весь процесс — одна команда, но
// перерисовываются лишь его потребители.
func broadcastPoll(name string) {
	scenesMu.Lock()
	list := append([]*Scene(nil), activeScenes...)
	scenesMu.Unlock()
	for _, s := range list {
		if s.pollDeps[name] {
			s.wake()
		}
	}
}

// anyFlashes — есть ли в процессе слой, всплывающий по этому опросу.
func anyFlashes(name string) bool {
	scenesMu.Lock()
	defer scenesMu.Unlock()
	for _, s := range activeScenes {
		for _, l := range s.cfg.Layers {
			if l.Flash == name {
				return true
			}
		}
	}
	return false
}

// holdPollWake будит зависящие сцены, когда окно показа OSD истекает.
func holdPollWake(name string) {
	d := time.Duration(0)
	scenesMu.Lock()
	for _, s := range activeScenes {
		for _, l := range s.cfg.Layers {
			if l.Flash == name && l.FlashFor > d {
				d = l.FlashFor
			}
		}
	}
	scenesMu.Unlock()
	if d <= 0 {
		return
	}
	time.AfterFunc(d+50*time.Millisecond, func() { broadcastPoll(name) })
}

func (s *Scene) StartLogic() {
	scenesMu.Lock()
	activeScenes = append(activeScenes, s)
	scenesMu.Unlock()
	go func() { // список меню читаем фоном, чтобы не тормозить первый кадр
		s.loadMenus()
		if s.OnChange != nil {
			s.OnChange()
		}
	}()
	// часы: {time:%H:%M} меняется раз в минуту, поэтому и будим цикл раз в
	// минуту — ровно к смене. Секундный тик впустую жёг процессор.
	// Будим все сцены с часами, а не только первую (иначе на «жирном»
	// конфиге часы замирали на остальных поверхностях).
	if s.wantTime {
		clockOnce.Do(func() {
			go func() {
				for {
					next := time.Now().Truncate(time.Minute).Add(time.Minute)
					time.Sleep(time.Until(next) + 30*time.Millisecond)
					scenesMu.Lock()
					list := append([]*Scene(nil), activeScenes...)
					scenesMu.Unlock()
					for _, sc := range list {
						if sc.wantTime {
							sc.wake()
						}
					}
				}
			}()
		})
	}
	for _, p := range s.cfg.Polls {
		if _, dup := pollStarted.LoadOrStore(p.Name, true); dup {
			continue
		}
		go func(p *Poll) {
			seen := false // первый замер не считаем сменой (иначе OSD на старте)
			for {
				if v, err := runCmd(p.Command); err == nil || v != "" {
					if s.setPoll(p.Name, v) {
						if seen && anyFlashes(p.Name) {
							s.markFlash(p.Name)
							holdPollWake(p.Name)
						}
						broadcastPoll(p.Name) // только сцены-потребители
					}
					seen = true
				}
				time.Sleep(p.Interval)
			}
		}(p)
	}
}

// PollOnce выполняет все опросы один раз (для LNF_DUMP).
func (s *Scene) PollOnce() {
	for _, p := range s.cfg.Polls {
		if v, err := runCmd(p.Command); err == nil {
			if s.setPoll(p.Name, v) && s.flashesOn(p.Name) {
				s.markFlash(p.Name) // в дампе показываем OSD «как после смены»
			}
		}
	}
	s.loadMenus()
}

func runCmd(cmd string) (string, error) {
	out, err := exec.Command("sh", "-c", cmd).Output()
	return strings.TrimSpace(string(out)), err
}

// setPoll сохраняет значение опроса и говорит, изменилось ли оно
// (чтобы не перерисовывать панель на каждый такт опроса).
func (s *Scene) setPoll(name, val string) bool {
	if old, ok := pollValues.Load(name); ok && old.(string) == val {
		return false
	}
	pollValues.Store(name, val)
	pollGen.Add(1)
	return true
}

// markFlash отмечает момент изменения опроса — по нему всплывают OSD.
func (s *Scene) markFlash(pollName string) {
	pollAt.Store(pollName, time.Now().UnixNano())
}

// flashesOn — есть ли слой, всплывающий по этому опросу.
func (s *Scene) flashesOn(pollName string) bool {
	for _, l := range s.cfg.Layers {
		if l.Flash == pollName {
			return true
		}
	}
	return false
}

func (s *Scene) getPoll(name string) string {
	if v, ok := pollValues.Load(name); ok {
		return v.(string)
	}
	return ""
}

// RefreshText раскрывает шаблоны и говорит, изменилось ли что-нибудь.
// Быстрый выход: если ни один опрос не менялся и минута та же, раскрывать
// нечего — не тратим CPU на каждом событии мыши.
func (s *Scene) RefreshText() bool {
	gen := pollGen.Load()
	min := int64(0)
	if s.wantTime {
		min = time.Now().Unix() / 60
	}
	if gen == s.textGen && min == s.textMin {
		return false
	}
	s.textGen, s.textMin = gen, min
	changed := false
	for _, l := range s.cfg.Layers {
		if l.Text == "" {
			continue
		}
		if v := s.expand(l.Text); v != l.shown {
			l.shown, changed = v, true
		}
	}
	return changed
}

func (s *Scene) expand(tpl string) string {
	if !strings.ContainsRune(tpl, '{') {
		return tpl
	}
	var b strings.Builder
	for {
		i := strings.IndexByte(tpl, '{')
		if i < 0 {
			b.WriteString(tpl)
			return b.String()
		}
		j := strings.IndexByte(tpl[i:], '}')
		if j < 0 {
			b.WriteString(tpl)
			return b.String()
		}
		b.WriteString(tpl[:i])
		key := tpl[i+1 : i+j]
		tpl = tpl[i+j+1:]
		switch {
		case strings.HasPrefix(key, "time:"):
			b.WriteString(time.Now().Format(strftimeToGo(key[5:])))
		case strings.HasPrefix(key, "poll:"):
			b.WriteString(s.getPoll(key[5:]))
		default:
			b.WriteString(s.getPoll(key))
		}
	}
}

var strftimeMap = map[byte]string{
	'H': "15", 'I': "03", 'M': "04", 'S': "05", 'p': "PM",
	'd': "02", 'e': "_2", 'm': "01", 'b': "Jan", 'B': "January",
	'a': "Mon", 'A': "Monday", 'Y': "2006", 'y': "06", 'j': "002",
	'Z': "MST", '%': "%",
}

func strftimeToGo(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+1 < len(s) {
			if g, ok := strftimeMap[s[i+1]]; ok {
				b.WriteString(g)
				i++
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// --- взаимодействие --------------------------------------------------

// visible — показан ли слой прямо сейчас (с учётом раскрывашек)
// shown — выполнено ли условие видимости слоя: либо включена его группа,
// либо недавно менялся его опрос-вспышка (OSD).
func (s *Scene) shown(l *Layer) bool {
	switch {
	case l.ShowWhen != "":
		return s.groups[l.ShowWhen]
	case l.Flash != "":
		v, ok := pollAt.Load(l.Flash)
		return ok && time.Since(time.Unix(0, v.(int64))) < l.FlashFor
	}
	return true
}

func (s *Scene) visible(l *Layer) bool {
	return s.shown(l) || l.appearT > 0 // невидимый доигрывает анимацию закрытия
}

func (l *Layer) rowH() float64 {
	if l.RowH > 0 {
		return l.RowH
	}
	return 40
}

func (s *Scene) menuItems(l *Layer) []menuItem {
	v, ok := menuStore.Load(l)
	if !ok {
		return nil
	}
	items := v.([]menuItem)
	if l.Search && s.query != "" {
		return filterItems(items, s.query)
	}
	return items
}

// filterItems — пункты, в подписи которых встречается строка (без учёта регистра)
func filterItems(items []menuItem, q string) []menuItem {
	q = strings.ToLower(q)
	out := make([]menuItem, 0, len(items))
	for _, it := range items {
		if strings.Contains(strings.ToLower(it.label), q) {
			out = append(out, it)
		}
	}
	return out
}

// menuIndexAt — индекс пункта под точкой (или -1). Понимает и список, и сетку.
func (s *Scene) menuIndexAt(l *Layer, mx, my float64) int {
	items := s.menuItems(l)
	if len(items) == 0 {
		return -1
	}
	x, y, w, h := s.layerRect(l)
	if mx < x || mx >= x+w || my < y || my >= y+h {
		return -1
	}
	var idx int
	if l.Grid {
		cols, cellW, cellH, ox, oy := s.gridGeom(l)
		if mx < ox || mx >= ox+float64(cols)*cellW || my < oy {
			return -1
		}
		c := int((mx - ox) / cellW)
		r := l.scroll + int((my-oy)/cellH)
		if c < 0 || c >= cols || r < 0 {
			return -1
		}
		idx = r*cols + c
	} else {
		idx = l.scroll + int((my-y)/l.rowH())
	}
	if idx < 0 || idx >= len(items) {
		return -1
	}
	return idx
}

func (s *Scene) menuRows(l *Layer) int {
	_, _, _, h := s.layerRect(l)
	return int(h / l.rowH())
}

// gridGeom — раскладка сетки: сколько столбцов, размер ячейки, откуда
// начинается блок (по горизонтали центрируем на экране).
func (s *Scene) gridGeom(l *Layer) (cols int, cellW, cellH, ox, oy float64) {
	x, y, w, h := s.layerRect(l)
	cols = l.Cols
	if l.CellW > 0 {
		cols = int(w / l.CellW)
		if cols < 1 {
			cols = 1
		}
		if l.Cols > 0 && cols > l.Cols {
			cols = l.Cols
		}
	}
	if cols <= 0 {
		cols = 6
	}
	cellW = l.CellW
	if cellW <= 0 {
		cellW = w / float64(cols)
	}
	cellH = l.CellH
	if cellH <= 0 {
		cellH = 120
	}
	ox = x + (w-float64(cols)*cellW)/2
	oy = y
	_ = h
	return
}

// menuMaxScroll — на сколько строк можно прокрутить список/сетку.
func (s *Scene) menuMaxScroll(l *Layer) int {
	items := s.menuItems(l)
	if len(items) == 0 {
		return 0
	}
	if l.Grid {
		cols, _, cellH, _, _ := s.gridGeom(l)
		_, _, _, h := s.layerRect(l)
		totalRows := (len(items) + cols - 1) / cols
		vis := int(h / cellH)
		if vis < 1 {
			vis = 1
		}
		if n := totalRows - vis; n > 0 {
			return n
		}
		return 0
	}
	if n := len(items) - s.menuRows(l); n > 0 {
		return n
	}
	return 0
}

// loadMenus — один раз выполняем команды меню и раскладываем пункты
func (s *Scene) loadMenus() {
	for _, l := range s.cfg.Layers {
		if l.MenuCmd != "" {
			s.reloadMenu(l)
		}
	}
}

// reloadMenusFor перечитывает меню только той раскрывашки, которую открываем:
// открытие лаунчера не должно запускать clip.sh/utils.sh (лишние процессы).
func (s *Scene) reloadMenusFor(group string) {
	for _, l := range s.cfg.Layers {
		if l.MenuCmd != "" && (l.ShowWhen == "" || l.ShowWhen == group) {
			s.reloadMenu(l)
		}
	}
}

// reloadMenu перечитывает список одного слоя-меню (например, после удаления
// записи буфера обмена — ящик при этом остаётся открытым).
func (s *Scene) reloadMenu(l *Layer) {
	if l.MenuCmd == "" {
		return
	}
	out, err := runCmd(l.MenuCmd)
	if err != nil && out == "" {
		return
	}
	items := parseMenu(out)
	menuStore.Store(l, items)
	// Иконки декодируем заранее, в фоне: иначе первый кадр открытого
	// лаунчера/ящика тратил время на PNG всех видимых пунктов.
	go prefetchIcons(l, items)
}

// iconPx — размер иконки, который возьмёт отрисовка этого слоя.
func (l *Layer) iconPx() int {
	if l.Grid {
		s := l.IconSize
		if s <= 0 {
			s = 64
		}
		return int(s)
	}
	chip := l.rowH() - 12
	if chip > 38 {
		chip = 38
	}
	if chip < 1 {
		chip = 1
	}
	return int(chip)
}

func prefetchIcons(l *Layer, items []menuItem) {
	size := l.iconPx()
	for _, it := range items {
		if strings.HasPrefix(it.icon, "/") {
			menuIcon(it.icon, size)
		}
	}
}

func (s *Scene) clickable(l *Layer) bool {
	return l.ClickScale > 0 || l.ClickExec != "" || l.RightExec != "" || l.MiddleExec != "" ||
		l.WheelUp != "" || l.WheelDown != "" || l.SpinDur > 0 || l.Toggle != "" || l.MenuCmd != "" ||
		l.Search || l.HasHover || l.HoverScale != 1 || l.HoverShift != [2]float64{} || l.HoverRot != 0
}

// hit — верхний кликабельный слой под курсором
func (s *Scene) hit() *Layer {
	var found *Layer
	for _, l := range s.cfg.Layers {
		if !s.clickable(l) || !s.visible(l) {
			continue
		}
		x, y, w, h := s.layout(l)
		x -= l.shift[0]
		y -= l.shift[1]
		if s.MouseX >= x && s.MouseX < x+w && s.MouseY >= y && s.MouseY < y+h {
			found = l
		}
	}
	return found
}

func (s *Scene) Move(x, y float64) {
	s.MouseX, s.MouseY, s.InSurface = x, y, true
}

func (s *Scene) Leave() {
	s.InSurface = false
	if s.pressed != nil {
		s.pressed.pressed = false
		s.pressed = nil
	}
}

// коды кнопок wl_pointer (BTN_*)
const (
	btnLeft   = 0x110
	btnMiddle = 0x111
	btnRight  = 0x112
)

func (s *Scene) Button(code uint32, pressed bool) {
	if pressed {
		if l := s.hit(); l != nil {
			l.pressed = true
			s.pressed = l
		}
		return
	}
	l := s.pressed
	if l == nil {
		return
	}
	l.pressed, s.pressed = false, nil
	if !l.hovered {
		return
	}
	idx := -1
	if l.MenuCmd != "" {
		idx = s.menuIndexAt(l, s.MouseX, s.MouseY)
	}
	var cmd string
	switch code {
	case btnLeft:
		cmd = l.ClickExec
		s.armSpin(l)
		if l.MenuCmd != "" {
			cmd = ""
			if idx >= 0 {
				cmd = s.menuItems(l)[idx].cmd
			}
		}
		if l.Toggle != "" {
			if s.OnGroup != nil {
				s.OnGroup(l.Toggle, 0) // переключить группу во всех сценах
			} else {
				s.toggle(l.Toggle)
			}
		}
	case btnMiddle:
		cmd = l.MiddleExec
	case btnRight:
		// ПКМ по строке меню — её «вторичное» действие (например, удалить
		// запись буфера). Список обновляем, ящик оставляем открытым.
		if idx >= 0 {
			if alt := s.menuItems(l)[idx].alt; alt != "" {
				// Действие и перечитывание списка — в фоне, чтобы
				// удаление записи буфера не подвешивало отрисовку.
				go func() {
					if err := exec.Command("sh", "-c", alt).Run(); err != nil {
						fmt.Fprintf(os.Stderr, "LNF: %q: %v\n", alt, err)
					}
					s.reloadMenu(l)
					s.wake()
				}()
				return
			}
		}
		cmd = l.RightExec
	}
	if cmd != "" {
		s.Exec(cmd)
	}
	// выбрали пункт меню левым кликом — закрываем ящик, из которого он раскрыт
	if code == btnLeft && l.MenuCmd != "" && l.ShowWhen != "" && s.groups[l.ShowWhen] {
		s.toggle(l.ShowWhen)
	}
}

// toggle переключает группу-раскрывашку и будит анимацию появления
func (s *Scene) toggle(group string) {
	s.applyGroup(group, !s.groups[group])
}

// setGroup — внешнее управление раскрывашкой (клавиши/FIFO):
// mode 0 — переключить, 1 — открыть, 2 — закрыть.
func (s *Scene) setGroup(group string, mode int) {
	on := mode == 1
	if mode == 0 {
		on = !s.groups[group]
	}
	s.applyGroup(group, on)
}

// applyGroup открывает/закрывает раскрывашку: одна за раз, с анимацией
func (s *Scene) applyGroup(group string, on bool) {
	if on == s.groups[group] {
		return
	}
	// открываем одну раскрывашку за раз: открытие закрывает прочие
	if on {
		for g := range s.groups {
			if g != group {
				s.groups[g] = false
			}
		}
	}
	s.groups[group] = on
	hasSearch := false
	for _, l := range s.cfg.Layers {
		// при открытии слой стартует с нуля; при закрытии appearT
		// должен остаться, иначе анимация закрытия обрывается
		if on && l.ShowWhen == group && l.Appear > 0 {
			l.appearT = 0
		}
		if l.ShowWhen == group {
			l.hovered, l.pressed = false, false
			l.scroll = 0
		}
		if l.Search && l.ShowWhen == group {
			hasSearch = true
		}
	}
	if hasSearch {
		s.setQuery("") // открыли — показать подсказку, закрыли — очистить
	}
	// при раскрытии перечитываем меню — чтобы буфер обмена/список были свежими
	if on {
		go func() {
			s.reloadMenusFor(group)
			if s.OnChange != nil {
				s.OnChange()
			}
		}()
	}
	if s.OnChange != nil {
		s.OnChange()
	}
	// лаунчер закрыли — вернуть ядру память под кэш иконок, буферы и растр
	if !on && group == "apps" && hasSearch {
		s.raster = vector.NewRasterizer(1, 1)
		time.AfterFunc(600*time.Millisecond, debug.FreeOSMemory)
	}
}

// --- поиск по списку (клавиатура) ---

func (s *Scene) searchOpen() bool { return s.groups["apps"] }

// setQuery меняет строку поиска; её показывает слой с {poll:search}
func (s *Scene) setQuery(q string) {
	s.query = q
	// Поле без текста: значок поиска рисует отдельный слой панели,
	// здесь — только набранное и курсор.
	disp := ""
	if q != "" {
		disp = q + string(rune(0x258f)) // курсор
	}
	s.setPoll("search", disp)
	for _, l := range s.cfg.Layers {
		if l.Search {
			l.scroll = 0
		}
	}
	if s.OnChange != nil {
		s.OnChange()
	}
}

// typeText дописывает набранный текст (через text-input — любая раскладка)
func (s *Scene) typeText(t string) {
	if s.searchOpen() {
		s.setQuery(s.query + t)
	}
}

func (s *Scene) backspace() {
	if !s.searchOpen() || s.query == "" {
		return
	}
	r := []rune(s.query)
	s.setQuery(string(r[:len(r)-1]))
}

// activateFirst запускает первый подходящий пункт (Enter)
func (s *Scene) activateFirst() {
	if !s.searchOpen() {
		return
	}
	for _, l := range s.cfg.Layers {
		if l.Search {
			if items := s.menuItems(l); len(items) > 0 {
				s.Exec(items[0].cmd)
			}
			s.toggle("apps")
			return
		}
	}
}

func (s *Scene) closeSearch() {
	if s.searchOpen() {
		s.toggle("apps")
	}
}

// Wheel — прокрутка колеса над слоем; вверх = отрицательное значение
func (s *Scene) Wheel(v float64) {
	if time.Since(s.wheelAt) < 120*time.Millisecond {
		return
	}
	l := s.hit()
	if l == nil {
		return
	}
	if l.MenuCmd != "" {
		if v < 0 {
			l.scroll--
		} else {
			l.scroll++
		}
		l.scroll = int(clamp(float64(l.scroll), 0, float64(s.menuMaxScroll(l))))
		return
	}
	cmd := l.WheelDown
	if v < 0 {
		cmd = l.WheelUp
	}
	if cmd == "" {
		return
	}
	s.wheelAt = time.Now()
	s.Exec(cmd)
}

// armSpin заводит вращение по клику. Если задана задержка, ждём её
// отдельной горутиной и только потом будим цикл — до тех пор виджет спит.
func (s *Scene) armSpin(l *Layer) {
	if l.SpinDur <= 0 {
		return
	}
	if l.SpinDelay <= 0 {
		l.spinOn, l.spinT = true, 0
		return
	}
	go func() {
		time.Sleep(time.Duration(l.SpinDelay * float64(time.Second)))
		s.spinMu.Lock()
		s.spinQ = append(s.spinQ, l)
		s.spinMu.Unlock()
		if s.OnChange != nil {
			s.OnChange()
		}
	}()
}

// spinAngle — угол вращения от клика, градусы
func (l *Layer) spinAngle() float64 {
	if l.SpinDur <= 0 || l.spinT <= 0 {
		return 0
	}
	t := l.spinT / l.SpinDur
	if t > 1 {
		t = 1
	}
	return 360 * l.SpinTurns * easeInOut(t)
}

// angleNow — полный наклон слоя: базовый + hover + вращение
func (l *Layer) angleNow() float64 { return l.angle + l.spinAngle() }

func (s *Scene) Exec(cmd string) {
	go func() {
		if out, err := exec.Command("sh", "-c", cmd).CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "LNF: %q: %v (%s)\n", cmd, err, strings.TrimSpace(string(out)))
		}
	}()
}

// --- анимации --------------------------------------------------------

func (s *Scene) Update(now time.Time) bool {
	dt := 0.0
	if !s.last.IsZero() {
		dt = now.Sub(s.last).Seconds()
	}
	s.last = now
	if dt < 0 {
		dt = 0
	}
	if dt > 0.1 {
		dt = 0.1
	}

	anim := false

	// горутины отложенного вращения: переносим заявки в сам цикл
	s.spinMu.Lock()
	for _, l := range s.spinQ {
		l.spinOn, l.spinT = true, 0
	}
	s.spinQ = s.spinQ[:0]
	s.spinMu.Unlock()

	for _, l := range s.cfg.Layers {
		if s.updateLayer(l, dt) {
			anim = true
		}
	}
	return anim
}

func (s *Scene) updateLayer(l *Layer, dt float64) bool {
	if !s.visible(l) {
		return false
	}
	l.why = ""
	anim := false

	// индикатор воркспейсов: ширина точек едет пружиной к цели
	if l.Dots && s.updateDots(l, dt) {
		anim, l.why = true, "dots"
	}

	// появление/закрытие: едем к цели (1 — открыто, 0 — закрыто)
	target := 1.0
	if !s.shown(l) {
		target = 0
	}
	switch {
	case l.Appear > 0 && l.appearT < target:
		l.appearT = math.Min(target, l.appearT+dt/l.Appear)
		anim, l.why = true, "appear"
	case l.Appear > 0 && l.appearT > target:
		l.appearT = math.Max(target, l.appearT-dt/l.Appear)
		anim, l.why = true, "close"
	case l.Appear <= 0 && l.appearT != target:
		l.appearT = target
		anim, l.why = true, "appear"
	}

	// ползём вдоль другого слоя
	if l.Follow != "" {
		if t := s.byName[l.Follow]; t != nil {
			tx, ty, tw, th := s.layout(t)
			lo, hi := tx+l.Pad, tx+tw-l.Pad
			want := s.MouseX
			if l.Axis == "y" {
				lo, hi = ty+l.Pad, ty+th-l.Pad
				want = s.MouseY
			}
			if !l.centerInit {
				l.center, l.centerInit = (lo+hi)/2, true
			}
			want = clamp(want, lo, hi)
			l.center += (want - l.center) * smear(18, dt)
			if math.Abs(want-l.center) > 0.15 {
				anim, l.why = true, "follow"
			} else {
				l.center = want
			}
			if l.Axis == "y" {
				l.shift[1] += l.center - (l.Y + l.H/2)
			} else {
				l.shift[0] += l.center - (l.X + l.W/2)
			}
		}
	}

	x, y, w, h := s.layout(l)
	x -= l.shift[0]
	y -= l.shift[1]
	inside := s.InSurface && s.MouseX >= x && s.MouseX < x+w && s.MouseY >= y && s.MouseY < y+h
	if inside != l.hovered {
		l.hovered, anim, l.why = inside, true, "hover"
	}

	targetFG := l.FG
	if l.hovered && l.HasHover {
		targetFG = l.HoverFG
	}
	// Шаг пружины меньше половины байта — доезжать уже некуда:
	// доводим до цели одним махом, иначе кадры будут крутиться вечно.
	if c := blend(l.cur, targetFG, smear(16, dt)); c != targetFG && c != l.cur {
		l.cur, anim, l.why = c, true, "fg"
	} else {
		l.cur = targetFG
	}

	targetText := l.FG
	switch {
	case l.HasTextHi && l.hovered:
		targetText = l.TextHover
	case l.HasTextFG:
		targetText = l.TextFG
	case l.hovered && l.HasHover:
		targetText = l.HoverFG
	}
	if c := blend(l.textCur, targetText, smear(16, dt)); c != targetText && c != l.textCur {
		l.textCur, anim, l.why = c, true, "text"
	} else {
		l.textCur = targetText
	}

	targetHS := 1.0
	if l.hovered && l.HoverScale > 0 {
		targetHS = l.HoverScale
	}
	if v := l.hoverScale + (targetHS-l.hoverScale)*smear(14, dt); math.Abs(v-targetHS) > 0.001 {
		l.hoverScale, anim, l.why = v, true, "hoverScale"
	} else {
		l.hoverScale = targetHS
	}

	targetScale := 1.0
	if l.pressed {
		targetScale = 1 - l.ClickScale
	}
	decay := l.ClickDecay
	if decay <= 0 {
		decay = 9
	}
	if v := l.scale + (targetScale-l.scale)*smear(decay, dt); math.Abs(v-targetScale) > 0.001 {
		l.scale, anim, l.why = v, true, "scale"
	} else {
		l.scale = targetScale
	}

	// наклон: rotate + hover.rotate — пружиной
	targetRot := l.Rotate
	if l.hovered {
		targetRot += l.HoverRot
	}
	if v := l.angle + (targetRot-l.angle)*smear(14, dt); math.Abs(v-targetRot) > 0.01 {
		l.angle, anim, l.why = v, true, "rotate"
	} else {
		l.angle = targetRot
	}

	// вращение по клику
	if l.spinOn {
		l.spinT += dt
		if l.spinT >= l.SpinDur {
			l.spinOn, l.spinT = false, 0 // обороты целые — встаём как стояли
		}
		anim, l.why = true, "spin"
	}
	return anim
}

// --- рисование -------------------------------------------------------

// Render рисует сцену в картинку размера поверхности.
func (s *Scene) Render(img *image.RGBA, w, h float64) {
	s.Resize(w, h)
	fillBG(img, s.cfg.BG)

	// слои с follow зависят от положения других — считаем их первыми
	for _, l := range s.cfg.Layers {
		if l.Follow != "" {
			s.updateFollow(l)
		}
	}
	for _, l := range s.cfg.Layers {
		if !s.visible(l) {
			continue
		}
		if len(l.Cmds) > 0 || l.Radius > 0 {
			s.drawShape(img, l)
		}
		if l.Dots {
			s.drawDots(img, l)
		}
		if l.shown != "" {
			s.drawText(img, l)
		}
		if l.MenuCmd != "" {
			if l.Grid {
				s.drawGrid(img, l)
			} else {
				s.drawMenu(img, l)
			}
		}
	}
}

// updateFollow досчитывает смещение для follow-слоя (без анимации).
func (s *Scene) updateFollow(l *Layer) {
	t := s.byName[l.Follow]
	if t == nil {
		return
	}
	tx, ty, tw, th := s.layout(t)
	lo, hi := tx+l.Pad, tx+tw-l.Pad
	if !l.centerInit {
		l.center, l.centerInit = (lo+hi)/2, true
	}
	if l.Axis == "y" {
		lo, hi = ty+l.Pad, ty+th-l.Pad
		l.shift[1] = l.center - (l.Y + l.H/2)
	} else {
		l.shift[0] = l.center - (l.X + l.W/2)
	}
	l.center = clamp(l.center, lo, hi)
}

func (s *Scene) layerRect(l *Layer) (x, y, w, h float64) {
	x, y, w, h = s.layout(l)
	return x - l.shift[0], y - l.shift[1], w, h
}

// InputRects — прямоугольники видимых слоёв. По ним бэкенд сужает область
// ввода поверхности: прозрачные зоны не должны перехватывать мышь.
func (s *Scene) InputRects() [][4]int {
	out := s.rectBuf[:0]
	for _, l := range s.cfg.Layers {
		if !s.visible(l) {
			continue
		}
		// подпись без плашки и без действия мышь не ловит: иначе текст
		// поверх прозрачного фона перехватывал бы клики по окнам
		if len(l.Cmds) == 0 && l.Radius <= 0 && l.MenuCmd == "" &&
			l.ClickExec == "" && l.MiddleExec == "" && l.RightExec == "" &&
			l.WheelUp == "" && l.WheelDown == "" && l.Toggle == "" {
			continue
		}
		x, y, w, h := s.layerRect(l)
		if w < 1 || h < 1 {
			continue
		}
		out = append(out, [4]int{int(x), int(y), int(w), int(h)})
	}
	s.rectBuf = out
	return out
}

// --- индикатор воркспейсов ------------------------------------------

// updateDots читает "N A" из опроса и ведёт ширину каждой точки к цели.
// Первый замер — без анимации; смена активного воркспейса — пружиной.
func (s *Scene) updateDots(l *Layer, dt float64) bool {
	v := s.getPoll(l.DotsPoll)
	if v == "" {
		v = "1 1"
	}
	var n, active int
	if _, err := fmt.Sscanf(v, "%d %d", &n, &active); err != nil {
		return false
	}
	if n < 1 {
		n = 1
	}
	if n > 12 {
		n = 12
	}
	if active < 1 {
		active = 1
	}
	if active > n {
		active = n
	}

	size := l.DotSize
	if size <= 0 {
		size = 7
	}
	activeW := l.DotActiveW
	if activeW < size {
		activeW = 24
	}

	changed := false
	if !l.dotsInit || v != l.dotsVal {
		l.dotsVal, l.dotsActive = v, active
		if n != len(l.dotsCur) {
			cur := make([]float64, n)
			copy(cur, l.dotsCur)
			l.dotsCur = cur
			l.dotsTarget = make([]float64, n)
		}
		for i := range l.dotsCur {
			want := size
			if i+1 == active {
				want = activeW
			}
			l.dotsTarget[i] = want
			if !l.dotsInit {
				l.dotsCur[i] = want // стартуем сразу в форме, без «вырастания»
			}
		}
		l.dotsInit = true
		changed = true
	}
	if dt <= 0 {
		return changed
	}

	anim := changed
	for i, cur := range l.dotsCur {
		want := l.dotsTarget[i]
		next := cur + (want-cur)*smear(20, dt)
		if math.Abs(next-want) > 0.05 {
			l.dotsCur[i] = next
			anim = true
		} else {
			l.dotsCur[i] = want
		}
	}
	return anim
}

// drawDots рисует точки по центру слоя. Активная — вытянутая капсула.
func (s *Scene) drawDots(img *image.RGBA, l *Layer) {
	if !l.Dots {
		return
	}
	s.updateDots(l, 0) // в дампе (без Update) точки должны быть готовы
	x, y, w, h := s.layerRect(l)
	if len(l.dotsCur) == 0 {
		return
	}

	// Числовой режим: ровные плитки с номерами, активная — primary.
	if l.DotNums {
		size := l.DotSize
		if size <= 0 {
			size = 30
		}
		if size > h-8 {
			size = h - 8
		}
		gap := l.DotGap
		if gap <= 0 {
			gap = 8
		}
		n := len(l.dotsCur)
		sx := x // числовой индикатор прижат влево, как обычный элемент бара
		sy := y + (h-size)/2
		alpha := ease(l.appearT)
		face := s.faceSize(l.FontSize)
		radius := size * 0.34
		for i := 0; i < n; i++ {
			active := i+1 == l.dotsActive
			bgc := l.DotFG
			if active {
				bgc = l.DotActive
			}
			if bgc.A != 0 {
				fillRoundRect(s.raster, img, sx, sy, size, size, radius, withAlpha(bgc, alpha))
			}
			tc := l.DotNumFG
			if active {
				tc = l.DotNumActiveFG
			}
			if face != nil && tc.A != 0 {
				s.drawGlyphCentered(img, face, withAlpha(tc, alpha), sx, sy, size, strconv.Itoa(i+1))
			}
			sx += size + gap
		}
		return
	}

	size := l.DotSize
	if size <= 0 {
		size = 7
	}
	if size > h-6 {
		size = h - 6
	}
	gap := l.DotGap
	if gap <= 0 {
		gap = 9
	}
	total := gap * float64(len(l.dotsCur)-1)
	for _, dw := range l.dotsCur {
		if dw < size {
			dw = size
		}
		total += dw
	}
	sx := x + (w-total)/2
	sy := y + (h-size)/2
	inactive := l.DotFG
	if inactive.A == 0 {
		inactive = color.NRGBA{76, 86, 106, 255}
	}
	active := l.DotActive
	if active.A == 0 {
		active = color.NRGBA{136, 192, 208, 255}
	}
	alpha := ease(l.appearT)
	for i, dw := range l.dotsCur {
		if dw < size {
			dw = size
		}
		col := inactive
		if i+1 == l.dotsActive {
			col = active
		}
		fillRoundRect(s.raster, img, sx, sy, dw, size, size/2, withAlpha(col, alpha))
		sx += dw + gap
	}
}

func (s *Scene) drawShape(img *image.RGBA, l *Layer) {
	x, y, w, h := s.layerRect(l)
	cx, cy := x+w/2, y+h/2
	scale := l.scale * l.hoverScale * (0.85 + 0.15*ease(l.appearT))

	// наклон вокруг центра
	ang := l.angleNow() * math.Pi / 180
	cos, sin := math.Cos(ang), math.Sin(ang)

	at := func(nx, ny float64) (float64, float64) {
		px := x + (nx+1)/2*w
		py := y + (ny+1)/2*h
		dx, dy := (px-cx)*scale, (py-cy)*scale
		return cx + dx*cos - dy*sin, cy + dx*sin + dy*cos
	}

	col := withAlpha(l.cur, ease(l.appearT))

	if len(l.Cmds) == 0 && l.Radius > 0 {
		// простой скруглённый прямоугольник: радиус задан в пикселях
		rw, rh := w*scale, h*scale
		fillRoundRect(s.raster, img, cx-rw/2, cy-rh/2, rw, rh, l.Radius*scale, col)
		return
	}

	// Считаем bounding box пути: растеризуем только его, а не всю
	// поверхность. Иначе каждая мелкая иконка на полноэкранном лаунчере
	// заставляла бы растеризатор проходить все 2 мегапикселя.
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	add := func(px, py float64) {
		minX, minY = math.Min(minX, px), math.Min(minY, py)
		maxX, maxY = math.Max(maxX, px), math.Max(maxY, py)
	}
	for _, c := range l.Cmds {
		n := 1
		if c.op == 'C' {
			n = 3 // контрольные точки охватывают всю кривую
		}
		for k := 0; k < n; k++ {
			add(at(c.pts[k][0], c.pts[k][1]))
		}
	}
	bx0, by0, bx1, by1 := shapeBounds(img, minX, minY, maxX, maxY)
	if bx1 <= bx0 || by1 <= by0 {
		return
	}
	r := s.raster
	r.Reset(bx1-bx0, by1-by0)
	ox, oy := float64(bx0), float64(by0)
	lat := func(nx, ny float64) (float32, float32) {
		px, py := at(nx, ny)
		return float32(px - ox), float32(py - oy)
	}
	for _, c := range l.Cmds {
		switch c.op {
		case 'M':
			r.MoveTo(lat(c.pts[0][0], c.pts[0][1]))
		case 'L':
			r.LineTo(lat(c.pts[0][0], c.pts[0][1]))
		case 'C':
			bx, by := lat(c.pts[0][0], c.pts[0][1])
			cx2, cy2 := lat(c.pts[1][0], c.pts[1][1])
			dx, dy := lat(c.pts[2][0], c.pts[2][1])
			r.CubeTo(bx, by, cx2, cy2, dx, dy)
		case 'Z':
			r.ClosePath()
		}
	}
	r.Draw(img, image.Rect(bx0, by0, bx1, by1), uni(col), image.Point{})
}

// shapeBounds переводит вещественный bbox фигуры в целые пиксели
// поверхности (с запасом на сглаживание), обрезая по краям картинки.
func shapeBounds(img *image.RGBA, minX, minY, maxX, maxY float64) (x0, y0, x1, y1 int) {
	if math.IsInf(minX, 1) || math.IsInf(maxX, -1) {
		return 0, 0, 0, 0
	}
	b := img.Bounds()
	x0 = int(math.Floor(minX)) - 1
	y0 = int(math.Floor(minY)) - 1
	x1 = int(math.Ceil(maxX)) + 1
	y1 = int(math.Ceil(maxY)) + 1
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	return
}

// drawMenu рисует список-меню: значок в цветной плитке, подпись и подсветка
func (s *Scene) drawMenu(img *image.RGBA, l *Layer) {
	items := s.menuItems(l)
	if len(items) == 0 {
		return
	}
	face := s.face(l)
	if face == nil {
		return
	}
	x, y, w, h := s.layerRect(l)
	rh := l.rowH()
	pad := l.Pad
	if pad <= 0 {
		pad = 12
	}
	chip := rh - 12
	if chip > 38 {
		chip = 38
	}
	iconFace := s.faceSize(chip * 0.46)
	m := face.Metrics()
	asc, desc := float64(m.Ascent)/64, float64(m.Descent)/64
	rows := int(h / rh)
	alpha := ease(l.appearT)
	white := color.NRGBA{255, 255, 255, 255}

	textCol := color.NRGBA{225, 228, 218, 255}
	if l.HasTextFG {
		textCol = l.TextFG
	}
	hiCol := textCol
	if l.HasTextHi {
		hiCol = l.TextHover
	}
	base := func(ry float64) float64 { return ry + (rh-(asc+desc))/2 + asc }

	for n := 0; n < rows; n++ {
		i := l.scroll + n
		if i >= len(items) {
			break
		}
		it := items[i]
		ry := y + float64(n)*rh
		hovered := s.InSurface && s.MouseX >= x && s.MouseX < x+w &&
			s.MouseY >= ry && s.MouseY < ry+rh
		col := textCol
		if hovered {
			fillRoundRect(s.raster, img, x+2, ry+2, w-4, rh-6, (rh-6)/2, withAlpha(l.cur, alpha))
			col = hiCol
		}
		tx := x + pad
		if it.icon != "" {
			switch {
			case strings.HasPrefix(it.icon, "/"): // настоящая иконка (PNG)
				sz := int(chip)
				if ic := menuIcon(it.icon, sz); ic != nil {
					cy := int(ry + (rh-chip)/2)
					blitIcon(img, image.Rect(int(tx), cy, int(tx)+sz, cy+sz), ic, alpha)
					tx += chip + pad*0.75
				}
			case it.color != "" && iconFace != nil: // глиф в цветной плитке
				cc := it.col
				cy := ry + (rh-chip)/2
				fillRoundRect(s.raster, img, tx, cy, chip, chip, chip*0.3, withAlpha(cc, alpha))
				s.drawGlyphCentered(img, iconFace, withAlpha(white, alpha), tx, cy, chip, it.icon)
				tx += chip + pad*0.75
			default: // просто глиф перед подписью
				tx += s.drawGlyph(img, face, withAlpha(col, alpha), tx, base(ry), it.icon) + pad*0.7
			}
		}
		s.drawGlyph(img, face, withAlpha(col, alpha), tx, base(ry),
			fitString(face, it.label, x+w-pad-tx))
	}
}

// drawGrid рисует сетку плиток (лаунчер): значок по центру ячейки, под ним
// подпись. Наведение — скруглённый «слой состояния» под ячейкой.
func (s *Scene) drawGrid(img *image.RGBA, l *Layer) {
	items := s.menuItems(l)
	alpha := ease(l.appearT)
	cols, cellW, cellH, ox, oy := s.gridGeom(l)

	if len(items) == 0 {
		if l.EmptyText != "" {
			if face := s.faceSize(l.FontSize + 4); face != nil {
				x, y, w, h := s.layerRect(l)
				col := color.NRGBA{188, 197, 182, 255}
				if l.HasTextFG {
					col = l.TextFG
				}
				lw := stringWidth(face, l.EmptyText)
				s.drawGlyph(img, face, withAlpha(col, alpha),
					x+(w-lw)/2, y+h/2, l.EmptyText)
			}
		}
		return
	}

	iconSize := l.IconSize
	if iconSize <= 0 {
		iconSize = 64
	}
	topPad := l.TopPad
	if topPad <= 0 {
		topPad = 16
	}
	labelFace := s.faceSize(l.FontSize)
	glyphFace := s.faceSize(iconSize * 0.52)
	white := color.NRGBA{255, 255, 255, 255}
	labelCol := color.NRGBA{225, 228, 218, 255}
	if l.HasTextFG {
		labelCol = l.TextFG
	}
	hiCol := labelCol
	if l.HasTextHi {
		hiCol = l.TextHover
	}
	_, _, _, h := s.layerRect(l)
	rows := int(h / cellH)
	labelAsc := 0.0
	if labelFace != nil {
		labelAsc = float64(labelFace.Metrics().Ascent) / 64
	}

	for n := 0; n < rows; n++ {
		row := l.scroll + n
		for c := 0; c < cols; c++ {
			i := row*cols + c
			if i >= len(items) {
				return
			}
			it := items[i]
			cx := ox + float64(c)*cellW
			cy := oy + float64(n)*cellH
			hovered := s.InSurface && s.MouseX >= cx && s.MouseX < cx+cellW &&
				s.MouseY >= cy && s.MouseY < cy+cellH
			col := labelCol
			if hovered {
				fillRoundRect(s.raster, img, cx+6, cy+4, cellW-12, cellH-8, 22,
					withAlpha(l.cur, alpha))
				col = hiCol
			}
			ix := cx + (cellW-iconSize)/2
			iy := cy + topPad
			switch {
			case strings.HasPrefix(it.icon, "/"): // настоящая иконка
				if ic := menuIcon(it.icon, int(iconSize)); ic != nil {
					blitIcon(img, image.Rect(int(ix), int(iy), int(ix+iconSize), int(iy+iconSize)), ic, alpha)
				}
			case it.color != "" && glyphFace != nil: // глиф в цветной плитке
				cc := it.col
				fillRoundRect(s.raster, img, ix, iy, iconSize, iconSize, iconSize*0.24,
					withAlpha(cc, alpha))
				s.drawGlyphCentered(img, glyphFace, withAlpha(white, alpha), ix, iy, iconSize, it.icon)
			case glyphFace != nil: // просто глиф
				s.drawGlyphCentered(img, glyphFace, withAlpha(col, alpha), ix, iy, iconSize, it.icon)
			}
			if labelFace != nil {
				label := fitString(labelFace, it.label, cellW-18)
				lw := stringWidth(labelFace, label)
				baseline := iy + iconSize + 16 + labelAsc
				s.drawGlyph(img, labelFace, withAlpha(col, alpha),
					cx+(cellW-lw)/2, baseline, label)
			}
		}
	}
}

// stringWidth — ширина строки шрифтом в пикселях.
func stringWidth(face font.Face, str string) float64 {
	return float64(font.MeasureString(face, str)) / 64
}

// --- отрисовка строк без лишнего мусора -------------------------------

// uni — цветной src для glyph-рисования. Цветов в панели мало, а строк
// в кадре — много: полупрозрачные градиенты hover/появления дают десятки
// уникальных цветов, но конечное число (≈ количество шагов blend).
var uniCache sync.Map // color.NRGBA -> *image.Uniform

func uni(c color.NRGBA) *image.Uniform {
	if v, ok := uniCache.Load(c); ok {
		return v.(*image.Uniform)
	}
	u, _ := uniCache.LoadOrStore(c, image.NewUniform(c))
	return u.(*image.Uniform)
}

// measureString — advance строки в fixed-пикселях 26.6, с кэшем. За кадр
// одна и та же строка мелькает много раз: fitString ищет длину двоичным
// поиском (log₂ мерок на пункт меню), drawGlyphCentered меряет до
// центрирования, drawTextInto — до выравнивания. Без кэша каждый кадр
// заново грузит глифы из шрифта (это was 7% профиля).
type widthKey struct {
	face font.Face
	str  string
}

var (
	widthCache    sync.Map // widthKey -> fixed.Int26_6
	widthCount    atomic.Int64
	widthCacheMax = 4096
)

func measureString(face font.Face, str string) fixed.Int26_6 {
	if str == "" {
		return 0
	}
	k := widthKey{face, str}
	if v, ok := widthCache.Load(k); ok {
		return v.(fixed.Int26_6)
	}
	adv := font.MeasureString(face, str)
	widthCache.Store(k, adv)
	if widthCount.Add(1) > int64(widthCacheMax) {
		widthCache.Range(func(key, _ any) bool {
			widthCache.Delete(key)
			return true
		})
		widthCount.Store(0)
	}
	return adv
}

// drawString — нарисовать строку в точку шрифтом и цветом src.
// Drawer берётся из пула: раньше на каждый глиф в каждом кадре аллокация.
var drawerPool sync.Pool

func drawString(img *image.RGBA, face font.Face, src *image.Uniform, dot fixed.Point26_6, str string) fixed.Int26_6 {
	if str == "" {
		return 0
	}
	d, _ := drawerPool.Get().(*font.Drawer)
	if d == nil {
		d = &font.Drawer{}
	}
	defer drawerPool.Put(d)
	d.Dst, d.Src, d.Face = img, src, face
	d.Dot = dot
	d.DrawString(str)
	return measureString(face, str)
}

// blitIcon накладывает иконку. При полной непрозрачности маска не нужна —
// draw.Draw идёт быстрым путём, а DrawMask с Uniform-маской — общим.
func blitIcon(dst *image.RGBA, r image.Rectangle, ic *image.RGBA, alpha float64) {
	if alpha >= 1 {
		draw.Draw(dst, r, ic, image.Point{}, draw.Over)
		return
	}
	mask := image.NewUniform(color.Alpha{uint8(clamp(alpha, 0, 1)*255 + 0.5)})
	draw.DrawMask(dst, r, ic, image.Point{}, mask, image.Point{}, draw.Over)
}

// menuIcon возвращает уменьшенную копию иконки-файла (кэшируется).
type iconKey struct {
	path string
	size int
}

func menuIcon(path string, size int) *image.RGBA {
	key := iconKey{path, size}
	if v, ok := iconCache.Load(key); ok {
		if v == nil {
			return nil
		}
		return v.(*image.RGBA)
	}
	f, err := os.Open(path)
	if err != nil {
		iconCache.Store(key, (*image.RGBA)(nil))
		return nil
	}
	defer f.Close()
	src, err := png.Decode(f)
	if err != nil {
		iconCache.Store(key, (*image.RGBA)(nil))
		return nil
	}
	dst := scaleIcon(src, size)
	iconCache.Store(key, dst)
	return dst
}

// scaleIcon масштабирует картинку в квадрат size×size билинейно.
func scaleIcon(src image.Image, size int) *image.RGBA {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	if sw == 0 || sh == 0 {
		return dst
	}
	mix := func(a, b float64, t float64) float64 { return a + (b-a)*t }
	bl := func(v00, v10, v01, v11 uint32, wx, wy float64) uint8 {
		top := mix(float64(v00), float64(v10), wx)
		bot := mix(float64(v01), float64(v11), wx)
		return uint8(mix(top, bot, wy) / 257)
	}
	for y := 0; y < size; y++ {
		fy := (float64(y)+0.5)*float64(sh)/float64(size) - 0.5
		y0 := int(math.Floor(fy))
		wy := fy - float64(y0)
		if y0 < 0 {
			y0, wy = 0, 0
		}
		y1 := y0 + 1
		if y1 >= sh {
			y1 = sh - 1
		}
		for x := 0; x < size; x++ {
			fx := (float64(x)+0.5)*float64(sw)/float64(size) - 0.5
			x0 := int(math.Floor(fx))
			wx := fx - float64(x0)
			if x0 < 0 {
				x0, wx = 0, 0
			}
			x1 := x0 + 1
			if x1 >= sw {
				x1 = sw - 1
			}
			r00, g00, b00, a00 := src.At(sb.Min.X+x0, sb.Min.Y+y0).RGBA()
			r10, g10, b10, a10 := src.At(sb.Min.X+x1, sb.Min.Y+y0).RGBA()
			r01, g01, b01, a01 := src.At(sb.Min.X+x0, sb.Min.Y+y1).RGBA()
			r11, g11, b11, a11 := src.At(sb.Min.X+x1, sb.Min.Y+y1).RGBA()
			i := dst.PixOffset(x, y)
			dst.Pix[i+0] = bl(r00, r10, r01, r11, wx, wy)
			dst.Pix[i+1] = bl(g00, g10, g01, g11, wx, wy)
			dst.Pix[i+2] = bl(b00, b10, b01, b11, wx, wy)
			dst.Pix[i+3] = bl(a00, a10, a01, a11, wx, wy)
		}
	}
	return dst
}

// drawGlyphCentered рисует значок по центру квадратной плитки
type glyphBoxKey struct {
	face font.Face
	str  string
}

type glyphBox struct {
	minX, minY, maxX, maxY float64
}

var glyphBoxCache sync.Map

// glyphInk — реальные (чернильные) границы глифа в пикселях.
// Nerd Font иконки имеют разные боковые поля, поэтому центрировать их
// по advance-width нельзя: получается кривой сдвиг вправо/влево.
func glyphInk(face font.Face, str string) glyphBox {
	k := glyphBoxKey{face, str}
	if v, ok := glyphBoxCache.Load(k); ok {
		return v.(glyphBox)
	}
	b, _ := font.BoundString(face, str)
	gb := glyphBox{
		minX: float64(b.Min.X) / 64, minY: float64(b.Min.Y) / 64,
		maxX: float64(b.Max.X) / 64, maxY: float64(b.Max.Y) / 64,
	}
	glyphBoxCache.Store(k, gb)
	return gb
}

// drawGlyphCentered — центрирует именно нарисованный глиф, а не его
// advance-box: так иконки в кнопках стоят ровно по центру.
func (s *Scene) drawGlyphCentered(img *image.RGBA, face font.Face, col color.NRGBA, x, y, size float64, str string) {
	if str == "" {
		return
	}
	gb := glyphInk(face, str)
	bw, bh := gb.maxX-gb.minX, gb.maxY-gb.minY
	if bw <= 0 || bh <= 0 {
		return
	}
	tx := x + (size-bw)/2 - gb.minX + 0.5
	baseline := y + (size-bh)/2 - gb.minY + 0.5
	s.drawGlyph(img, face, col, tx, baseline, str)
}

// drawGlyph рисует строку шрифтом в точку и возвращает её ширину
func (s *Scene) drawGlyph(img *image.RGBA, face font.Face, col color.NRGBA, tx, baseline float64, str string) float64 {
	if str == "" {
		return 0
	}
	w := drawString(img, face, uni(col), fixed.P(int(tx), int(baseline)), str)
	return float64(w) / 64
}

// fitString обрезает строку под ширину, добавляя многоточие.
// Ширина монотонна по префиксу, поэтому подходящую длину ищем двоичным
// поиском, а не перебором каждого символа (длинные подписи в меню).
// Результат мемоизируется: подписи и ширины не меняются, а бин-поиск
// строил на каждый кадр временные строки под каждый шаг поиска.
type fitKey struct {
	face font.Face
	str  string
	maxw uint64 // exact биты maxw: ширина слоя стабильна
}

var fitCache sync.Map

func fitString(face font.Face, str string, maxw float64) string {
	if maxw <= 0 {
		return ""
	}
	k := fitKey{face, str, math.Float64bits(maxw)}
	if v, ok := fitCache.Load(k); ok {
		return v.(string)
	}
	out := fitStringCalc(face, str, maxw)
	fitCache.Store(k, out)
	if widthCount.Add(1) > int64(widthCacheMax) { // общий лимит с ширинами
		fitCache.Range(func(key, _ any) bool {
			fitCache.Delete(key)
			return true
		})
		widthCount.Store(0)
	}
	return out
}

func fitStringCalc(face font.Face, str string, maxw float64) string {
	measure := func(s string) float64 { return float64(measureString(face, s)) / 64 }
	if measure(str) <= maxw {
		return str
	}
	r := []rune(str)
	lo, hi := 0, len(r)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if measure(string(r[:mid])+"…") <= maxw {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	if lo == 0 {
		return ""
	}
	return string(r[:lo]) + "…"
}

// fillBG заливает поверхность фоном. Для полностью прозрачного фона это
// обычный memclr, для непрозрачного — быстрое заполнение по образцу вместо
// попиксельного draw.Draw.
func fillBG(img *image.RGBA, c color.NRGBA) {
	w := img.Rect.Dx()
	if w <= 0 {
		return
	}
	if c.A == 0 {
		clear(img.Pix)
		return
	}
	a := uint32(c.A)
	px := [4]byte{uint8(uint32(c.R) * a / 255), uint8(uint32(c.G) * a / 255),
		uint8(uint32(c.B) * a / 255), uint8(c.A)}
	for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
		row := img.Pix[img.PixOffset(img.Rect.Min.X, y):][:4*w]
		row[0], row[1], row[2], row[3] = px[0], px[1], px[2], px[3]
		for n := 4; n < len(row); n *= 2 {
			copy(row[n:], row[:min(n, len(row)-n)])
		}
	}
}

// fillRoundRect заливает скруглённый прямоугольник (для подсветки строк меню).
// Растеризуем только область фигуры: сброс растеризатора на весь экран для
// каждой плитки — самая дорогая операция в кадре.
func fillRoundRect(r *vector.Rasterizer, img *image.RGBA, x, y, w, h, rad float64, col color.NRGBA) {
	if w <= 0 || h <= 0 || col.A == 0 {
		return
	}
	if rad > w/2 {
		rad = w / 2
	}
	if rad > h/2 {
		rad = h / 2
	}
	// Фигура накрывает всю поверхность, а скругление крошечное (фон
	// лаунчера): дешевле залить всё, чем гонять растеризатор по экрану.
	b := img.Bounds()
	if rad <= 6 && x <= float64(b.Min.X) && y <= float64(b.Min.Y) &&
		x+w >= float64(b.Max.X) && y+h >= float64(b.Max.Y) {
		if col.A == 255 {
			fillBG(img, col)
		} else {
			draw.Draw(img, b, uni(col), image.Point{}, draw.Over)
		}
		return
	}
	x0, y0, x1, y1 := shapeBounds(img, x, y, x+w, y+h)
	if x1 <= x0 || y1 <= y0 {
		return
	}
	ox, oy := float64(x0), float64(y0)
	x, y = x-ox, y-oy
	k := 0.552285 * rad
	r.Reset(x1-x0, y1-y0)
	r.MoveTo(float32(x+rad), float32(y))
	r.LineTo(float32(x+w-rad), float32(y))
	r.CubeTo(float32(x+w-rad+k), float32(y), float32(x+w), float32(y+rad-k), float32(x+w), float32(y+rad))
	r.LineTo(float32(x+w), float32(y+h-rad))
	r.CubeTo(float32(x+w), float32(y+h-rad+k), float32(x+w-rad+k), float32(y+h), float32(x+w-rad), float32(y+h))
	r.LineTo(float32(x+rad), float32(y+h))
	r.CubeTo(float32(x+rad-k), float32(y+h), float32(x), float32(y+h-rad+k), float32(x), float32(y+h-rad))
	r.LineTo(float32(x), float32(y+rad))
	r.CubeTo(float32(x), float32(y+rad-k), float32(x+rad-k), float32(y), float32(x+rad), float32(y))
	r.ClosePath()
	r.Draw(img, image.Rect(x0, y0, x1, y1), uni(col), image.Point{})
}

func (s *Scene) drawText(img *image.RGBA, l *Layer) {
	face := s.face(l)
	if face == nil {
		return
	}
	x, y, w, h := s.layerRect(l)
	if a := math.Mod(l.angleNow(), 360); math.Abs(a) > 0.2 {
		s.drawTextRotated(img, l, face, x, y, w, h, a)
		return
	}
	s.drawTextInto(img, l, face, x, y, w, h)
}

// drawTextInto — обычная отрисовка текста в прямоугольник слоя
func (s *Scene) drawTextInto(img *image.RGBA, l *Layer, face font.Face, x, y, w, h float64) {
	tw := float64(measureString(face, l.shown)) / 64
	c := withAlpha(l.textCur, ease(l.appearT))

	// Одиночный глиф (иконка в кружке/квадрате): центрируем по
	// чернильным границам, иначе Nerd Font уезжает вправо/вниз.
	if l.TextAlign == "center" && utf8.RuneCountInString(l.shown) == 1 {
		gb := glyphInk(face, l.shown)
		bw, bh := gb.maxX-gb.minX, gb.maxY-gb.minY
		if bw > 0 && bh > 0 {
			tx := x + (w-bw)/2 - gb.minX + 0.5
			baseline := y + (h-bh)/2 - gb.minY + 0.5
			drawString(img, face, uni(c), fixed.P(int(tx), int(baseline)), l.shown)
			return
		}
	}

	pad := l.TextPad
	if pad == 0 {
		pad = 4
	}
	pad += (w * (l.hoverScale - 1)) / 2 // текст не масштабируем, но центр держим

	switch l.TextAlign {
	case "right":
		x += w - tw - pad
	case "center":
		x += (w - tw) / 2
	default:
		x += pad
	}

	asc, desc := float64(face.Metrics().Ascent)/64, float64(face.Metrics().Descent)/64
	baseline := y + (h-(asc+desc))/2 + asc
	drawString(img, face, uni(c), fixed.P(int(x), int(baseline)), l.shown)
}

// drawTextRotated — текст в наклонённом слое: рисуем в буфер и поворачиваем
func (s *Scene) drawTextRotated(dst *image.RGBA, l *Layer, face font.Face, x, y, w, h, ang float64) {
	iw, ih := int(math.Ceil(w)), int(math.Ceil(h))
	if iw <= 0 || ih <= 0 || iw > 2048 || ih > 2048 || iw*ih > 500_000 {
		s.drawTextInto(dst, l, face, x, y, w, h) // слишком крупный слой — не крутим
		return
	}
	if l.tmp == nil || l.tmp.Bounds().Dx() != iw || l.tmp.Bounds().Dy() != ih {
		l.tmp = image.NewRGBA(image.Rect(0, 0, iw, ih))
	} else {
		clear(l.tmp.Pix)
	}
	s.drawTextInto(l.tmp, l, face, 0, 0, w, h)

	a := ang * math.Pi / 180
	cos, sin := math.Cos(a), math.Sin(a)
	cx, cy := x+w/2, y+h/2
	nx := int(math.Ceil(math.Abs(w*cos) + math.Abs(h*sin)))
	ny := int(math.Ceil(math.Abs(w*sin) + math.Abs(h*cos)))
	src := l.tmp.Pix
	for dy := -ny / 2; dy < ny/2; dy++ {
		for dx := -nx / 2; dx < nx/2; dx++ {
			// обратный поворот: где эта точка лежит в исходном буфере
			fx, fy := float64(dx), float64(dy)
			sx := fx*cos + fy*sin + w/2
			sy := -fx*sin + fy*cos + h/2
			c, ok := sampleRGBA(src, iw, ih, sx, sy)
			if !ok || c.A == 0 {
				continue
			}
			paintOver(dst, int(cx)+dx, int(cy)+dy, c)
		}
	}
}

// sampleRGBA — билинейная выборка (пиксели premultiplied)
func sampleRGBA(pix []byte, w, h int, x, y float64) (color.RGBA, bool) {
	if x < 0 || y < 0 || x >= float64(w-1) || y >= float64(h-1) {
		return color.RGBA{}, false
	}
	x0, y0 := int(x), int(y)
	fx, fy := x-float64(x0), y-float64(y0)
	at := func(px, py int) (float64, float64, float64, float64) {
		i := (py*w + px) * 4
		return float64(pix[i]), float64(pix[i+1]), float64(pix[i+2]), float64(pix[i+3])
	}
	r0, g0, b0, a0 := at(x0, y0)
	r1, g1, b1, a1 := at(x0+1, y0)
	r2, g2, b2, a2 := at(x0, y0+1)
	r3, g3, b3, a3 := at(x0+1, y0+1)
	mix := func(v0, v1, v2, v3 float64) float64 {
		top := v0*(1-fx) + v1*fx
		bot := v2*(1-fx) + v3*fx
		return top*(1-fy) + bot*fy
	}
	return color.RGBA{
		uint8(mix(r0, r1, r2, r3) + 0.5), uint8(mix(g0, g1, g2, g3) + 0.5),
		uint8(mix(b0, b1, b2, b3) + 0.5), uint8(mix(a0, a1, a2, a3) + 0.5),
	}, true
}

// paintOver — наложить premultiplied цвет на картинку (src over dst)
func paintOver(dst *image.RGBA, x, y int, c color.RGBA) {
	b := dst.Bounds()
	if x < b.Min.X || y < b.Min.Y || x >= b.Max.X || y >= b.Max.Y {
		return
	}
	i := dst.PixOffset(x, y)
	k := 255 - int(c.A)
	p := dst.Pix[i : i+4 : i+4]
	p[0] = uint8(min(255, int(c.R)+int(p[0])*k/255))
	p[1] = uint8(min(255, int(c.G)+int(p[1])*k/255))
	p[2] = uint8(min(255, int(c.B)+int(p[2])*k/255))
	p[3] = uint8(min(255, int(c.A)+int(p[3])*k/255))
}

// face достаёт (и кэширует) шрифт нужного размера
func (s *Scene) face(l *Layer) font.Face {
	size := l.FontSize
	if size <= 0 {
		size = s.cfg.FontSize
	}
	return s.faceSize(size)
}

// faceSize — шрифт заданного кегля (для значков меню нужен свой размер).
// Кэш общий на процесс: раньше каждая из семи сцен заново читала и
// разбирала один и тот же файл шрифта для каждого кегля.
type faceKey struct {
	path string
	size float64
}

var (
	fontMu    sync.Mutex
	fontFiles = map[string]*opentype.Font{}
	faceCache = map[faceKey]font.Face{}
	fontBad   = map[faceKey]bool{} // уже ругнулись — не спамим каждый кадр
)

func (s *Scene) faceSize(size float64) font.Face {
	if size <= 0 {
		size = 14
	}
	key := faceKey{s.cfg.FontPath, size}
	fontMu.Lock()
	defer fontMu.Unlock()
	if f, ok := faceCache[key]; ok {
		return f
	}
	if fontBad[key] {
		return nil
	}
	path := key.path
	fnt := fontFiles[path]
	if fnt == nil {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "LNF: шрифт %s: %v\n", path, err)
			s.cfg.FontPath = ""
			fontBad[key] = true
			return nil
		}
		fnt, err = opentype.Parse(data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "LNF: шрифт %s: %v\n", path, err)
			s.cfg.FontPath = ""
			fontBad[key] = true
			return nil
		}
		fontFiles[path] = fnt
	}
	f, err := opentype.NewFace(fnt, &opentype.FaceOptions{
		Size: size, DPI: 72, Hinting: font.HintingFull,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "LNF: шрифт %s: %v\n", path, err)
		fontBad[key] = true
		return nil
	}
	faceCache[key] = f
	return f
}

// defaultFont — первый подходящий системный шрифт
func defaultFont() string {
	for _, p := range []string{
		"/usr/share/fonts/noto/NotoSans-Regular.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/TTF/DejaVuSans.ttf",
		"/usr/share/fonts/Adwaita/AdwaitaSans-Regular.ttf",
		"/usr/share/fonts/liberation/LiberationSans-Regular.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// ============================================================
// Мелочи
// ============================================================

func smear(rate, dt float64) float64 {
	if dt <= 0 {
		return 0
	}
	return 1 - math.Exp(-rate*dt)
}

// ease — плавный выход для появления
func ease(t float64) float64 {
	if t >= 1 {
		return 1
	}
	if t <= 0 {
		return 0
	}
	u := t - 1
	return u*u*u + 1
}

// easeInOut — разгон и торможение: вращение по клику
func easeInOut(t float64) float64 {
	if t >= 1 {
		return 1
	}
	if t <= 0 {
		return 0
	}
	return t * t * (3 - 2*t)
}

func clamp(v, lo, hi float64) float64 {
	if lo > hi {
		lo, hi = hi, lo
	}
	return math.Min(math.Max(v, lo), hi)
}

func withAlpha(c color.NRGBA, k float64) color.NRGBA {
	if k >= 1 {
		return c
	}
	c.A = uint8(float64(c.A)*clamp(k, 0, 1) + 0.5)
	return c
}

func blend(a, b color.NRGBA, t float64) color.NRGBA {
	f := func(x, y uint8) uint8 {
		return uint8(float64(x) + (float64(y)-float64(x))*t + 0.5)
	}
	return color.NRGBA{f(a.R, b.R), f(a.G, b.G), f(a.B, b.B), f(a.A, b.A)}
}

// ============================================================

func main() {
	trimMemory()

	// Своя программа обоев: собственный layer-shell клиент, без awww/swaybg/swww.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "wallpaper":
			if err := wpClient(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "lnf wallpaper:", err)
				os.Exit(1)
			}
			return
		case "wallpaperd":
			if err := wpRun(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "lnf wallpaperd:", err)
				os.Exit(1)
			}
			return
		}
	}
	// Панель описывается прямо в Go (panel.go). Движок получает готовые
	// сцены: одно соединение с компоновщиком, одна поверхность на сцену.
	cfgs := panelConfig()

	dbg := os.Getenv("LNF_DEBUG") != ""
	for _, cfg := range cfgs {
		if dbg {
			fmt.Fprintf(os.Stderr, "LNF: [%s] | поверхность %.0fx%.0f | слой=%s якоря=%d отступы=%v | шрифт %s %.0f | слоёв %d, опросов %d\n",
				cfg.Namespace, cfg.W, cfg.H, cfg.Layer, cfg.Anchor, cfg.Margin, cfg.FontPath, cfg.FontSize, len(cfg.Layers), len(cfg.Polls))
		}
		for _, l := range cfg.Layers {
			if l.Style != "fill" {
				fmt.Fprintf(os.Stderr, "LNF: %s: слой %q: style=%s пока не поддержан, рисую заливку\n", cfg.Namespace, l.Name, l.Style)
			}
		}
	}

	if out := os.Getenv("LNF_DUMP"); out != "" {
		for i, cfg := range cfgs {
			p := dumpPath(out, i)
			// 0 в размере = «растянуть по якорям»: в дампе берём экран,
			// чтобы вытянутые поверхности выглядели как в жизни
			w, h := int(cfg.W), int(cfg.H)
			if w <= 0 {
				w = 1920
			}
			if h <= 0 {
				h = 1080
			}
			sc := NewScene(cfg)
			// LNF_GROUPS=power — раскрыть группы-раскрывашки в дампе
			for _, g := range strings.Split(os.Getenv("LNF_GROUPS"), ",") {
				if g = strings.TrimSpace(g); g != "" {
					sc.groups[g] = true
				}
			}
			// LNF_SEARCH=foo — показать строку поиска (для ящика приложений)
			if v := os.Getenv("LNF_SEARCH"); v != "" {
				for _, l := range cfg.Layers {
					if l.Search {
						sc.setQuery(v)
						break
					}
				}
			}
			// LNF_NOTIFY=текст — показать баннер сообщения
			if v := os.Getenv("LNF_NOTIFY"); v != "" {
				if sc.flashesOn("notify") {
					sc.setPoll("notify", v)
					sc.markFlash("notify")
				}
			}
			sc.PollOnce()
			sc.RefreshText()
			for _, l := range cfg.Layers {
				if sc.shown(l) {
					l.appearT = 1 // в дампе показываем конечное состояние
				} else {
					l.appearT = 0
				}
				l.angle = l.Rotate
			}
			// LNF_SPIN=1.5 — показать вращение спустя столько секунд
			if v := os.Getenv("LNF_SPIN"); v != "" {
				f, err := strconv.ParseFloat(v, 64)
				if err != nil {
					log.Fatalf("LNF: LNF_SPIN=%s: %v", v, err)
				}
				for _, l := range cfg.Layers {
					if l.SpinDur > 0 {
						l.spinOn, l.spinT = true, f
					}
				}
			}
			img := image.NewRGBA(image.Rect(0, 0, w, h))
			sc.Render(img, float64(w), float64(h))
			f, err := os.Create(p)
			if err != nil {
				log.Fatalf("LNF: %v", err)
			}
			if err := png.Encode(f, img); err != nil {
				f.Close()
				log.Fatalf("LNF: %v", err)
			}
			f.Close()
			fmt.Fprintln(os.Stderr, "LNF: сцена", cfg.Namespace, "отрендерена в", p)
		}
		return
	}

	// через пару секунд, когда шрифты и буферы уже созданы,
	// вернём ядру то, что осталось от разбора и первого кадра
	time.AfterFunc(2*time.Second, debug.FreeOSMemory)

	if err := run(cfgs); err != nil {
		log.Fatalf("LNF: %v", err)
	}
}

// trimMemory держит кучу под контролем, но не заставляет GC работать
// вхолостую: буферы поверхностей и кэш иконок — это десятки мегабайт.
// Жёсткий лимит 96 МБ на «жирном» конфиге оказывался ниже живого heap,
// и сборщик молотил непрерывно. Лимит можно задать через LNF_MEM_LIMIT (МБ).
func trimMemory() {
	debug.SetGCPercent(60)
	limit := int64(256 << 20)
	if v := os.Getenv("LNF_MEM_LIMIT"); v != "" {
		if mb, err := strconv.ParseInt(v, 10, 64); err == nil && mb > 0 {
			limit = mb << 20
		}
	}
	debug.SetMemoryLimit(limit)
}

// dumpPath — для одной сцены путь как задан, для нескольких — с номером
func dumpPath(out string, i int) string {
	if i == 0 {
		return out
	}
	ext := path.Ext(out)
	return strings.TrimSuffix(out, ext) + "-" + strconv.Itoa(i+1) + ext
}
