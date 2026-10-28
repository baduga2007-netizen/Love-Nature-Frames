package main

// Своя программа обоев: клиент wlr-layer-shell, который сам рисует обои в
// слой background через wl_shm. Никаких awww/swaybg/swww — только Wayland.
//
//	./lnf wallpaperd                 — демон (слушает FIFO)
//	./lnf wallpaperd wall.png        — демон сразу с картинкой
//	./lnf wallpaperd gen:sage        — сгенерировать обои палитрой
//	./lnf wallpaper wall.png         — сказать демону поставить обои
//	./lnf wallpaper gen:moss         — то же, но сгенерировать
//
// FIFO: $XDG_RUNTIME_DIR/lnf-wallpaper.fifo
// Команды: set <путь|gen:имя>, fade <мс>, quit.
//
// Формат кадра — wl_shm ARGB8888 (image.RGBA ложится в него байт в байт),
// масштаб — cover: картинка обрезается по центру, без искажений.

import (
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	draw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"golang.org/x/sys/unix"
)

// палитры генератора: верх, низ, блик
var wpPalettes = map[string][3]color.NRGBA{
	"sage": {{0x7E, 0x9E, 0x67, 0xFF}, {0x27, 0x38, 0x22, 0xFF}, {0xDE, 0xFB, 0xB0, 0xFF}},
	"moss": {{0x4C, 0x66, 0x41, 0xFF}, {0x11, 0x1A, 0x0E, 0xFF}, {0xC9, 0xEC, 0x8F, 0xFF}},
	"dew":  {{0xAE, 0xCB, 0xA6, 0xFF}, {0x3C, 0x55, 0x44, 0xFF}, {0xF3, 0xF7, 0xE4, 0xFF}},
}

type wpBuf struct {
	id   uint32
	data []byte
	img  *image.RGBA
	busy bool
}

type wpSurface struct {
	output             uint32
	idSurface, idLayer uint32
	idPool             uint32
	memfd              int
	bufs               [2]*wpBuf
	W, H               int // логический размер (как его дал компоновщик)
	scale              int // масштаб выхода: буфер в W*scale x H*scale
	bufW, bufH         int
	lastBuf            int  // индекс буфера, показанного последним
	shown              bool // в буфере уже есть показанный кадр
	configured         bool
	bufScaleSent       bool
	sentScale          int

	from, to *image.RGBA // переход: из чего и во что
	next     *image.RGBA
	lastSpec string
	t0       time.Time
	fading   bool
	dirty    bool // кадр не влез: оба буфера заняты компоновщиком
}

type wpDaemon struct {
	c                                 *conn
	nextID                            uint32
	idCompositor, idShm, idLayerShell uint32
	outputs                           []uint32
	outScale                          map[uint32]int
	surfs                             []*wpSurface
	byObj                             map[uint32]*wpSurface
	fadeDur                           time.Duration
	lock                              *os.File
	started                           bool   // регистрация пройдена: новые выходы — сразу
	needReap                          bool   // вернуть ОС память после перехода
	spec                              string // текущие обои — для новых выходов
}

func wpFIFO() string {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "lnf-wallpaper.fifo")
	}
	return filepath.Join(os.TempDir(), "lnf-wallpaper.fifo")
}

// ---------------------------------------------------------------- клиент

