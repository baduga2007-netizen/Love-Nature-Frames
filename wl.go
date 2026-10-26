// wl.go — бэкенд Wayland: подписка на wlr-layer-shell, разделяемая память,
// ввод с указателя и вывод кадров. Всё «грязное» — руками, без cgo.
//
// На одно соединение с компоновщиком — сколько угодно поверхностей слоя:
// каждая секция конфига даёт свою поверхность, свою сцену и свои два
// буфера в разделяемой памяти. События указателя разводятся по поверхности,
// над которой он сейчас находится.
package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"os"
	"os/signal"
	"path"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// ============================================================
// Протокол: провод
// ============================================================

const (
	idDisplay = 1
	idReg     = 2
	idSync    = 3
)

// A — сборщик аргументов сообщения
type A []byte

func (a A) u32(v uint32) A { return binary.LittleEndian.AppendUint32(a, v) }
func (a A) i32(v int32) A  { return a.u32(uint32(v)) }
func (a A) str(s string) A {
	a = a.u32(uint32(len(s) + 1))
	a = append(a, s...)
	a = append(a, 0)
	for len(a)%4 != 0 {
		a = append(a, 0)
	}
	return a
}

type conn struct {
	fd      int
	trace   bool
	fds     []int // дескрипторы, пришедшие с текущим сообщением (SCM_RIGHTS)
	lastFds []int // дескрипторы последнего прочитанного сообщения
}

func dial(trace bool) (*conn, error) {
	name := os.Getenv("WAYLAND_DISPLAY")
	if name == "" {
		name = "wayland-0"
	}
	if name[0] != '/' {
		name = os.Getenv("XDG_RUNTIME_DIR") + "/" + name
	}
	fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	if err := unix.Connect(fd, &unix.SockaddrUnix{Name: name}); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("connect %s: %w", name, err)
	}
	return &conn{fd: fd, trace: trace}, nil
}

func (c *conn) send(obj, opcode uint32, args A) {
	if c.trace {
		fmt.Fprintf(os.Stderr, "-> obj=%d op=%d len=%d\n", obj, opcode, len(args))
	}
	c.write(obj, opcode, args, -1)
}

func (c *conn) sendFD(obj, opcode uint32, args A, fd int) {
	if c.trace {
		fmt.Fprintf(os.Stderr, "-> obj=%d op=%d len=%d fd=%d\n", obj, opcode, len(args), fd)
	}
	c.write(obj, opcode, args, fd)
}

func (c *conn) write(obj, opcode uint32, args A, fd int) {
	size := uint32(8 + (len(args)+3)&^3)
	var buf []byte
	buf = binary.LittleEndian.AppendUint32(buf, obj)
	buf = binary.LittleEndian.AppendUint32(buf, size<<16|opcode)
	buf = append(buf, args...)
	for len(buf)%4 != 0 {
		buf = append(buf, 0)
	}
	var oob []byte
	if fd >= 0 {
		oob = unix.UnixRights(fd) // дескрипторы уходят отдельно, через SCM_RIGHTS
	}
	for len(buf) > 0 {
		n, err := unix.SendmsgN(c.fd, buf, oob, nil, 0)
		if err != nil {
			die("sendmsg: %v", err)
		}
		buf, oob = buf[n:], nil
	}
}

func (c *conn) readFull(n int) ([]byte, error) {
	b := make([]byte, n)
	oob := make([]byte, 256)
	off := 0
	for off < n {
		k, oobn, _, _, err := unix.Recvmsg(c.fd, b[off:], oob, 0)
		if err != nil {
			return nil, err
		}
		if k == 0 {
			return nil, errors.New("компоновщик закрыл соединение")
		}
		if oobn > 0 { // забираем переданные дескрипторы (например, раскладку)
			if cms, err := unix.ParseSocketControlMessage(oob[:oobn]); err == nil {
				for i := range cms {
					if fds, err := unix.ParseUnixRights(&cms[i]); err == nil {
						c.fds = append(c.fds, fds...)
					}
				}
			}
		}
		off += k
	}
	return b, nil
}

// closeFds закрывает дескрипторы последнего сообщения (вызывать после handle).
func (c *conn) closeFds() {
	for _, fd := range c.lastFds {
		unix.Close(fd)
	}
	c.lastFds = nil
}

