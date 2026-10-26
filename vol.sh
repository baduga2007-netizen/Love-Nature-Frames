#!/bin/sh
# LNF vol.sh — изменить громкость и сразу показать OSD, без опроса.
# Использование: vol.sh up | down | mute | mic-mute
# Вешается на клавиши XF86Audio* в niri вместо голого wpctl.
HOME_DIR="${HOME:-/home/larper}"

case "$1" in
    up)       wpctl set-volume --limit 1.0 @DEFAULT_AUDIO_SINK@ 5%+ ;;
    down)     wpctl set-volume @DEFAULT_AUDIO_SINK@ 5%- ;;
    mute)     wpctl set-mute @DEFAULT_AUDIO_SINK@ toggle ;;
    mic-mute) wpctl set-mute @DEFAULT_AUDIO_SOURCE@ toggle ;;
    *)        echo "vol.sh: up|down|mute|mic-mute" >&2; exit 2 ;;
esac

# Показать текущее значение в OSD панели (одна короткая команда, без опроса).
val=$("$HOME_DIR/LNF/osd.sh") || exit 0
[ -n "$val" ] && "$HOME_DIR/LNF/lnf.sh" set vol "$val"
exit 0