// wpClient — команда панели/скрипта: положить в FIFO.
func wpClient(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("использование: lnf wallpaper <картинка|gen:sage>")
	}
	target, err := wpResolve(args[0])
	if err != nil {
		return err
	}
	fifo := wpFIFO()
	if fi, err := os.Stat(fifo); err != nil || fi.Mode()&os.ModeNamedPipe == 0 {
		if err := wpSpawnDaemon(); err != nil {
			return err
		}
		for i := 0; i < 60; i++ {
			if _, err := os.Stat(fifo); err == nil {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	f, err := os.OpenFile(fifo, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("демон обоев не отвечает (%s): %w", fifo, err)
	}
	defer f.Close()
	n, err := fmt.Fprintf(f, "set %s\n", target)
	if os.Getenv("LNF_WP_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "wp-client: %s <- %q (%d байт, err=%v)\n", fifo, target, n, err)
	}
	return err
}

func wpSpawnDaemon() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer devnull.Close()
	attr := &os.ProcAttr{
		Files: []*os.File{devnull, devnull, devnull},
		Env:   os.Environ(),
		Sys:   &syscall.SysProcAttr{Setsid: true},
	}
	_, err = os.StartProcess(exe, []string{exe, "wallpaperd"}, attr)
	return err
}

// wpResolve: gen:<имя> оставляем как есть, путь делаем абсолютным.
func wpResolve(s string) (string, error) {
	if strings.HasPrefix(s, "gen:") {
		if _, ok := wpPalettes[strings.TrimPrefix(s, "gen:")]; !ok {
			return "", fmt.Errorf("нет такой палитры: %s (есть sage, moss, dew)", s)
		}
		return s, nil
	}
	if strings.HasPrefix(s, "~") {
		home, _ := os.UserHomeDir()
		s = filepath.Join(home, strings.TrimPrefix(s, "~"))
	}
	abs, err := filepath.Abs(s)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("нет файла: %s", abs)
	}
	return abs, nil
}

// ------------------------------------------------------------ генератор

// wpGenerate — свои обои без файлов: градиент + световое пятно.
func wpGenerate(name string, w, h int) *image.RGBA {
	pal, ok := wpPalettes[name]
	if !ok {
		pal = wpPalettes["sage"]
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	top, bot, hi := pal[0], pal[1], pal[2]
	for y := 0; y < h; y++ {
		t := float64(y) / float64(h-1)
		r := float64(top.R)*(1-t) + float64(bot.R)*t
		g := float64(top.G)*(1-t) + float64(bot.G)*t
		b := float64(top.B)*(1-t) + float64(bot.B)*t
		for x := 0; x < w; x++ {
			// световое пятно в верхней трети
			dx := (float64(x)/float64(w) - 0.32) * 1.9
			dy := (float64(y)/float64(h) - 0.22) * 1.5
			k := math.Exp(-(dx*dx + dy*dy) * 2.2)
			rr := r + (float64(hi.R)-r)*k*0.45
			gg := g + (float64(hi.G)-g)*k*0.45
			bb := b + (float64(hi.B)-b)*k*0.45
			i := img.PixOffset(x, y)
			img.Pix[i+0] = uint8(rr)
			img.Pix[i+1] = uint8(gg)
			img.Pix[i+2] = uint8(bb)
			img.Pix[i+3] = 0xFF
		}
	}
	return img
}

// wpDecode — файл в RGBA. Формат определяется по содержимому:
// PNG, JPEG и WebP (регистрируются пустыми импортами выше).
func wpDecode(spec string) (*image.RGBA, error) {
	f, err := os.Open(spec)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	src, format, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: не удалось разобрать картинку (%w)",
			filepath.Base(spec), err)
	}
	b := src.Bounds()
	if r, ok := src.(*image.RGBA); ok && b.Min == (image.Point{}) {
		return r, nil // уже то, что нужно, без копии
	}
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), src, b.Min, draw.Src)
	_ = format
	return rgba, nil
}

// wpPrepare — кадр нужного размера: генератор или cover из исходника.
// src декодируется один раз на команду, а не на каждый выход.
func wpPrepare(spec string, src *image.RGBA, w, h int) (*image.RGBA, error) {
	if strings.HasPrefix(spec, "gen:") {
		return wpGenerate(strings.TrimPrefix(spec, "gen:"), w, h), nil
	}
	if src == nil {
		return nil, fmt.Errorf("нет исходной картинки")
	}
	return wpCover(src, w, h), nil
}

// wpCover — cover-масштаб в размер w×h: без искажений, центр по центру.
func wpCover(src *image.RGBA, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	sw, sh := float64(src.Bounds().Dx()), float64(src.Bounds().Dy())
	if sw <= 0 || sh <= 0 {
		return dst
	}
	scale := math.Max(float64(w)/sw, float64(h)/sh)
	cw, ch := float64(w)/scale, float64(h)/scale
	x0 := (sw - cw) / 2
	y0 := (sh - ch) / 2
	sr := image.Rect(int(x0), int(y0), int(x0+cw), int(y0+ch))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, sr, draw.Src, nil)
	return dst
}

