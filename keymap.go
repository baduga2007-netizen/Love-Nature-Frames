package main

import (
	"strconv"
	"strings"
	"unicode"
)

// Раскладка клавиатуры из wl_keyboard.keymap. Без IME текстовый ввод в niri
// не приходит, поэтому скан-коды переводим в символы сами: берём первый
// (нулевой) уровень каждой группы — фильтр приложений регистронезависимый,
// так что Shift/Caps не важны, достаточно базового символа.

type keymap struct {
	codeName map[int]string    // кодеклавиши xkb -> имя (<AD01>)
	base     map[string][]rune // имя -> базовый символ по группам (0 = группа1)
	groups   int
}

func parseKeymap(text string) *keymap {
	km := &keymap{codeName: map[int]string{}, base: map[string][]rune{}}
	section := ""
	var name string
	var block strings.Builder

	for _, ln := range strings.Split(text, "\n") {
		t := strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(t, "xkb_keycodes"):
			section = "codes"
		case strings.HasPrefix(t, "xkb_symbols"):
			section = "symbols"
		case strings.HasPrefix(t, "xkb_"):
			section = "other"
		}

		if section == "codes" {
			if n, code, ok := codeOf(ln); ok {
				km.codeName[code] = n
			}
			continue
		}
		if section != "symbols" {
			continue
		}

		if strings.HasPrefix(t, "key <") {
			km.parseBlock(name, block.String())
			name, block = keyName(t), strings.Builder{}
			block.WriteString(t)
		} else if name == "" {
			continue
		} else {
			block.WriteString("\n")
			block.WriteString(t)
		}
		if strings.Contains(t, "};") {
			km.parseBlock(name, block.String())
			name, block = "", strings.Builder{}
		}
	}
	return km
}

// parseBlock разбирает тело «key <NAME> { ... }»: символы каждой группы.
func (km *keymap) parseBlock(name, block string) {
	if name == "" {
		return
	}
	grouped := false
	for _, ln := range strings.Split(block, "\n") {
		if gi, content, ok := symbolGroup(ln); ok {
			grouped = true
			km.setBase(name, gi, firstSym(content))
		}
	}
	if !grouped { // однострочная запись — это группа 1
		if content, ok := between(block, '[', ']'); ok {
			km.setBase(name, 1, firstSym(content))
		}
	}
}

// codeOf разбирает строку секции keycodes «<AD01> = 24;».
func codeOf(ln string) (string, int, bool) {
	ln = strings.TrimSpace(ln)
	if !strings.HasPrefix(ln, "<") {
		return "", 0, false
	}
	end := strings.IndexByte(ln, '>')
	if end < 0 {
		return "", 0, false
	}
	rest := strings.TrimSpace(ln[end+1:])
	if !strings.HasPrefix(rest, "=") {
		return "", 0, false
	}
	rest = strings.TrimSuffix(strings.TrimSpace(rest[1:]), ";")
	code, err := strconv.Atoi(strings.TrimSpace(rest))
	if err != nil {
		return "", 0, false
	}
	return ln[1:end], code, true
}

func keyName(ln string) string {
	rest := ln[len("key <"):]
	if i := strings.IndexByte(rest, '>'); i >= 0 {
		return rest[:i]
	}
	return ""
}

// symbolGroup разбирает «symbols[2]= [ q, Q ]» -> номер группы и содержимое.
func symbolGroup(ln string) (int, string, bool) {
	i := strings.Index(ln, "symbols[")
	if i < 0 {
		return 0, "", false
	}
	rest := ln[i+len("symbols["):]
	j := strings.IndexByte(rest, ']')
	if j < 0 {
		return 0, "", false
	}
	n, err := strconv.Atoi(rest[:j])
	if err != nil {
		return 0, "", false
	}
	content, ok := between(rest[j+1:], '[', ']')
	return n, content, ok
}

func between(s string, open, close byte) (string, bool) {
	i := strings.IndexByte(s, open)
	if i < 0 {
		return "", false
	}
	j := strings.IndexByte(s[i+1:], close)
	if j < 0 {
		return "", false
	}
	return s[i+1 : i+1+j], true
}

// firstSym берёт первый символ из « q, Q » -> "q"
func firstSym(list string) string {
	if i := strings.IndexByte(list, ','); i >= 0 {
		list = list[:i]
	}
	return strings.TrimSpace(list)
}