// readable — есть ли в сокете что читать (сам сокет блокирующий)
func (c *conn) readable() bool {
	fds := []unix.PollFd{{Fd: int32(c.fd), Events: unix.POLLIN}}
	n, err := unix.Poll(fds, 0)
	return err == nil && n > 0
}

func (c *conn) readMsg() (obj, opcode uint32, args []byte, err error) {
	c.fds = nil
	hdr, err := c.readFull(8)
	if err != nil {
		return 0, 0, nil, err
	}
	obj = binary.LittleEndian.Uint32(hdr[0:4])
	word := binary.LittleEndian.Uint32(hdr[4:8])
	size := int(word >> 16)
	opcode = word & 0xffff
	if size < 8 {
		return 0, 0, nil, fmt.Errorf("битый размер сообщения: %d", size)
	}
	if size > 8 {
		if args, err = c.readFull(size - 8); err != nil {
			return 0, 0, nil, err
		}
	}
	c.lastFds = c.fds
	c.fds = nil
	if c.trace {
		fmt.Fprintf(os.Stderr, "<- obj=%d op=%d len=%d\n", obj, opcode, len(args))
	}
	return obj, opcode, args, nil
}

func die(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "lnf: "+f+"\n", a...)
	os.Exit(1)
}

// klog — диагностика ввода (включается LNF_KEYS=1)
var keysDebug = os.Getenv("LNF_KEYS") != ""

func klog(f string, a ...any) {
	if keysDebug {
		fmt.Fprintf(os.Stderr, "lnf keys: "+f+"\n", a...)
	}
}

// rd — разбор аргументов события
type rd struct {
	b []byte
	i int
}

func (r *rd) u32() uint32 { v := binary.LittleEndian.Uint32(r.b[r.i:]); r.i += 4; return v }
func (r *rd) fixed() float64 {
	return float64(int32(r.u32())) / 256
}
func (r *rd) str() string {
	n := int(r.u32())
	s := string(r.b[r.i : r.i+n-1])
	r.i += (n + 3) &^ 3
	return s
}

// ============================================================
// Бэкенд
// ============================================================

const (
	wlLayerBackground = 0
	wlLayerBottom     = 1
	wlLayerTop        = 2
	wlLayerOverlay    = 3

	// WL_SHM_FORMAT_ABGR8888: в памяти байты R,G,B,A — ровно как image.RGBA.
	// Благодаря этому рисуем прямо в разделяемую память, без копии кадра.
	shmABGR8888 = 0x34324241
)

type shmBuf struct {
	id   uint32
	data []byte
	img  *image.RGBA
	busy bool
}

// surface — одна поверхность слоя: своя сцена и свои буферы
type surface struct {
	cfg   *Config
	scene *Scene

	idSurface, idLayerSurface uint32
	idPool                    uint32
	bufs                      [2]*shmBuf
	memfd                     int

	W, H       int // размер буферов
	bufW, bufH int // размер, под который построены буферы

	configured   bool
	needDraw     bool
	animating    bool
	chain        bool // просить ли следующий кадр (идёт анимация)
	pendingFrame uint32
	frameArrived bool

	idRegion  uint32 // область ввода: только видимые слои
	regionSig uint64

	kbMode int // текущая интерактивность клавиатуры слоя
}

var (
	stats                                       bool
	statsIter, statsEINTR, statsDraw, statsWake atomic.Int64
)

type backend struct {
	surfaces []*surface
	byObj    map[uint32]*surface // любой объект поверхности -> сама поверхность

	c *conn

	nextID                                               uint32
	idCompositor, idShm, idLayerShell, idSeat, idPointer uint32
	idKeyboard                                           uint32

	pointerSurf *surface // над чем сейчас курсор
	kbSurf      *surface // у кого сейчас клавиатура

	km      *keymap // раскладка из wl_keyboard.keymap
	kbGroup int     // активная группа раскладки

	cmdFD int // FIFO управления (lnf.sh)

	wakeR, wakeW int
	wakeBuf      []byte
}

