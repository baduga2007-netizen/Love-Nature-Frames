#!/bin/sh
# LNF: поставить обои своей программой — lnf wallpaper (демон на
# wlr-layer-shell + wl_shm). Внешние awww/swaybg/swww больше не нужны.
#
#   set_wallpaper.sh PATH.png     — картинка
#   set_wallpaper.sh gen:sage     — сгенерировать (sage | moss | dew)
#
# Демон поднимется сам, если ещё не запущен.
f="$1"
[ -n "$f" ] || { echo "нужен файл: set_wallpaper.sh picture.png" >&2; exit 1; }
case "$f" in
    gen:*) ;;
    *) [ -f "$f" ] || { echo "нет файла: $f" >&2; exit 1; } ;;
esac

bin="${LNF_BIN:-$HOME/LNF/lnf}"
[ -x "$bin" ] || { echo "нет $bin — собери: go build -o lnf ." >&2; exit 1; }

exec "$bin" wallpaper "$f"
