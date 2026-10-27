#!/bin/sh
# ============================================================
# ws.sh — индикатор рабочих столов для LNF.
#
# Печатает «N A»: N — максимальный номер воркспейса,
# A — активный. Геометрию точек (обычная/растянутая) рисует панель.
# Работает под sway/swayfx, niri и Hyprland.
# ============================================================

if [ -n "${SWAYSOCK:-}" ] || pgrep -x sway >/dev/null 2>&1; then
    swaymsg -t get_workspaces 2>/dev/null | jq -r '
      ([.[].num | select(. > 0)] | max // 0) as $n |
      ([.[] | select(.focused) | .num | select(. > 0)][0] // 1) as $a |
      "\($n) \($a)"
    '
elif [ -n "${NIRI_SOCKET:-}" ] || pgrep -x niri >/dev/null 2>&1; then
    niri msg --json workspaces 2>/dev/null | jq -r '
      ([.[] | .idx] | max // 0) as $n |
      ([.[] | select(.is_focused) | .idx][0] // 1) as $a |
      "\($n) \($a)"
    '
elif command -v hyprctl >/dev/null 2>&1 &&
    { [ -n "${HYPRLAND_INSTANCE_SIGNATURE:-}" ] || pgrep -x Hyprland >/dev/null 2>&1; }; then
    # Hyprland: id могут быть отрицательными (special workspaces) — берём только обычные.
    max=$(hyprctl -j workspaces 2>/dev/null |
        jq -r '[.[] | .id | select(. > 0)] | max // 0' 2>/dev/null)
    act=$(hyprctl -j activeworkspace 2>/dev/null | jq -r '.id // 1' 2>/dev/null)
    case "${max:-}" in '' | *[!0-9]*) max=1 ;; esac
    case "${act:-}" in '' | *[!0-9]*) act=1 ;; esac
    [ "$max" -gt 0 ] 2>/dev/null || max=1
    [ "$act" -gt 0 ] 2>/dev/null || act=1
    printf '%s %s\n' "$max" "$act"
fi