func run(cfgs []*Config) error {
	signal.Ignore(syscall.SIGPIPE)
	stats = os.Getenv("LNF_STATS") != ""

	b := &backend{
		nextID:  3, // 1 — wl_display, 2 — wl_registry, 3 — callback синхронизации
		byObj:   map[uint32]*surface{},
		wakeBuf: make([]byte, 64),
		cmdFD:   -1,
	}
	wr, ww, err := unixPipe()
	if err != nil {
		return err
	}
	b.wakeR, b.wakeW = wr, ww

	if b.c, err = dial(os.Getenv("LNF_TRACE") != ""); err != nil {
		return err
	}
	if b.c.trace {
		fmt.Fprintln(os.Stderr, "lnf: подключились к компоновщику")
	}

	// 1) глобальные объекты
	b.send(idDisplay, 1, A{}.u32(idReg))  // get_registry
	b.send(idDisplay, 0, A{}.u32(idSync)) // sync
	if err := b.drain(idSync); err != nil {
		return err
	}
	if b.idCompositor == 0 || b.idShm == 0 {
		return errors.New("компоновщик без wl_compositor/wl_shm")
	}
	if b.idLayerShell == 0 {
		return errors.New("компоновщик не поддерживает wlr-layer-shell (нужен niri/sway/hyprland и т.п.)")
	}
	if b.c.trace {
		fmt.Fprintf(os.Stderr, "lnf: compositor=%d shm=%d layer-shell=%d seat=%d\n",
			b.idCompositor, b.idShm, b.idLayerShell, b.idSeat)
	}

	// 2) поверхность на каждую сцену конфига
	for _, cfg := range cfgs {
		s, err := b.addSurface(cfg)
		if err != nil {
			return err
		}
		b.surfaces = append(b.surfaces, s)
	}

	// 3) логика виджетов: опросы команд и часы
	for _, s := range b.surfaces {
		// события из горутин (опросы, часы, готовый список меню) требуют
		// не только пробуждения цикла, но и перерисовки
		s.scene.OnChange = func() { s.needDraw = true; b.wake() }
		s.scene.OnWake = func() { b.wake() }
		// клик по кнопке-раскрывашке переключает группу на всех поверхностях
		s.scene.OnGroup = func(name string, mode int) { b.group(name, mode) }
		s.scene.StartLogic()
	}

	// LNF_SPIN_ONCE — отладка: сразу завести все кнопки с click.spin,
	// чтобы посмотреть на анимацию, не щёлкая мышью
	if os.Getenv("LNF_SPIN_ONCE") != "" {
		for _, s := range b.surfaces {
			for _, l := range s.cfg.Layers {
				if l.SpinDur > 0 {
					l.spinOn, l.spinT = true, 0
				}
			}
		}
	}

	// LNF_MOUSE=x,y — отладка: будто курсор стоит в этой точке поверхности
	if v := os.Getenv("LNF_MOUSE"); v != "" {
		var x, y float64
		if _, err := fmt.Sscanf(v, "%f,%f", &x, &y); err == nil {
			for _, s := range b.surfaces {
				s.scene.Move(x, y)
				s.needDraw = true
			}
		}
	}

	// 4) цикл: ждём либо компоновщик, либо толчок из горутин виджета
	if stats {
		go func() {
			for range time.Tick(time.Second) {
				fmt.Fprintf(os.Stderr, "lnf: итераций=%d EINTR=%d кадров=%d будит=%d",
					statsIter.Load(), statsEINTR.Load(), statsDraw.Load(), statsWake.Load())
				for _, s := range b.surfaces {
					for _, l := range s.cfg.Layers {
						if l.why != "" {
							fmt.Fprintf(os.Stderr, " %s.%s:%s", s.cfg.Namespace, l.Name, l.why)
						}
					}
				}
				var ms runtime.MemStats
				runtime.ReadMemStats(&ms)
				fmt.Fprintf(os.Stderr, " | куча=%dК зарезервировано=%dК сборок=%d\n",
					ms.HeapAlloc/1024, ms.HeapSys/1024, ms.NumGC)
			}
		}()
	}
	// FIFO управления: `lnf.sh` шлёт панели команды (открыть ящик, сообщение)
	if fd, err := setupFIFO(); err == nil {
		b.cmdFD = fd
	} else if b.c.trace {
		fmt.Fprintf(os.Stderr, "lnf: FIFO управления: %v\n", err)
	}
	pfds := []unix.PollFd{
		{Fd: int32(b.c.fd), Events: unix.POLLIN},
		{Fd: int32(b.wakeR), Events: unix.POLLIN},
		{Fd: int32(b.cmdFD), Events: unix.POLLIN},
	}
	for {
		if _, err := unix.Poll(pfds, -1); err != nil {
			if err == unix.EINTR {
				if stats {
					statsEINTR.Add(1)
				}
				continue
			}
			return err
		}
		if stats {
			statsIter.Add(1)
		}
		if pfds[0].Revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR) != 0 {
			for b.c.readable() {
				obj, op, args, err := b.c.readMsg()
				if err != nil {
					return err
				}
				if err := b.handle(obj, op, args); err != nil {
					return err
				}
				b.c.closeFds()
			}
		}
		if pfds[1].Revents != 0 {
			b.drainWake()
		}
		if pfds[2].Revents&unix.POLLIN != 0 {
			b.readCommands()
		}
		now := time.Now()
		for _, s := range b.surfaces {
			// клавиатура: пока открыт ящик приложений — exclusive (1), иначе не берём
			if s.cfg.Keyboard > 0 {
				want := 0
				if s.scene.groups["apps"] {
					want = 1
				}
				if s.kbMode != want {
					s.kbMode = want
					klog("интерактивность клавиатуры -> %d", want)
					b.send(s.idLayerSurface, 4, A{}.u32(uint32(want)))
					s.needDraw = true
				}
			}
			if s.scene.RefreshText() {
				s.needDraw = true
			}
			if !s.configured || s.pendingFrame != 0 {
				continue
			}
			if !s.frameArrived && !s.needDraw && !s.animating {
				continue
			}
			s.frameArrived = false
			s.animating = s.scene.Update(now)
			s.chain = s.animating
			if err := b.draw(s); err != nil {
				return err
			}
		}
	}
}

