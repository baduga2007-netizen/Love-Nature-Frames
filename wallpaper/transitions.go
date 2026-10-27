package wallpaper

import "time"

// TransitionFunc — тип функции перехода.
type TransitionFunc func(current, next string, wm *WM, t time.Duration) error

// TransitionByName — получение перехода по имени.
func TransitionByName(name string) (TransitionFunc, bool) {
	fn, ok := builtinTransitions[name]
	return fn, ok
}

// TransitionList — список всех переходов.
func TransitionList() []string {
	out := make([]string, 0, len(builtinTransitions))
	for name := range builtinTransitions {
		out = append(out, name)
	}
	return out
}

// Встроенные переходы.
var builtinTransitions = map[string]TransitionFunc{
	"fade":             transitionFade,
	"wipe":             transitionWipe,
	"flip":             transitionFlip,
	"zoom":             transitionZoom,
	"morph":            transitionMorph,
	"custom":           nil,
	"slide":            transitionSlide,
	"radial":           transitionRadial,
	"swirl":            transitionSwirl,
	"crossfade":        transitionCrossfade,
	"zoom-out":         transitionZoomOut,
	"zoom-in":          transitionZoomIn,
	"zoom-center":      transitionZoomCenter,
	"zoom-center-x":    transitionZoomCenterX,
	"zoom-center-y":    transitionZoomCenterY,
	"zoom-center-xy":   transitionZoomCenterXY,
	"zoom-center-xyd":  transitionZoomCenterXYD,
	"zoom-center-xydd": transitionZoomCenterXYDD,
	"color":            transitionColor,
	"fade-fade":        transitionFadeFade,
	"fade-slide":       transitionFadeSlide,
	"fade-flip":        transitionFadeFlip,
	"fade-zoom":        transitionFadeZoom,
	"fade-morph":       transitionFadeMorph,
	"fade-radial":      transitionFadeRadial,
	"fade-radial-750":  transitionFadeRadial750,
	"fade-fade-fade":   transitionFadeFadeFade,
	"slide-slide":      transitionSlideSlide,
	"slide-fade":       transitionSlideFade,
}

// transitionFade — базовый fade.
func transitionFade(current, next string, wm *WM, t time.Duration) error {
	_ = current
	_ = next
	_ = wm
	_ = t
	return nil
}

// transitionWipe — wipe.
func transitionWipe(current, next string, wm *WM, t time.Duration) error {
	_ = current
	_ = next
	_ = wm
	_ = t
	return nil
}

// transitionFlip — flip.
func transitionFlip(current, next string, wm *WM, t time.Duration) error {
	_ = current
	_ = next
	_ = wm
	_ = t
	return nil
}

// transitionZoom — zoom.
func transitionZoom(current, next string, wm *WM, t time.Duration) error {
	_ = current
	_ = next
	_ = wm
	_ = t
	return nil
}

// transitionMorph — morph.
func transitionMorph(current, next string, wm *WM, t time.Duration) error {
	_ = current
	_ = next
	_ = wm
	_ = t
	return nil
}

// transitionSlide — slide.
func transitionSlide(current, next string, wm *WM, t time.Duration) error {
	_ = current
	_ = next
	_ = wm
	_ = t
	return nil
}

// transitionRadial — radial.
func transitionRadial(current, next string, wm *WM, t time.Duration) error {
	_ = current
	_ = next
	_ = wm
	_ = t
	return nil
}

// transitionSwirl — swirl.
func transitionSwirl(current, next string, wm *WM, t time.Duration) error {
	_ = current
	_ = next
	_ = wm
	_ = t
	return nil
}

// transitionCrossfade — crossfade.
func transitionCrossfade(current, next string, wm *WM, t time.Duration) error {
	_ = current
	_ = next
	_ = wm
	_ = t
	return nil
}