// wpBlendInto — смешать a и b в dst: dst = a*(1-t) + b*t.
// Целочисленно (8.8 фиксированная точка) и построчно: на 1080p это
// заметно дешевле, чем float на каждый пиксель.
func wpBlendInto(a, b *image.RGBA, t float64, dst *image.RGBA) {
	if a == nil || b == nil || a.Bounds() != dst.Bounds() || b.Bounds() != dst.Bounds() {
		if b != nil {
			draw.Draw(dst, dst.Bounds(), b, dst.Bounds().Min, draw.Src)
		}
		return
	}
	k := int(t*256 + 0.5)
	if k < 0 {
		k = 0
	} else if k > 256 {
		k = 256
	}
	inv := 256 - k
	bd := dst.Bounds()
	w, h := bd.Dx(), bd.Dy()
	for y := 0; y < h; y++ {
		ar := a.Pix[y*a.Stride:]
		br := b.Pix[y*b.Stride:]
		dr := dst.Pix[y*dst.Stride:]
		for x := 0; x < w; x++ {
			i := x * 4
			dr[i+0] = uint8((int(ar[i+0])*inv + int(br[i+0])*k) >> 8)
			dr[i+1] = uint8((int(ar[i+1])*inv + int(br[i+1])*k) >> 8)
			dr[i+2] = uint8((int(ar[i+2])*inv + int(br[i+2])*k) >> 8)
			dr[i+3] = 0xFF
		}
	}
}

// ---------------------------------------------------------------- демон

func (d *wpDaemon) tracef(f string, a ...any) {
	if d.c.trace {
		fmt.Fprintf(os.Stderr, f, a...)
	}
}

func wpRun(args []string) error {
	d := &wpDaemon{nextID: 4, byObj: map[uint32]*wpSurface{},
		outScale: map[uint32]int{}, fadeDur: 300 * time.Millisecond}
	var err error
	if d.c, err = dial(os.Getenv("LNF_TRACE") != ""); err != nil {
		return err
	}
	d.c.trace = os.Getenv("LNF_TRACE") != ""

	// глобалы
	d.send(idDisplay, 1, A{}.u32(idReg))
	d.send(idDisplay, 0, A{}.u32(idSync))
	for {
		obj, op, a, err := d.c.readMsg()
		if err != nil {
			return err
		}
		if obj == idSync && op == 0 {
			break
		}
		d.handle(obj, op, a)
	}
	if d.idCompositor == 0 || d.idShm == 0 {
		return fmt.Errorf("компоновщик без wl_compositor/wl_shm")
	}
	if d.idLayerShell == 0 {
		return fmt.Errorf("компоновщик не поддерживает wlr-layer-shell")
	}

	// по поверхности на каждый выход; если выходов ещё не знаем — одна общая
	outs := d.outputs
	if len(outs) == 0 {
		outs = []uint32{0}
	}
	for _, o := range outs {
		if err := d.addSurface(o); err != nil {
			return err
		}
	}
	d.started = true

	// один демон на сессию
	lockPath := strings.TrimSuffix(wpFIFO(), ".fifo") + ".lock"
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("lock: %w", err)
	}
	if err := unix.Flock(int(lockFile.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		lockFile.Close()
		return fmt.Errorf("демон обоев уже запущен")
	}
	d.lock = lockFile
	defer lockFile.Close()

	// FIFO управления
	fifo := wpFIFO()
	os.Remove(fifo)
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		return fmt.Errorf("fifo: %w", err)
	}
	// сырой fd: bufio/os.File здесь блокирует цикл, а нам нужен poll
	fifoFD, err := unix.Open(fifo, unix.O_RDWR|unix.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("fifo: %w", err)
	}
	defer unix.Close(fifoFD)
	defer os.Remove(fifo)

	// начальная картинка из аргументов
	if len(args) > 0 {
		spec, err := wpResolve(args[0])
		if err != nil {
			return err
		}
		if err := d.apply(spec); err != nil {
			return err
		}
	} else {
		_ = d.apply("gen:sage")
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	buf := make([]byte, 4096)
	last := time.Now()
	for {
		timeout := 200
		if d.animating() {
			timeout = 16
		}
		fds := []unix.PollFd{
			{Fd: int32(d.c.fd), Events: unix.POLLIN},
			{Fd: int32(fifoFD), Events: unix.POLLIN},
		}
		if _, err := unix.Poll(fds, timeout); err != nil && err != unix.EINTR {
			return err
		}
		// события Wayland
		for d.c.readable() {
			obj, op, a, err := d.c.readMsg()
			if err != nil {
				return err
			}
			d.handle(obj, op, a)
		}
		// команды
		if n, err := unix.Read(fifoFD, buf); err == nil && n > 0 {
			for _, line := range strings.Split(string(buf[:n]), "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				fields := strings.Fields(line)
				switch fields[0] {
				case "set":
					if len(fields) > 1 {
						spec, err := wpResolve(fields[1])
						if err == nil {
							if err := d.apply(spec); err != nil {
								fmt.Fprintf(os.Stderr, "lnf wallpaperd: %v\n", err)
							}
						} else {
							fmt.Fprintf(os.Stderr, "lnf wallpaperd: %v\n", err)
						}
					}
				case "fade":
					if len(fields) > 1 {
						if ms, err := strconv.Atoi(fields[1]); err == nil {
							d.fadeDur = time.Duration(ms) * time.Millisecond
						}
					}
				case "quit":
					return nil
				}
			}
		}
		// шаг анимации
		now := time.Now()
		dt := now.Sub(last)
		last = now
		if d.animating() {
			d.stepFade(dt)
		}
		// Переход закончился — возвращаем ОС промежуточные кадры.
		// Один раз на смену обоев, не в горячем цикле.
		if d.needReap && !d.animating() {
			d.needReap = false
			debug.FreeOSMemory()
		}
		select {
		case <-sig:
			return nil
		default:
		}
	}
}