func (b *backend) wake() {
	if stats {
		statsWake.Add(1)
	}
	unix.Write(b.wakeW, []byte{1}) // канал неблокирующий, EAGAIN игнорируем
}

// unixPipe — канал без os.File: иначе финализатор закроет дескрипторы,
// как только объекты соберёт сборщик мусора.
func unixPipe() (r, w int, err error) {
	fds := make([]int, 2)
	if err := unix.Pipe2(fds, unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		return 0, 0, err
	}
	return fds[0], fds[1], nil
}

func (b *backend) drainWake() {
	for {
		if _, err := unix.Read(b.wakeR, b.wakeBuf); err != nil {
			return
		}
	}
}

func (b *backend) newID() uint32 {
	b.nextID++
	return b.nextID
}

func (b *backend) send(obj, op uint32, args A) { b.c.send(obj, op, args) }

func layerID(name string) uint32 {
	switch name {
	case "bottom":
		return wlLayerBottom
	case "top":
		return wlLayerTop
	case "overlay":
		return wlLayerOverlay
	default:
		return wlLayerBackground
	}
}

// drain читает сообщения, пока не придёт wl_callback.done для id
func (b *backend) drain(id uint32) error {
	for {
		obj, op, args, err := b.c.readMsg()
		if err != nil {
			return err
		}
		if obj == id && op == 0 {
			b.c.closeFds()
			return nil
		}
		if err := b.handle(obj, op, args); err != nil {
			return err
		}
		b.c.closeFds()
	}
}

// ============================================================
// Поверхности и разделяемая память
// ============================================================

func (b *backend) addSurface(cfg *Config) (*surface, error) {
	s := &surface{
		cfg: cfg, scene: NewScene(cfg),
		W: int(cfg.W), H: int(cfg.H), needDraw: true,
	}
	if err := b.makeBuffers(s); err != nil {
		return nil, err
	}

	s.idSurface = b.newID()
	b.send(b.idCompositor, 0, A{}.u32(s.idSurface)) // create_surface
	s.idLayerSurface = b.newID()
	b.send(b.idLayerShell, 0, A{}.u32(s.idLayerSurface).u32(s.idSurface).u32(0).
		u32(layerID(cfg.Layer)).str(cfg.Namespace)) // get_layer_surface

	b.send(s.idLayerSurface, 0, A{}.u32(uint32(s.W)).u32(uint32(s.H))) // set_size
	b.send(s.idLayerSurface, 1, A{}.u32(cfg.Anchor))                   // set_anchor
	b.send(s.idLayerSurface, 2, A{}.i32(cfg.Exclusive))                // set_exclusive_zone
	b.send(s.idLayerSurface, 3, A{}.i32(cfg.Margin[0]).i32(cfg.Margin[1]).
		i32(cfg.Margin[2]).i32(cfg.Margin[3])) // set_margin
	b.send(s.idLayerSurface, 4, A{}.u32(0)) // set_keyboard_interactivity: none, возьмём exclusive при поиске
	b.send(s.idSurface, 6, nil)             // commit

	b.byObj[s.idSurface] = s
	b.byObj[s.idLayerSurface] = s
	return s, nil
}