// transitionZoomOut — zoom-out.
func transitionZoomOut(current, next string, wm *WM, t time.Duration) error {
	_ = current
	_ = next
	_ = wm
	_ = t
	return nil
}

// transitionZoomIn — zoom-in.
func transitionZoomIn(current, next string, wm *WM, t time.Duration) error {
	_ = current
	_ = next
	_ = wm
	_ = t
	return nil
}

// transitionZoomCenter — zoom-center.
func transitionZoomCenter(current, next string, wm *WM, t time.Duration) error {
	_ = current
	_ = next
	_ = wm
	_ = t
	return nil
}

// transitionZoomCenterX — zoom-center-x.
func transitionZoomCenterX(current, next string, wm *WM, t time.Duration) error {
	_ = current
	_ = next
	_ = wm
	_ = t
	return nil
}

// transitionZoomCenterY — zoom-center-y.
func transitionZoomCenterY(current, next string, wm *WM, t time.Duration) error {
	_ = current
	_ = next
	_ = wm
	_ = t
	return nil
}

// transitionZoomCenterXY — zoom-center-xy.
func transitionZoomCenterXY(current, next string, wm *WM, t time.Duration) error {
	_ = current
	_ = next
	_ = wm
	_ = t
	return nil
}

// transitionZoomCenterXYD — zoom-center-xyd.
func transitionZoomCenterXYD(current, next string, wm *WM, t time.Duration) error {
	_ = current
	_ = next
	_ = wm
	_ = t
	return nil
}

// transitionZoomCenterXYDD — zoom-center-xydd.
func transitionZoomCenterXYDD(current, next string, wm *WM, t time.Duration) error {
	_ = current
	_ = next
	_ = wm
	_ = t
	return nil
}

// transitionColor — color.
func transitionColor(current, next string, wm *WM, t time.Duration) error {
	_ = current
	_ = next
	_ = wm
	_ = t
	return nil
}

// transitionFadeFade — fade-fade.
func transitionFadeFade(current, next string, wm *WM, t time.Duration) error {
	_ = current
	_ = next
	_ = wm
	_ = t
	return nil
}

// transitionFadeSlide — fade-slide.
func transitionFadeSlide(current, next string, wm *WM, t time.Duration) error {
	_ = current
	_ = next
	_ = wm
	_ = t
	return nil
}

// transitionFadeFlip — fade-flip.
func transitionFadeFlip(current, next string, wm *WM, t time.Duration) error {
	_ = current
	_ = next
	_ = wm
	_ = t
	return nil
}

// transitionFadeZoom — fade-zoom.
func transitionFadeZoom(current, next string, wm *WM, t time.Duration) error {
	_ = current
	_ = next
	_ = wm
	_ = t
	return nil
}

// transitionFadeMorph — fade-morph.
func transitionFadeMorph(current, next string, wm *WM, t time.Duration) error {
	_ = current
	_ = next
	_ = wm
	_ = t
	return nil
}

// transitionFadeRadial — fade-radial.
func transitionFadeRadial(current, next string, wm *WM, t time.Duration) error {
	_ = current
	_ = next
	_ = wm
	_ = t
	return nil
}

// transitionFadeRadial750 — fade-radial-750.
func transitionFadeRadial750(current, next string, wm *WM, t time.Duration) error {
	_ = current
	_ = next
	_ = wm
	_ = t
	return nil
}

// transitionFadeFadeFade — fade-fade-fade.
func transitionFadeFadeFade(current, next string, wm *WM, t time.Duration) error {
	_ = current
	_ = next
	_ = wm
	_ = t
	return nil
}

// transitionSlideSlide — slide-slide.
func transitionSlideSlide(current, next string, wm *WM, t time.Duration) error {
	_ = current
	_ = next
	_ = wm
	_ = t
	return nil
}

// transitionSlideFade — slide-fade.
func transitionSlideFade(current, next string, wm *WM, t time.Duration) error {
	_ = current
	_ = next
	_ = wm
	_ = t
	return nil
}
