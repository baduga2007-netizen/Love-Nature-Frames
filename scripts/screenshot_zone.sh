#!/bin/sh
# ============================================================
# shot.sh — скриншот с задержкой и выбором зоны (swayfx/sway).
#
# Задержку и зону кладёт виджет LNF в ~/.cache/lnf/shot.sec и
# ~/.cache/lnf/shot.zone (0/3/5/10 и full|zone|window). Скрипт
# закрывает карточку «Скриншот», ждёт, снимает и кладёт PNG
# в буфер обмена; уведомление показывает сама панель.
#
#   shot.sh              — взять сохранённый выбор
#   shot.sh 5 zone       — задержка 5 с, зона по выделению
#   shot.sh 0 full       — сразу весь экран
# ============================================================

PATH="${HOME}/.local/bin:/usr/local/bin:/usr/bin:/bin:${PATH:-}"
export PATH

dir="${XDG_CACHE_HOME:-$HOME/.cache}/lnf"
mkdir -p "$dir" 2>/dev/null

sec="$1"
[ -n "$sec" ] || sec=$(tr -cd '0-9' < "$dir/shot.sec" 2>/dev/null)
[ -n "$sec" ] || sec=0

zone="$2"
[ -n "$zone" ] || zone=$(cat "$dir/shot.zone" 2>/dev/null)
case "$zone" in
    full | zone | window) ;;
    *) zone=full ;;
esac

notify() { [ -x "$HOME/LNF/scripts/lnf_fifo.sh" ] && "$HOME/LNF/scripts/lnf_fifo.sh" notify "$*" 2>/dev/null; }

# карточка не должна висеть в кадре — закрываем её до задержки
[ -x "$HOME/LNF/scripts/lnf_fifo.sh" ] && "$HOME/LNF/scripts/lnf_fifo.sh" toggle shot 2>/dev/null

[ "$sec" -gt 0 ] 2>/dev/null && sleep "$sec"

f=$(mktemp /tmp/lnf-shot-XXXXXX.png) || exit 1

case "$zone" in
    zone)
        if command -v slurp >/dev/null 2>&1; then
            geo=$(slurp 2>/dev/null)
            [ -n "$geo" ] || { rm -f "$f"; exit 0; }
            grim -g "$geo" "$f" 2>/dev/null
        else
            grim "$f" 2>/dev/null
        fi
        ;;
    window)
        geo=""
        if command -v swaymsg >/dev/null 2>&1 && command -v jq >/dev/null 2>&1; then
            geo=$(swaymsg -t get_tree 2>/dev/null | jq -r \
                '.. | select(.focused? == true) | .rect |
                 "\(.x),\(.y) \(.width)x\(.height)"' 2>/dev/null | head -1)
        fi
        # Hyprland: геометрия активного окна через hyprctl
        if [ -z "$geo" ] && command -v hyprctl >/dev/null 2>&1 &&
            command -v jq >/dev/null 2>&1; then
            geo=$(hyprctl -j activewindow 2>/dev/null | jq -r \
                'select(.at != null and .size != null) |
                 "\(.at[0]),\(.at[1]) \(.size[0])x\(.size[1])"' 2>/dev/null | head -1)
        fi
        if [ -n "$geo" ]; then
            grim -g "$geo" "$f" 2>/dev/null
        else
            grim "$f" 2>/dev/null
        fi
        ;;
    *)
        grim "$f" 2>/dev/null
        ;;
esac

if [ -s "$f" ]; then
    wl-copy < "$f" 2>/dev/null
    notify "Скриншот скопирован в буфер"
else
    rm -f "$f"
    notify "Скриншот не получился"
fi

exit 0