func (b *backend) makeBuffers(s *surface) error {
	if s.W <= 0 || s.H <= 0 {
		return nil // размер ещё не знаем — ждём configure
	}
	if s.idPool != 0 {
		for _, buf := range s.bufs {
			if buf != nil {
				b.send(buf.id, 0, nil) // buffer.destroy
				delete(b.byObj, buf.id)
			}
		}
		b.send(s.idPool, 1, nil) // pool.destroy
		delete(b.byObj, s.idPool)
		if s.bufs[0] != nil {
			unix.Munmap(s.bufs[0].data[:cap(s.bufs[0].data)])
		}
		unix.Close(s.memfd)
	}

	stride := 4
	sz := s.W * stride * s.H
	fd, err := unix.MemfdCreate("lnf", unix.MFD_CLOEXEC)
	if err != nil {
		return fmt.Errorf("memfd: %w", err)
	}
	if err := unix.Ftruncate(fd, int64(2*sz)); err != nil {
		return fmt.Errorf("ftruncate: %w", err)
	}
	data, err := unix.Mmap(fd, 0, 2*sz, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return fmt.Errorf("mmap: %w", err)
	}
	s.memfd = fd

	s.idPool = b.newID()
	// create_pool(id, size) + fd через SCM_RIGHTS
	b.c.sendFD(b.idShm, 0, A{}.u32(s.idPool).i32(int32(2*sz)), fd)

	for i := 0; i < 2; i++ {
		buf := &shmBuf{
			id:   b.newID(),
			data: data[i*sz : (i+1)*sz],
		}
		// image.RGBA поверх той же памяти: Render пишет сразу в wl_shm.
		buf.img = &image.RGBA{
			Pix:    buf.data[:sz],
			Stride: s.W * stride,
			Rect:   image.Rect(0, 0, s.W, s.H),
		}
		// create_buffer(id, offset, w, h, stride, format)
		b.send(s.idPool, 0, A{}.u32(buf.id).i32(int32(i*sz)).i32(int32(s.W)).
			i32(int32(s.H)).i32(int32(s.W*stride)).u32(shmABGR8888))
		s.bufs[i] = buf
		b.byObj[buf.id] = s
	}
	s.bufW, s.bufH = s.W, s.H
	return nil
}

// draw отдаёт очередной кадр компоновщику
func (b *backend) draw(s *surface) error {
	var buf *shmBuf
	for _, c := range s.bufs {
		if c != nil && !c.busy {
			buf = c
			break
		}
	}
	if buf == nil {
		s.needDraw = true // все буферы заняты — попробуем на следующем событии
		return nil
	}
	buf.busy = true
	s.needDraw = false
	if stats {
		statsDraw.Add(1)
	}

	s.scene.Render(buf.img, float64(s.W), float64(s.H))
	b.setInputRegion(s)

	// кадровый callback просим ДО commit: иначе компоновщик будет ждать
	// следующего кадра, которого без callback'а не случится. И только если
	// анимация продолжается — иначе спим до следующего события.
	if s.chain && s.pendingFrame == 0 {
		s.pendingFrame = b.newID()
		b.byObj[s.pendingFrame] = s
		b.send(s.idSurface, 3, A{}.u32(s.pendingFrame)) // frame
	}
	b.send(s.idSurface, 1, A{}.u32(buf.id).i32(0).i32(0)) // attach
	b.send(s.idSurface, 2, A{}.i32(0).i32(0).i32(int32(s.W)).i32(int32(s.H)))
	b.send(s.idSurface, 6, nil) // commit
	return nil
}

// setInputRegion сужает область ввода поверхности до видимых слоёв: где панель
// прозрачна, клики уходят в окна под ней. Регион пересоздаётся только при смене
// прямоугольников. Пустой регион = поверхность мышь не ловит вовсе.
func (b *backend) setInputRegion(s *surface) {
	rects := s.scene.InputRects()
	h := uint64(1469598103934665603)
	for _, r := range rects {
		for _, v := range r {
			h ^= uint64(uint32(v))
			h *= 1099511628211
		}
	}
	if h == s.regionSig && s.idRegion != 0 {
		return
	}
	s.regionSig = h
	if s.idRegion != 0 {
		b.send(s.idRegion, 0, nil) // region.destroy
		s.idRegion = 0
	}
	s.idRegion = b.newID()
	b.send(b.idCompositor, 1, A{}.u32(s.idRegion)) // create_region
	for _, r := range rects {
		b.send(s.idRegion, 1, A{}.i32(int32(r[0])).i32(int32(r[1])).i32(int32(r[2])).i32(int32(r[3]))) // region.add
	}
	b.send(s.idSurface, 5, A{}.u32(s.idRegion)) // set_input_region
}

