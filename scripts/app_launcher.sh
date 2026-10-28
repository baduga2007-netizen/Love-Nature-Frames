#!/bin/sh
# LNF drun — список приложений для меню-панели.
# Строка: подпись <TAB> команда <TAB> значок <TAB> цвет плитки.
#
# «значок» — либо путь к PNG (рисуется как настоящая иконка),
# либо глиф шрифта (рисуется белым в цветной плитке «цвет плитки»).
#
# Список кэшируется: собирать его каждый раз дорого (find по всему
# /usr/share/icons + rsvg для SVG), а открытие лаунчера должно быть мгновенным.
# Кэш живёт LNF_DRUN_TTL секунд (по умолчанию 300) и сбрасывается, когда
# меняется каталог приложений (поставили/удалили программу).

CACHE="${XDG_CACHE_HOME:-$HOME/.cache}/lnf-icons"
mkdir -p "$CACHE"
LIST="$CACHE/drun.list"
INDEX="$CACHE/drun.index"
TTL="${LNF_DRUN_TTL:-300}"

APPDIRS="/usr/share/applications $HOME/.local/share/applications $HOME/.local/share/flatpak/exports/share/applications"

fresh() { [ -f "$1" ] && [ $(( $(date +%s) - $(stat -c %Y "$1" 2>/dev/null || echo 0) )) -lt "$TTL" ]; }
# каталог приложений новее списка — что-то установили/удалили
apps_changed() {
    [ -f "$LIST" ] || return 0
    for d in $APPDIRS; do
        [ -d "$d" ] && [ "$d" -nt "$LIST" ] && return 0
    done
    return 1
}

if fresh "$LIST" && ! apps_changed; then
    cat "$LIST"
    exit 0
fi

# Кэш путей к иконкам (find по /usr/share/icons — самое дорогое место).
if ! fresh "$INDEX"; then
    # find может вернуть ненулевой код на недоступных каталогах — это не
    # повод терять индекс, поэтому mv не завязываем на его статус.
    find /usr/share/icons /usr/share/pixmaps "${XDG_DATA_HOME:-$HOME/.local/share}/icons" \
         -type f \( -name '*.png' -o -name '*.svg' \) 2>/dev/null > "$INDEX.tmp"
    mv "$INDEX.tmp" "$INDEX"
fi

# icon_path — настоящая PNG-иконка по имени из Icon=.
# Сначала ищем PNG (самый крупный), затем SVG (растеризуем в кэш). Пусто — если нет.
icon_path() {
    n=$1
    case "$n" in
        /*) [ -f "$n" ] && line=$n || line='' ;;
        *)  line=$(awk -v n="$n" '
                BEGIN { best=-1 }
                { p=$0
                  if      (p ~ ("/"n"\\.png$")) ext=1
                  else if (p ~ ("/"n"\\.svg$")) ext=0
                  else next
                  sz=0; k=split(p,a,"/")
                  for (i=1;i<=k;i++) if (a[i] ~ /^[0-9]+x[0-9]+$/) { split(a[i],b,"x"); sz=b[1]+0 }
                  s=ext*1000000+sz
                  if (s>best) { best=s; ln=p } }
                END { if (ln!="") print ln }' "$INDEX") ;;
    esac
    case "$line" in
        *.png) printf '%s' "$line" ;;
        *.svg)
            mkdir -p "$CACHE"
            out="$CACHE/$(printf '%s' "$n" | tr '/ ' '__').png"
            if [ ! -f "$out" ]; then
                rsvg-convert -w 96 -h 96 "$line" -o "$out" 2>/dev/null ||
                magick -background none "$line" -resize 96x96 "$out" 2>/dev/null || true
            fi
            [ -f "$out" ] && printf '%s' "$out" || printf '' ;;
        *) printf '' ;;
    esac
}

build() {
    for d in /usr/share/applications \
             "$HOME/.local/share/applications" \
             "$HOME/.local/share/flatpak/exports/share/applications"; do
        [ -d "$d" ] || continue
        for f in "$d"/*.desktop; do
            [ -f "$f" ] || continue
            grep -qi '^NoDisplay=true' "$f" && continue
            grep -qi '^Hidden=true' "$f" && continue
            name=$(grep -m1 '^Name=' "$f" | cut -d= -f2-)
            exec=$(grep -m1 '^Exec=' "$f" | cut -d= -f2-)
            icon=$(grep -m1 '^Icon=' "$f" | cut -d= -f2-)
            [ -z "$name" ] && continue
            [ -z "$exec" ] && continue
            exec=$(printf '%s' "$exec" | sed 's/ *%[fFuUdDnNickvm]//g')

            img=$(icon_path "$icon")
            if [ -n "$img" ]; then
                printf '%s\t%s\t%s\t\n' "$name" "$HOME/LNF/scripts/wm_control.sh spawn $exec" "$img"
                continue
            fi

            lc=$(printf '%s' "$icon" | tr 'A-Z' 'a-z')
            case "$lc" in
                *terminal*|*konsole*|*foot*|*alacritty*|*xterm*|*kitty*|*console*) g='\357\204\240'; c='#12A4B8' ;;
                *code*|*dev*|*editor*|*vim*|*emacs*|*sublime*)                    g='\357\204\241'; c='#4285F4' ;;
                *firefox*|*chrom*|*browser*|*web*|*epiphany*)                     g='\357\202\254'; c='#4285F4' ;;
                *folder*|*file*|*thunar*|*nautilus*|*archive*|*zip*)              g='\357\201\273'; c='#E8710A' ;;
                *music*|*audio*|*spotify*|*rhythmbox*|*audacious*|*sound*)        g='\357\200\201'; c='#34A853' ;;
                *video*|*vlc*|*mpv*|*player*|*film*)                              g='\357\200\275'; c='#E8710A' ;;
                *game*|*steam*|*lutris*|*wine*|*minecraft*)                       g='\357\204\233'; c='#EA4335' ;;
                *mail*|*thunderbird*|*evolution*)                                 g='\357\203\240'; c='#EA4335' ;;
                *image*|*photo*|*gimp*|*krita*|*paint*|*viewer*)                  g='\357\200\276'; c='#A142F4' ;;
                *chat*|*discord*|*telegram*|*signal*|*message*|*matrix*)          g='\357\202\206'; c='#A142F4' ;;
                *setting*|*preference*|*control*|*tweak*)                         g='\357\200\223'; c='#5F6368' ;;
                *office*|*writer*|*calc*|*document*|*pdf*)                        g='\357\205\234'; c='#4285F4' ;;
                *network*|*wifi*|*vpn*|*ethernet*)                                g='\357\207\253'; c='#34A853' ;;
                *)                                                                g='\357\200\211'; c='#34A853' ;;
            esac
            printf '%s\t%s\t%b\t%s\n' "$name" "$HOME/LNF/scripts/wm_control.sh spawn $exec" "$g" "$c"
        done
    done
}

build | LC_ALL=C sort -u > "$LIST.tmp" && mv "$LIST.tmp" "$LIST"
cat "$LIST"
