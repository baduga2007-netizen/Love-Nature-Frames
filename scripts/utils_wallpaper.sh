#!/bin/sh
# LNF: утилиты и переключение обоев из каталога.
# Формат строки меню: подпись <TAB> команда <TAB> значок <TAB> цвет [<TAB> ПКМ]
g_img=''
g_stop=''
g_mute=''
g_shot=''
g_lock=''
g_relaunch=''

# каталог обоев: $LNF_WALLS или первый подходящий
WALLS="${LNF_WALLS:-}"
if [ -z "$WALLS" ]; then
    for d in "$HOME/Изображения" "$HOME/Pictures/Wallpapers" "$HOME/Pictures" \
             "$HOME/wallpapers" "$HOME/.local/share/wallpapers"; do
        [ -d "$d" ] && { WALLS="$d"; break; }
    done
fi

if [ -n "$WALLS" ] && [ -d "$WALLS" ]; then
    cache="$HOME/.cache/lnf-walls"
    mkdir -p "$cache" 2>/dev/null
    find "$WALLS" -maxdepth 2 -type f \
        \( -iname '*.png' -o -iname '*.jpg' -o -iname '*.jpeg' -o -iname '*.webp' \) 2>/dev/null |
        sort | head -16 | while IFS= read -r f; do
            icon=""
            if command -v magick >/dev/null 2>&1; then
                key=$(printf '%s' "$f" | md5sum | cut -d' ' -f1)
                icon="$cache/$key.png"
                [ -f "$icon" ] || magick "$f" -thumbnail 96x96^ -gravity center -extent 96x96 "$icon" 2>/dev/null
                [ -f "$icon" ] || icon=""
            elif [ "${f##*.}" = "png" ]; then
                icon="$f"
            fi
            printf '%s\t%s\t%s\t\n' "$(basename "$f")" "$HOME/LNF/scripts/set_wallpaper.sh \"$f\"" "$icon"
        done
fi

# прочие утилиты
printf '%s\t%s\t%s\t\n' "Stop wallpaper" 'printf "quit\n" > "${XDG_RUNTIME_DIR:-/tmp}/lnf-wallpaper.fifo"' "$g_stop"
printf '%s\t%s\t%s\t\n' "Mute sound" 'wpctl set-mute @DEFAULT_AUDIO_SINK@ toggle' "$g_mute"
printf '%s\t%s\t%s\t\n' "Screenshot" "$HOME/LNF/scripts/wm_control.sh screenshot" "$g_shot"
printf '%s\t%s\t%s\t\n' "Lock" "$HOME/LNF/scripts/wm_control.sh lock" "$g_lock"
printf '%s\t%s\t%s\t\n' "Restart LNF" "$HOME/LNF/scripts/restart_session.sh" "$g_relaunch"