// image.RGBA (R,G,B,A) -> wl_shm ABGR8888: порядок байтов совпадает,
// поэтому отдельная конвертация не нужна (см. makeBuffers).

// ============================================================
// Обработка событий
// ============================================================

func (b *backend) handle(obj, op uint32, args []byte) error {
	r := &rd{b: args}
	switch obj {
	case idDisplay:
		switch op {
		case 0: // error
			r.u32()
			code := r.u32()
			return fmt.Errorf("компоновщик вернул ошибку %d: %s", code, r.str())
		}
		return nil
	case idReg:
		if op == 0 { // global
			name := r.u32()
			iface := r.str()
			ver := r.u32()
			b.bind(name, iface, ver)
		}
		return nil
	case b.idSeat:
		if op == 0 { // capabilities
			caps := r.u32()
			klog("seat capabilities=%d", caps)
			if caps&1 != 0 && b.idPointer == 0 { // указатель
				b.idPointer = b.newID()
				b.send(b.idSeat, 0, A{}.u32(b.idPointer))
			}
			if caps&2 != 0 && b.idKeyboard == 0 { // клавиатура
				b.idKeyboard = b.newID()
				b.send(b.idSeat, 1, A{}.u32(b.idKeyboard))
			}
		}
		return nil
	case b.idKeyboard:
		b.keyboard(r, op)
		return nil
	case b.idPointer:
		b.pointer(r, op)
		return nil
	}

	s := b.byObj[obj]
	if s == nil {
		return nil
	}
	switch {
	case obj == s.idLayerSurface:
		switch op {
		case 0: // configure
			serial := r.u32()
			w, h := int(r.u32()), int(r.u32())
			b.send(s.idLayerSurface, 6, A{}.u32(serial)) // ack_configure
			if w > 0 && h > 0 {
				s.W, s.H = w, h
			}
			s.scene.Resize(float64(s.W), float64(s.H))
			if s.W != s.bufW || s.H != s.bufH {
				if err := b.makeBuffers(s); err != nil {
					return err
				}
			}
			s.configured, s.needDraw = true, true
		case 1: // closed
			return fmt.Errorf("компоновщик убрал поверхность %q", s.cfg.Namespace)
		}
	case s.bufs[0] != nil && (obj == s.bufs[0].id || obj == s.bufs[1].id):
		if op == 0 { // buffer.release
			for _, buf := range s.bufs {
				if buf.id == obj {
					buf.busy = false
				}
			}
		}
	case obj == s.pendingFrame:
		if op == 0 { // frame.done
			s.pendingFrame, s.frameArrived = 0, true
			delete(b.byObj, obj)
		}
	}
	return nil
}

// pointer — события мыши той поверхности, над которой курсор
func (b *backend) pointer(r *rd, op uint32) {
	switch op {
	case 0: // enter(serial, surface, x, y)
		r.u32()
		b.pointerSurf = b.byObj[r.u32()]
		if s := b.pointerSurf; s != nil {
			s.scene.Move(r.fixed(), r.fixed())
			s.needDraw = true
		}
	case 1: // leave(serial, surface)
		r.u32()
		if s := b.byObj[r.u32()]; s != nil {
			s.scene.Leave()
			s.needDraw = true
		}
		b.pointerSurf = nil
	case 2: // motion(time, x, y)
		if s := b.pointerSurf; s != nil {
			r.u32()
			s.scene.Move(r.fixed(), r.fixed())
			s.needDraw = true
		}
	case 3: // button(serial, time, button, state)
		if s := b.pointerSurf; s != nil {
			r.u32()
			r.u32()
			btn, state := r.u32(), r.u32()
			s.scene.Button(btn, state == 1)
			s.needDraw = true
		}
	case 4: // axis(time, axis, value)
		if s := b.pointerSurf; s != nil {
			r.u32()
			axis := r.u32()
			v := r.fixed()
			if axis == 0 { // вертикальное колесо
				s.scene.Wheel(-v)
				s.needDraw = true
			}
		}
	}
}

