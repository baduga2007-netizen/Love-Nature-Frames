#!/bin/sh
# LNF: системные ресурсы для левого острова (CPU % и занятая RAM).
# Один sh + free на такт; состояние для дельты CPU лежит в $XDG_RUNTIME_DIR.
state="${XDG_RUNTIME_DIR:-/tmp}/lnf-cpu-${UID:-$(id -u)}"

read -r _ u n s i w irq sirq st _ < /proc/stat
idle=$((i + w))
total=$((u + n + s + i + w + irq + sirq + st))

pt=0
pi=0
if [ -r "$state" ]; then
    read -r pt pi < "$state" 2>/dev/null
fi
printf '%s %s\n' "$total" "$idle" > "$state" 2>/dev/null

if [ "${pt:-0}" -gt 0 ] && [ "$total" -gt "${pt:-0}" ]; then
    dt=$((total - pt))
    di=$((idle - ${pi:-0}))
    cpu=$(((100 * (dt - di) + dt / 2) / dt))
else
    cpu=0
fi

mem=$(free -m | awk '/^Mem:/ { printf "%.1fG", $3 / 1024 }')
printf '%d%%  ·  %s\n' "$cpu" "$mem"