func (d *wpDaemon) send(obj, op uint32, a A) { d.c.send(obj, op, a) }

func (d *wpDaemon) newID() uint32 {
	id := d.nextID
	d.nextID++
	return id
}

func (d *wpDaemon) bind(name uint32, iface string, ver uint32) {
	lo := func(a, b uint32) uint32 {
		if a < b {
			return a
		}
		return b
	}
	switch iface {
	case "wl_compositor":
		d.idCompositor = d.newID()
		d.send(idReg, 0, A{}.u32(name).str(iface).u32(lo(ver, 4)).u32(d.idCompositor))
	case "wl_shm":
		d.idShm = d.newID()
		d.send(idReg, 0, A{}.u32(name).str(iface).u32(1).u32(d.idShm))
	case "zwlr_layer_shell_v1":
		d.idLayerShell = d.newID()
		d.send(idReg, 0, A{}.u32(name).str(iface).u32(lo(ver, 4)).u32(d.idLayerShell))
	case "wl_output":
		id := d.newID()
		// v2 уже присылает scale — он нужен для HiDPI
		d.send(idReg, 0, A{}.u32(name).str(iface).u32(lo(ver, 2)).u32(id))
		d.outputs = append(d.outputs, id)
		d.outScale[id] = 1
		d.tracef("lnf wallpaperd: output %s id=%d\n", iface, id)
		if d.started {
			// монитор подключили на ходу — сразу вешаем на него обои
			if err := d.addSurface(id); err != nil {
				fmt.Fprintf(os.Stderr, "lnf wallpaperd: hotplug %d: %v\n", id, err)
			}
		}
	}
}

func (d *wpDaemon) handle(obj, op uint32, args []byte) {
	r := &rd{b: args}
	switch obj {
	case idDisplay:
		return
	case idReg:
		if op == 0 {
			name := r.u32()
			iface := r.str()
			ver := r.u32()
			d.bind(name, iface, ver)
		}
		return
	}
	s := d.byObj[obj]
	if s == nil {
		if d.isOutput(obj) {
			if op == 3 { // wl_output.scale
				d.setOutputScale(obj, int(int32(r.u32())))
			}
		}
		return
	}
	switch obj {
	case s.idLayer:
		switch op {
		case 0: // configure(serial, w, h)
			serial := r.u32()
			w, h := int(r.u32()), int(r.u32())
			d.send(s.idLayer, 6, A{}.u32(serial)) // ack_configure
			s.configured = true
			if w <= 0 || h <= 0 {
				return
			}
			rebuild := false
			if s.idPool == 0 || w != s.W || h != s.H {
				s.W, s.H = w, h
				if err := d.makeBuffers(s); err != nil {
					fmt.Fprintf(os.Stderr, "lnf wallpaperd: %v\n", err)
					return
				}
				rebuild = true
			}
			if rebuild {
				// размер сменился (или это первый configure): кадры из
				// старого буфера больше не годятся
				s.from, s.to, s.next, s.fading = nil, nil, nil, false
				if err := d.reload(s); err != nil {
					d.tracef("lnf wallpaperd: %v\n", err)
				}
				d.repaint(s)
			}
		case 1: // closed
			s.configured = false
		}
		return
	}
	if op == 0 { // buffer.release
		for _, b := range s.bufs {
			if b != nil && b.id == obj {
				b.busy = false
			}
		}
		if s.dirty && !s.fading {
			d.repaint(s)
		}
	}
}

