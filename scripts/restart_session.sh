#!/bin/sh
# ============================================================
# restart.sh — перезапуск LNF.
#
# 1. Если панелью управляет runit (sv видит сервис lnf) — перезапускаем
#    через супервизор: sv restart lnf.
# 2. Если есть пользовательский runsvdir (~/.config/love-nature/service) —
#    перезапускаем оттуда.
# 3. Иначе — руками: убить и запустить заново.
# ============================================================

PATH="${HOME}/.local/bin:/usr/local/bin:/usr/bin:/bin:${PATH:-}"
export PATH

USER_SVDIR="$HOME/.config/love-nature/service"

if command -v sv >/dev/null 2>&1; then
    # системный сервис
    if sv status lnf >/dev/null 2>&1; then
        exec sv restart lnf
    fi
    # пользовательский каталог сервисов
    if [ -d "$USER_SVDIR/lnf" ]; then
        SVDIR="$USER_SVDIR" sv restart lnf >/dev/null 2>&1 && exit 0
    fi
fi

# Ручной перезапуск: панель + демон обоев (оба называются lnf)
pkill -x lnf 2>/dev/null
sleep 0.3
cd "$HOME/LNF" || exit 1
setsid ./lnf >/dev/null 2>&1 &
setsid ./lnf wallpaperd >/dev/null 2>&1 &
exit 0
