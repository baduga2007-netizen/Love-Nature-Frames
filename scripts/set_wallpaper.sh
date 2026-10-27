#!/bin/sh
# LNF: поставить обои. Поддерживаются awww и swaybg вместе.
# Приоритет: если swaybg установлен — им (статика), иначе awww.
f="$1"
[ -f "$f" ] || { echo "нет файла: $f" >&2; exit 1; }

if command -v swaybg >/dev/null 2>&1; then
    pkill -x swaybg 2>/dev/null
    sleep 0.1
    setsid swaybg -i "$f" -m fill >/dev/null 2>&1 &
elif command -v swww >/dev/null 2>&1; then
    if ! pgrep -x swww-daemon >/dev/null 2>&1; then
        setsid swww-daemon >/dev/null 2>&1 &
        sleep 0.3
    fi
    swww img "$f" >/dev/null 2>&1
else
    echo "нет ни swaybg, ни swww" >&2
    exit 1
fi
