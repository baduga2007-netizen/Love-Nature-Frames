#!/bin/sh
# ============================================================
# power.sh — питание без привязки к конкретному init.
#
# Порядок выбора:
#   1. systemd (systemctl)         — Arch/CachyOS и т.п.
#   2. elogind (loginctl)          — Artix/Void/Devuan + runit/openrc
#   3. zzz / ZZZ                   — минималистичные runit-сборки
#   4. reboot / poweroff           — последний шанс (через sudo -n)
#
# Использование: power.sh suspend | reboot | poweroff
# ============================================================

PATH="${HOME}/.local/bin:/usr/local/bin:/usr/bin:/bin:${PATH:-}"
export PATH

have() { command -v "$1" >/dev/null 2>&1; }

# Есть ли именно systemd (а не просто бинарь systemctl в контейнере).
sysd() { [ -d /run/systemd/system ] && have systemctl; }

action="$1"

do_action() {
    case "$1" in
        suspend)
            if sysd; then systemctl suspend
            elif have loginctl; then loginctl suspend
            elif have zzz; then zzz
            else
                echo "power.sh: нет способа усыпить систему" >&2
                return 1
            fi
            ;;
        reboot)
            if sysd; then systemctl reboot
            elif have loginctl; then loginctl reboot
            elif have ZZZ; then ZZZ
            else sudo -n reboot 2>/dev/null || reboot
            fi
            ;;
        poweroff)
            if sysd; then systemctl poweroff
            elif have loginctl; then loginctl poweroff
            elif have zzz; then zzz -p 2>/dev/null || zzz
            else sudo -n poweroff 2>/dev/null || poweroff
            fi
            ;;
        *)
            echo "power.sh: suspend|reboot|poweroff" >&2
            return 2
            ;;
    esac
}

do_action "$action"