func (km *keymap) setBase(name string, group int, sym string) {
	r := symRune(sym)
	if r == 0 || group < 1 {
		return
	}
	for len(km.base[name]) < group {
		km.base[name] = append(km.base[name], 0)
	}
	if km.base[name][group-1] == 0 {
		km.base[name][group-1] = r
	}
	if group > km.groups {
		km.groups = group
	}
}

// translate превращает скан-код (wl_keyboard.key) и номер группы в строку.
func (km *keymap) translate(code, group int) string {
	if km == nil {
		return ""
	}
	name, ok := km.codeName[code]
	if !ok {
		return ""
	}
	runes := km.base[name]
	if len(runes) == 0 {
		return ""
	}
	i := group
	if i < 0 || i >= len(runes) || runes[i] == 0 {
		i = 0
	}
	r := runes[i]
	if r < 0x20 { // управляющие и NoSymbol не нужны
		return ""
	}
	return string(unicode.ToLower(r))
}

// symRune переводит имя или число keysym в символ.
func symRune(sym string) rune {
	if sym == "" || sym == "NoSymbol" {
		return 0
	}
	if len(sym) > 2 && sym[0] == '0' && (sym[1] == 'x' || sym[1] == 'X') {
		if n, err := strconv.ParseUint(sym[2:], 16, 32); err == nil {
			return symFromCode(uint32(n))
		}
	}
	if len(sym) == 1 {
		return rune(sym[0])
	}
	if sym[0] == 'U' || sym[0] == 'u' {
		if n, err := strconv.ParseUint(sym[1:], 16, 32); err == nil {
			return rune(n)
		}
	}
	if r, ok := named[sym]; ok {
		return r
	}
	return 0
}

// symFromCode переводит числовой keysym в символ. Нужны латиница, цифры,
// кириллица; служебные клавиши (>0x7e) отбрасываем.
func symFromCode(v uint32) rune {
	switch {
	case v >= 0x20 && v <= 0x7e:
		return rune(v)
	case v >= 0x100 && v <= 0x1ff: // Latin-1 и Latin Extended-A
		return rune(v)
	case v == 0x06a3: // Cyrillic_io
		return 'ё'
	case v == 0x06b1: // numerosign
		return '№'
	case v >= 0x06c0 && v <= 0x06df: // строчная кириллица
		return cyr[v-0x06c0]
	case v >= 0x06e0 && v <= 0x06ff: // прописная кириллица
		return unicode.ToUpper(cyr[v-0x06e0])
	case v >= 0x01000000: // Unicode-keysym
		return rune(v & 0xffffff)
	}
	return 0
}

// строчная кириллица 0x06c0..0x06df (порядок legacy-keysym)
var cyr = []rune("юабцдефгхийклмнопярстужвьызшэщчъ")

var named = map[string]rune{
	"space": ' ', "minus": '-', "equal": '=', "plus": '+',
	"bracketleft": '[', "bracketright": ']', "backslash": '\\',
	"semicolon": ';', "apostrophe": '\'', "grave": '`',
	"comma": ',', "period": '.', "slash": '/',
	"numbersign": '#', "asterisk": '*', "underscore": '_',

	"Cyrillic_io": 'ё',
	"Cyrillic_a":  'а', "Cyrillic_be": 'б', "Cyrillic_ve": 'в',
	"Cyrillic_ghe": 'г', "Cyrillic_de": 'д', "Cyrillic_ie": 'е',
	"Cyrillic_zhe": 'ж', "Cyrillic_ze": 'з', "Cyrillic_ii": 'и',
	"Cyrillic_shorti": 'й', "Cyrillic_ka": 'к', "Cyrillic_el": 'л',
	"Cyrillic_em": 'м', "Cyrillic_en": 'н', "Cyrillic_o": 'о',
	"Cyrillic_pe": 'п', "Cyrillic_er": 'р', "Cyrillic_es": 'с',
	"Cyrillic_te": 'т', "Cyrillic_u": 'у', "Cyrillic_ef": 'ф',
	"Cyrillic_ha": 'х', "Cyrillic_tse": 'ц', "Cyrillic_che": 'ч',
	"Cyrillic_sha": 'ш', "Cyrillic_shcha": 'щ', "Cyrillic_hardsign": 'ъ',
	"Cyrillic_yeru": 'ы', "Cyrillic_softsign": 'ь', "Cyrillic_e": 'э',
	"Cyrillic_yu": 'ю', "Cyrillic_ya": 'я',
}
