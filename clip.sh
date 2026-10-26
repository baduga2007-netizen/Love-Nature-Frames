#!/bin/sh
# LNF clipboard — история буфера обмена из cliphist.
# Строка меню: подпись <TAB> команда <TAB> значок <TAB> цвет <TAB> ПКМ-команда
# Первой строкой — «очистить всё»; ПКМ по записи удаляет только её.
printf '%s\t%s\t%s\t\n' "Clear all" 'cliphist wipe' ''
cliphist list 2>/dev/null | head -60 |
while IFS="$(printf '\t')" read -r id rest; do
    [ -n "$id" ] || continue
    rest=$(printf '%s' "$rest" | tr '\t\n' '  ')
    case "$rest" in
        '[[ binary data'*) label='(image)'; g='' ;;
        *) label=$(printf '%s' "$rest" | cut -c1-70); [ -z "$label" ] && label='(empty)'; g='' ;;
    esac
    cmd="cliphist decode $id | wl-copy"
    alt="cliphist delete $id"
    printf '%s\t%s\t%s\t\t%s\n' "$label" "$cmd" "$g" "$alt"
done