func (d *wpDaemon) addSurface(output uint32) error {
	s := &wpSurface{output: output, scale: 1, lastSpec: d.spec}
	if v := d.outScale[output]; v > 0 {
		s.scale = v
	}
	s.idSurface = d.newID()
	d.send(d.idCompositor, 0, A{}.u32(s.idSurface))
	s.idLayer = d.newID()
	d.send(d.idLayerShell, 0, A{}.u32(s.idLayer).u32(s.idSurface).u32(output).
		u32(layerID("background")).str("lnf-wallpaper"))
	d.send(s.idLayer, 0, A{}.u32(0).u32(0)) // set_size: решает компоновщик
	d.send(s.idLayer, 1, A{}.u32(15))       // anchor: все четыре края
	d.send(s.idLayer, 2, A{}.i32(-1))       // exclusive_zone: не занимать место
	d.send(s.idLayer, 4, A{}.u32(0))        // keyboard_interactivity: none
	d.send(s.idSurface, 6, nil)             // commit
	d.tracef("lnf wallpaperd: surface=%d layer=%d output=%d\n", s.idSurface, s.idLayer, output)
	d.byObj[s.idSurface] = s
	d.byObj[s.idLayer] = s
	d.surfs = append(d.surfs, s)
	return nil
}

func (d *wpDaemon) makeBuffers(s *wpSurface) error {
	if s.W <= 0 || s.H <= 0 {
		return nil
	}
	if s.scale < 1 {
		s.scale = 1
	}
	// Буфер — в физических пикселях: на scale=2 это 2W x 2H, ровно столько,
	// сколько просит компоновщик, и никакого мыла из апскейла.
	bw, bh := s.W*s.scale, s.H*s.scale
	if s.idPool != 0 {
		for _, b := range s.bufs {
			if b != nil {
				d.send(b.id, 0, nil)
				delete(d.byObj, b.id)
			}
		}
		d.send(s.idPool, 1, nil)
		delete(d.byObj, s.idPool)
		if s.bufs[0] != nil {
			unix.Munmap(s.bufs[0].data[:cap(s.bufs[0].data)])
		}
		unix.Close(s.memfd)
		s.bufs = [2]*wpBuf{}
		s.idPool = 0
	}

	sz := bw * 4 * bh
	fd, err := unix.MemfdCreate("lnf-wallpaper", unix.MFD_CLOEXEC)
	if err != nil {
		return err
	}
	if err := unix.Ftruncate(fd, int64(2*sz)); err != nil {
		unix.Close(fd)
		return err
	}
	data, err := unix.Mmap(fd, 0, 2*sz, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		unix.Close(fd)
		return err
	}
	s.memfd = fd
	s.idPool = d.newID()
	d.c.sendFD(d.idShm, 0, A{}.u32(s.idPool).i32(int32(2*sz)), fd)

	for i := 0; i < 2; i++ {
		b := &wpBuf{id: d.newID(), data: data[i*sz : (i+1)*sz]}
		b.img = &image.RGBA{Pix: b.data[:sz], Stride: bw * 4,
			Rect: image.Rect(0, 0, bw, bh)}
		d.send(s.idPool, 0, A{}.u32(b.id).i32(int32(i*sz)).i32(int32(bw)).
			i32(int32(bh)).i32(int32(bw*4)).u32(shmABGR8888))
		s.bufs[i] = b
		d.byObj[b.id] = s
	}
	s.bufW, s.bufH = bw, bh

	// buffer_scale говорит компоновщику, что буфер в scale раз плотнее
	// логического размера поверхности.
	if !s.bufScaleSent || s.scale != s.sentScale {
		d.send(s.idSurface, 8, A{}.i32(int32(s.scale)))
		s.bufScaleSent = true
		s.sentScale = s.scale
	}
	return nil
}

