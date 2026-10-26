#!/bin/sh
# LNF OSD: громкость. Печатает «значок ██████████ 72%».
# Значок — volume-high (U+F028), при mute — volume-mute (U+F026).
# Полоска — 10 сегментов из блоков U+2588 / U+2591.
# Экранированные байты (\NNN) разворачивает printf %b, чтобы PUA-глифы
# не хранились в файле буквально.
v=$(wpctl get-volume @DEFAULT_AUDIO_SINK@ 2>/dev/null) || exit 0
[ -n "$v" ] || exit 0

mute=0
case "$v" in *MUTED*) mute=1 ;; esac

val=${v#Volume: }   # "0.72" или "0.72 [MUTED]"
val=${val%% *}
frac=${val#*.}
frac=${frac#0}
pct=$(( ${val%.*} * 100 + frac ))
[ "$pct" -lt 0 ] && pct=0
[ "$pct" -gt 150 ] && pct=150

filled=$(( pct / 10 ))
[ "$filled" -gt 10 ] && filled=10

BLK='\342\226\210'   # █
DIM='\342\226\221'   # ░
bar=""
i=0
while [ "$i" -lt 10 ]; do
    if [ "$i" -lt "$filled" ]; then bar="$bar$BLK"; else bar="$bar$DIM"; fi
    i=$((i + 1))
done

if [ "$mute" = 1 ]; then icon='\357\200\246'; else icon='\357\200\250'; fi
printf '%b %b %s%%\n' "$icon" "$bar" "$pct"
