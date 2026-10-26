#!/bin/sh
# lnf.sh — послать панели LNF команду через FIFO.
#
#   lnf.sh fan               раскрыть/свернуть веер действий
#   lnf.sh apps              открыть/закрыть «Приложения» (с поиском)
#   lnf.sh clip              буфер обмена
#   lnf.sh shot              скриншот (задержка и зона)
#   lnf.sh walls             обои
#   lnf.sh toggle NAME       открыть/закрыть группу NAME
#   lnf.sh set NAME VALUE    задать значение опроса (OSD без опроса)
#   lnf.sh notify ТЕКСТ      показать сообщение в OSD
#
# Пишем одной строкой в FIFO панели. Открываем на запись через timeout:
# если панель не запущена (читателя нет), команда не зависает.
f="${XDG_CACHE_HOME:-$HOME/.cache}/lnf/lnf.fifo"
[ -p "$f" ] || exit 1
printf '%s\n' "$*" | timeout 1 sh -c 'cat > "$1"' sh "$f" 2>/dev/null