// paint копирует кадр в свободный буфер и отдаёт компоновщику.
// isOutput — это объект wl_output, а не поверхность.
func (d *wpDaemon) isOutput(obj uint32) bool {
	for _, o := range d.outputs {
		if o == obj {
			return true
		}
	}
	return false
}

// setOutputScale — выход сменил масштаб (HiDPI): пересобираем буферы
// в физических пикселях и перерисовываем без перезапуска.
func (d *wpDaemon) setOutputScale(output uint32, sc int) {
	if sc < 1 {
		sc = 1
	}
	d.outScale[output] = sc
	for _, s := range d.surfs {
		if s.output != output || s.scale == sc {
			continue
		}
		s.scale = sc
		if s.W <= 0 || s.H <= 0 {
			continue
		}
		if err := d.makeBuffers(s); err != nil {
			fmt.Fprintf(os.Stderr, "lnf wallpaperd: %v\n", err)
			return
		}
		if err := d.reload(s); err != nil {
			d.tracef("lnf wallpaperd: %v\n", err)
		}
		d.repaint(s)
	}
}

// freeBuf — буфер для следующего кадра. Строго другой, чем прошлый:
// компоновщик кэширует текстуру wl_buffer и не заметит, что содержимое
// того же буфера изменилось. Если второй ещё занят — кадр пропускаем.
func (s *wpSurface) freeBuf() *wpBuf {
	idx := 1 - s.lastBuf
	if b := s.bufs[idx]; b != nil && !b.busy {
		s.lastBuf = idx
		return b
	}
	return nil
}

// paint — скопировать готовый кадр в свободный буфер и показать его.
// Возвращает false, если оба буфера заняты компоновщиком.
func (d *wpDaemon) paint(s *wpSurface, frame *image.RGBA) bool {
	if frame == nil || s.bufW <= 0 || s.bufH <= 0 {
		return false
	}
	b := s.freeBuf()
	if b == nil {
		s.dirty = true // оба заняты — повторим на buffer.release
		return false
	}
	draw.Draw(b.img, b.img.Bounds(), frame, frame.Bounds().Min, draw.Src)
	d.commit(s, b)
	return true
}

// paintBlend — кадр перехода: смешиваем прямо в shm, без image.NewRGBA
// на каждый тик. На 1080p это ~8 МБ аллокаций 60 раз в секунду меньше.
func (d *wpDaemon) paintBlend(s *wpSurface, a, nxt *image.RGBA, t float64) {
	if nxt == nil || s.bufW <= 0 || s.bufH <= 0 {
		return
	}
	b := s.freeBuf()
	if b == nil {
		s.dirty = true
		return
	}
	wpBlendInto(a, nxt, t, b.img)
	d.commit(s, b)
}

func (d *wpDaemon) commit(s *wpSurface, b *wpBuf) {
	b.busy = true
	s.shown = true
	s.dirty = false
	d.send(s.idSurface, 1, A{}.u32(b.id).i32(0).i32(0)) // attach
	d.send(s.idSurface, 2, A{}.i32(0).i32(0).
		i32(int32(s.bufW)).i32(int32(s.bufH))) // damage
	d.send(s.idSurface, 6, nil) // commit
}

// repaint — перерисовать статичный кадр. После успешной отрисовки копия
// кадра в памяти не нужна: он уже лежит в shm-буфере.
func (d *wpDaemon) repaint(s *wpSurface) {
	if s.to == nil || s.fading {
		return
	}
	if d.paint(s, s.to) {
		// статичный кадр теперь только в shm — копии в куче не нужны
		s.from, s.to, s.next = nil, nil, nil
		s.trim()
	}
}