// keyboard — клавиши: раскладку читаем из keymap и сами превращаем скан-коды
// в буквы (text-input без IME в niri не работает).
func (b *backend) keyboard(r *rd, op uint32) {
	switch op {
	case 0: // keymap(format, size) — дескриптор приходит отдельно, через SCM_RIGHTS
		r.u32()
		r.u32()
		if len(b.c.lastFds) > 0 {
			b.readKeymap(b.c.lastFds[0])
		}
		klog("keymap получен")
	case 1: // enter(serial, surface, keys)
		r.u32()
		b.kbSurf = b.byObj[r.u32()]
		klog("клавиатура вошла на поверхность %p", b.kbSurf)
	case 2: // leave(serial, surface)
		r.u32()
		klog("клавиатура ушла")
		b.kbSurf = nil
	case 3: // key(serial, time, key, state)
		r.u32()
		r.u32()
		key, state := r.u32(), r.u32()
		klog("клавиша keycode=%d state=%d", key, state)
		if state == 1 && b.kbSurf != nil { // 1 = нажатие
			b.keyPress(key)
		}
	case 4: // modifiers(serial, depressed, latched, locked, group)
		r.u32()
		r.u32()
		r.u32()
		r.u32()
		b.kbGroup = int(r.u32())
		klog("группа раскладки=%d", b.kbGroup)
	}
}

// readKeymap читает текст раскладки из дескриптора и разбирает его.
// niri отдаёт fd только через SCM_RIGHTS, а offset у него в конце — поэтому pread.
func (b *backend) readKeymap(fd int) {
	var buf []byte
	tmp := make([]byte, 65536)
	for {
		n, err := unix.Pread(fd, tmp, int64(len(buf)))
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil || n <= 0 {
			break
		}
	}
	b.km = parseKeymap(string(buf))
	klog("раскладка: %d байт, групп=%d клавиш=%d", len(buf), b.km.groups, len(b.km.base))
}

func (b *backend) keyPress(key uint32) {
	switch key {
	case 1: // Escape
		b.keyAction("escape")
	case 14: // Backspace
		b.keyAction("backspace")
	case 28: // Enter
		b.keyAction("enter")
	default:
		// буквы/цифры: переводим скан-код по раскладке (key+8 = кодеклавиши xkb)
		if ch := b.km.translate(int(key)+8, b.kbGroup); ch != "" {
			b.keyText(ch)
		}
	}
}

// hasSearch — есть ли на сцене слой, фильтруемый поиском.
func hasSearch(sc *Scene) bool {
	for _, l := range sc.cfg.Layers {
		if l.Search {
			return true
		}
	}
	return false
}

// keyText добавляет набранный символ в строку поиска открытого ящика
func (b *backend) keyText(s string) {
	for _, surf := range b.surfaces {
		if !surf.scene.searchOpen() || !hasSearch(surf.scene) {
			continue
		}
		surf.scene.typeText(s)
		surf.needDraw = true
	}
}

// keyAction применяет клавишу поиска к открытому ящику приложений
func (b *backend) keyAction(name string) {
	if name == "escape" {
		// закрываем согласованно на всех поверхностях, владеющих группой
		b.group("apps", 2)
		return
	}
	for _, s := range b.surfaces {
		if !s.scene.searchOpen() || !hasSearch(s.scene) {
			continue
		}
		switch name {
		case "backspace":
			s.scene.backspace()
		case "enter":
			s.scene.activateFirst()
		}
		s.needDraw = true
	}
}

// ============================================================
// Управление извне: FIFO, в который пишет lnf.sh
// ============================================================

// setupFIFO создаёт канал и открывает его чтение-запись (O_RDWR, чтобы
// чтение не упиралось в EOF, когда писателей нет).
func setupFIFO() (int, error) {
	dir := fifoDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return -1, err
	}
	p := path.Join(dir, "lnf.fifo")
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		if _, statErr := os.Stat(p); statErr != nil {
			return -1, err
		}
	}
	return unix.Open(p, unix.O_RDWR|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
}

func fifoDir() string {
	if d, err := os.UserCacheDir(); err == nil {
		return path.Join(d, "lnf")
	}
	return "/tmp/lnf"
}

func (b *backend) readCommands() {
	buf := make([]byte, 4096)
	n, err := unix.Read(b.cmdFD, buf)
	if n <= 0 || err != nil {
		return
	}
	for _, line := range strings.Split(string(buf[:n]), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			b.command(line)
		}
	}
}

