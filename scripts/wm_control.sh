#!/bin/sh
# ============================================================
# wm.sh — прослойка между LNF и композитором.
#
# LNF не знает, под чем запущен: swayfx/sway, niri или Hyprland. Все
# команды управления окнами/рабочими столами идут через этот скрипт,
# поэтому одна и та же панель работает везде.
#
# Использование:
#   wm.sh ws-up | ws-down | workspace N
#   wm.sh spawn КОМАНДА [АРГ...]
#   wm.sh screenshot | screenshot-window
#   wm.sh lock | logout | overview
# ============================================================

# PATH может быть пустым у сервисов runit — задаём явно.
PATH="${HOME}/.local/bin:/usr/local/bin:/usr/bin:/bin:${PATH:-}"
export PATH

have() { command -v "$1" >/dev/null 2>&1; }

detect_wm() {
    if [ -n "${SWAYSOCK:-}" ] || pgrep -x sway >/dev/null 2>&1; then
        echo sway
    elif [ -n "${NIRI_SOCKET:-}" ] || pgrep -x niri >/dev/null 2>&1; then
        echo niri
    elif [ -n "${HYPRLAND_INSTANCE_SIGNATURE:-}" ] || pgrep -x Hyprland >/dev/null 2>&1; then
        echo hypr
    else
        echo none
    fi
}

wm=$(detect_wm)

notify() {
    [ -x "$HOME/LNF/scripts/lnf_fifo.sh" ] && "$HOME/LNF/scripts/lnf_fifo.sh" notify "$*" 2>/dev/null
}

copy_png() {
    # $1 — файл; кладём в буфер и уведомляем панель.
    if [ -f "$1" ]; then
        wl-copy < "$1" 2>/dev/null
        notify "Screenshot copied to clipboard"
    fi
}

case "$1" in
    ws-up)
        case "$wm" in
            sway) swaymsg workspace prev >/dev/null 2>&1 ;;
            niri) niri msg action focus-workspace-up >/dev/null 2>&1 ;;
            hypr) hyprctl dispatch workspace e-1 >/dev/null 2>&1 ;;
        esac
        ;;

    ws-down)
        case "$wm" in
            sway) swaymsg workspace next >/dev/null 2>&1 ;;
            niri) niri msg action focus-workspace-down >/dev/null 2>&1 ;;
            hypr) hyprctl dispatch workspace e+1 >/dev/null 2>&1 ;;
        esac
        ;;

    workspace)
        shift
        n="$1"
        case "$wm" in
            sway) swaymsg workspace "$n" >/dev/null 2>&1 ;;
            niri) niri msg action focus-workspace "$n" >/dev/null 2>&1 ;;
            hypr) hyprctl dispatch workspace "$n" >/dev/null 2>&1 ;;
        esac
        ;;

    spawn)
        shift
        case "$wm" in
            sway) swaymsg exec -- "$*" >/dev/null 2>&1 ;;
            niri) niri msg action spawn -- "$@" >/dev/null 2>&1 ;;
            hypr) hyprctl dispatch exec -- "$*" >/dev/null 2>&1 ;;
            *)    setsid "$@" >/dev/null 2>&1 & ;;
        esac
        ;;

    screenshot)
        case "$wm" in
            sway)
                f=$(mktemp /tmp/lnf-shot-XXXXXX.png)
                if grim -g "$(slurp)" "$f" 2>/dev/null; then copy_png "$f"; else rm -f "$f"; fi
                ;;
            niri) niri msg action screenshot >/dev/null 2>&1 ;;
            hypr)
                f=$(mktemp /tmp/lnf-shot-XXXXXX.png)
                if grim -g "$(slurp)" "$f" 2>/dev/null; then copy_png "$f"; else rm -f "$f"; fi
                ;;
            *)
                f=$(mktemp /tmp/lnf-shot-XXXXXX.png)
                if grim "$f" 2>/dev/null; then copy_png "$f"; else rm -f "$f"; fi
                ;;
        esac
        ;;

    screenshot-window)
        case "$wm" in
            sway)
                geo=$(swaymsg -t get_tree 2>/dev/null | jq -r \
                    '.. | select(.focused? == true) | .rect | "\(.x),\(.y) \(.width)x\(.height)"' 2>/dev/null | head -1)
                f=$(mktemp /tmp/lnf-win-XXXXXX.png)
                if [ -n "$geo" ] && grim -g "$geo" "$f" 2>/dev/null; then copy_png "$f"; else rm -f "$f"; fi
                ;;
            niri) niri msg action screenshot-window >/dev/null 2>&1 ;;
            hypr)
                geo=$(hyprctl -j activewindow 2>/dev/null | jq -r \
                    'select(.at != null and .size != null) |
                     "\(.at[0]),\(.at[1]) \(.size[0])x\(.size[1])"' 2>/dev/null | head -1)
                f=$(mktemp /tmp/lnf-win-XXXXXX.png)
                if [ -n "$geo" ] && grim -g "$geo" "$f" 2>/dev/null; then copy_png "$f"; else rm -f "$f"; fi
                ;;
            *)
                f=$(mktemp /tmp/lnf-win-XXXXXX.png)
                if grim "$f" 2>/dev/null; then copy_png "$f"; else rm -f "$f"; fi
                ;;
        esac
        ;;

    lock)
        if have hyprlock; then
            hyprlock
        elif have swaylock; then
            setsid swaylock -f -c 2E3440 >/dev/null 2>&1 &
        elif have loginctl; then
            loginctl lock-session
        fi
        ;;

    logout)
        case "$wm" in
            sway) swaymsg exit >/dev/null 2>&1 ;;
            niri) niri msg action quit --skip-confirmation >/dev/null 2>&1 ;;
            hypr) hyprctl dispatch exit >/dev/null 2>&1 ;;
        esac
        ;;

    overview)
        case "$wm" in
            niri) niri msg action toggle-overview >/dev/null 2>&1 ;;
            *) : ;;
        esac
        ;;

    screenshot-screen)
        case "$wm" in
            sway)
                f=$(mktemp /tmp/lnf-screen-XXXXXX.png)
                if grim "$f" 2>/dev/null; then copy_png "$f"; else rm -f "$f"; fi
                ;;
            niri) niri msg action screenshot-screen >/dev/null 2>&1 ;;
            hypr)
                f=$(mktemp /tmp/lnf-screen-XXXXXX.png)
                if grim "$f" 2>/dev/null; then copy_png "$f"; else rm -f "$f"; fi
                ;;
        esac
        ;;

    *)
        echo "wm.sh: неизвестная команда: $1" >&2
        echo "  ws-up ws-down workspace N spawn ... screenshot screenshot-window lock logout overview screenshot-screen" >&2
        exit 2
        ;;
esac

exit 0
