#!/bin/sh
# Installs Dota Trainer for the current user: no root, nothing outside your home folder.
# Run it from the unpacked release folder. ./install.sh --remove takes it out again.
set -eu

bin="${XDG_BIN_HOME:-$HOME/.local/bin}"
apps="${XDG_DATA_HOME:-$HOME/.local/share}/applications"
icons="${XDG_DATA_HOME:-$HOME/.local/share}/icons/hicolor/256x256/apps"
here="$(cd "$(dirname "$0")" && pwd)"

if [ "${1:-}" = "--remove" ]; then
	rm -f "$bin/dotatrainer" "$apps/dotatrainer.desktop" "$icons/dotatrainer.png" \
		"${XDG_CONFIG_HOME:-$HOME/.config}/autostart/dotatrainer.desktop"
	echo "Removed Dota Trainer. Your matches and settings stay in ${XDG_CONFIG_HOME:-$HOME/.config}/dotatrainer."
	exit 0
fi

mkdir -p "$bin" "$apps" "$icons"
install -m 755 "$here/dotatrainer" "$bin/dotatrainer"
install -m 644 "$here/dotatrainer.desktop" "$apps/dotatrainer.desktop"
install -m 644 "$here/dotatrainer.png" "$icons/dotatrainer.png"
command -v update-desktop-database >/dev/null && update-desktop-database "$apps" 2>/dev/null || true

echo "Installed Dota Trainer to $bin/dotatrainer."
case ":$PATH:" in *":$bin:"*) ;; *) echo "Note: $bin is not on your PATH; the application menu entry works regardless." ;; esac
if ! command -v pw-play >/dev/null && ! command -v paplay >/dev/null && ! command -v aplay >/dev/null; then
	echo "For spoken tips install something to play sound with: sudo pacman -S pipewire (or alsa-utils)."
	echo "The trainer downloads its natural voice (Piper, about 90 MB) the first time it runs."
fi
echo "Start it from your application menu, then add -gamestateintegration to Dota 2's launch options in Steam."
echo "The HUD shows over Dota in borderless window mode (Settings > Video > Display mode)."