// trim — отдать ядру страницы второго буфера: в простое он не нужен,
// а при следующем переходе страницы поднимутся из memfd сами.
// Трогаем только буфер с выровненным размером, иначе заденем соседний.
func (s *wpSurface) trim() {
	for i, b := range s.bufs {
		if i == s.lastBuf || b == nil || b.busy || len(b.data) == 0 {
			continue
		}
		if len(b.data)%os.Getpagesize() != 0 {
			continue
		}
		if uintptr(unsafe.Pointer(&b.data[0]))%uintptr(os.Getpagesize()) != 0 {
			continue
		}
		_ = unix.Madvise(b.data, unix.MADV_DONTNEED)
	}
}

func wpClone(src *image.RGBA) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, src.Bounds().Dx(), src.Bounds().Dy()))
	draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Src)
	return dst
}

// apply: загрузить картинку и запустить переход.
// source — декодированный исходник. Вызывается один раз на команду, а не
// на каждый выход; в памяти не держим — на 1080p это лишние 8 МБ.
func (d *wpDaemon) source(spec string) (*image.RGBA, error) {
	if strings.HasPrefix(spec, "gen:") {
		return nil, nil
	}
	return wpDecode(spec)
}

// reload — пересобрать кадр поверхности из lastSpec под текущий размер.
func (d *wpDaemon) reload(s *wpSurface) error {
	if s.lastSpec == "" || s.bufW <= 0 || s.bufH <= 0 {
		return nil
	}
	src, err := d.source(s.lastSpec)
	if err != nil {
		return err
	}
	img, err := wpPrepare(s.lastSpec, src, s.bufW, s.bufH)
	if err != nil {
		return err
	}
	s.from, s.to, s.next, s.fading = img, img, nil, false
	s.dirty = false
	d.needReap = true
	return nil
}

func (d *wpDaemon) apply(spec string) error {
	// декодируем один раз — даже если поверхностей несколько
	src, err := d.source(spec)
	if err != nil {
		return err
	}
	d.needReap = true
	d.spec = spec
	ready := 0
	for _, s := range d.surfs {
		s.lastSpec = spec
		if s.bufW <= 0 || s.bufH <= 0 {
			continue // ещё не было configure; reload случится в нём
		}
		img, err := wpPrepare(spec, src, s.bufW, s.bufH)
		if err != nil {
			return err
		}
		ready++
		// откуда начинать переход
		var from *image.RGBA
		switch {
		case s.to != nil:
			from = s.to // кадр ещё не показан (первый configure)
		case s.shown && s.bufs[s.lastBuf] != nil:
			// старый кадр уже в буфере — снимаем с него копию, чтобы
			// не держать вторую копию в памяти постоянно
			from = wpClone(s.bufs[s.lastBuf].img)
		}
		if from == nil || d.fadeDur <= 0 || !s.shown {
			s.from, s.to, s.next = img, img, nil
			s.fading = false
			d.repaint(s)
			continue
		}
		s.from, s.to, s.next = from, img, img
		s.t0 = time.Now()
		s.fading = true
	}
	if ready == 0 {
		if len(d.surfs) == 0 {
			return fmt.Errorf("нет поверхностей: компоновщик не дал выходов")
		}
		// configure ещё не пришёл — кадр соберётся в обработчике configure
		return nil
	}
	return nil
}

func (d *wpDaemon) animating() bool {
	for _, s := range d.surfs {
		if s.fading {
			return true
		}
	}
	return false
}

// stepFade — один кадр перехода.
func (d *wpDaemon) stepFade(dt time.Duration) {
	for _, s := range d.surfs {
		if !s.fading || s.next == nil || s.bufW <= 0 {
			continue
		}
		t := 1.0
		if d.fadeDur > 0 {
			t = float64(time.Since(s.t0)) / float64(d.fadeDur)
		}
		if t >= 1 {
			// финальный кадр: from/next отпускаем, кадр остаётся в буфере
			s.from, s.to, s.next = nil, s.next, nil
			s.fading = false
			d.repaint(s)
			continue
		}
		if s.from == nil {
			s.from = s.to
		}
		d.paintBlend(s, s.from, s.next, t)
	}
}

// отладка
func (d *wpDaemon) dbg(f string, a ...any) {
	if os.Getenv("LNF_WP_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "wp: "+f+"\n", a...)
	}
}