// command выполняет строку из FIFO: «apps», «notify текст», «toggle apps»…
func (b *backend) command(line string) {
	action, arg := line, ""
	if i := strings.IndexAny(line, " \t"); i >= 0 {
		action, arg = line[:i], strings.TrimSpace(line[i+1:])
	}
	switch action {
	case "notify", "msg":
		b.notify(arg)
	case "type", "text":
		for _, s := range b.surfaces {
			if s.scene.searchOpen() && hasSearch(s.scene) {
				s.scene.typeText(arg)
				s.needDraw = true
			}
		}
	case "key":
		switch arg {
		case "bs", "backspace":
			b.keyAction("backspace")
		case "return", "enter":
			b.keyAction("enter")
		case "escape", "esc":
			b.keyAction("escape")
		}
	case "quit", "exit":
		os.Exit(0)
	case "set":
		if i := strings.IndexAny(arg, " \t"); i >= 0 {
			b.setPoll(strings.TrimSpace(arg[:i]), strings.TrimSpace(arg[i+1:]))
		}
	case "open":
		b.group(arg, 1)
	case "close":
		b.group(arg, 2)
	case "toggle":
		b.group(arg, 0)
	default:
		b.group(action, 0) // ярлык: apps / clip / utils / power
	}
}

// group управляет раскрывашкой во всех сценах, где она есть. mode 0 —
// переключить; при нём цель одна на все поверхности (иначе, если состояние
// поверхностей разошлось, toggle каждой увёл бы их ещё дальше друг от друга).
func (b *backend) group(name string, mode int) {
	if name == "" {
		return
	}
	var owners []*surface
	anyOn := false
	for _, s := range b.surfaces {
		owns := false
		for _, l := range s.cfg.Layers {
			if l.ShowWhen == name || l.Toggle == name {
				owns = true
				break
			}
		}
		if !owns {
			continue
		}
		owners = append(owners, s)
		if s.scene.groups[name] {
			anyOn = true
		}
	}
	if mode == 0 {
		if anyOn {
			mode = 2
		} else {
			mode = 1
		}
	}
	for _, s := range owners {
		s.scene.setGroup(name, mode)
		s.needDraw = true
	}
}

// setPoll — внешнее значение опроса (например, OSD громкости без опроса):
// обновляем значение, взводим вспышку там, где по нему всплывает OSD, и будим
// цикл. Так громкость показывается по нажатию клавиши, без постоянных опросов.
func (b *backend) setPoll(name, val string) {
	changed := false
	for _, s := range b.surfaces {
		if s.scene.setPoll(name, val) {
			changed = true
		}
	}
	if !changed {
		return
	}
	for _, s := range b.surfaces {
		if s.scene.flashesOn(name) {
			s.scene.markFlash(name)
		}
		s.needDraw = true
	}
	holdPollWake(name)
}

// notify отправляет сообщение в OSD-баннер
func (b *backend) notify(msg string) {
	if msg == "" {
		return
	}
	msg = string(rune(0xf0f3)) + " " + msg // колокольчик
	for _, s := range b.surfaces {
		if s.scene.flashesOn("notify") {
			s.scene.setPoll("notify", msg)
			s.scene.markFlash("notify")
			s.needDraw = true
		}
	}
	holdPollWake("notify")
}

func (b *backend) bind(name uint32, iface string, ver uint32) {
	if b.c.trace {
		fmt.Fprintf(os.Stderr, "lnf: глобал %s v%d\n", iface, ver)
	}
	lo := func(a, b2 uint32) uint32 {
		if a < b2 {
			return a
		}
		return b2
	}
	switch iface {
	case "wl_compositor":
		b.idCompositor = b.newID()
		b.send(idReg, 0, A{}.u32(name).str(iface).u32(lo(ver, 4)).u32(b.idCompositor))
	case "wl_shm":
		b.idShm = b.newID()
		b.send(idReg, 0, A{}.u32(name).str(iface).u32(1).u32(b.idShm))
	case "zwlr_layer_shell_v1":
		b.idLayerShell = b.newID()
		b.send(idReg, 0, A{}.u32(name).str(iface).u32(lo(ver, 4)).u32(b.idLayerShell))
	case "wl_seat":
		b.idSeat = b.newID()
		b.send(idReg, 0, A{}.u32(name).str(iface).u32(lo(ver, 5)).u32(b.idSeat))
	}
}
