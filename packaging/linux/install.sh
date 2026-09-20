#!/bin/sh
# Installs Gourdian for the current user: no root, nothing outside your home folder.
# Run it from the unpacked release folder. ./install.sh --remove takes it out again.
set -eu

bin="${XDG_BIN_HOME:-$HOME/.local/bin}"
apps="${XDG_DATA_HOME:-$HOME/.local/share}/applications"
icons="${XDG_DATA_HOME:-$HOME/.local/share}/icons/hicolor/256x256/apps"
here="$(cd "$(dirname "$0")" && pwd)"

if [ "${1:-}" = "--remove" ]; then
	rm -f "$bin/gourdian" "$apps/gourdian.desktop" "$icons/gourdian.png" \
		"${XDG_CONFIG_HOME:-$HOME/.config}/autostart/gourdian.desktop"
	echo "Removed Gourdian. Your matches and settings stay in ${XDG_CONFIG_HOME:-$HOME/.config}/dotatrainer."
	exit 0
fi

mkdir -p "$bin" "$apps" "$icons"
# Before 1.5 the app was called Dota Trainer: take its files away, and move a login entry over.
rm -f "$bin/dotatrainer" "$apps/dotatrainer.desktop" "$icons/dotatrainer.png"
autostart="${XDG_CONFIG_HOME:-$HOME/.config}/autostart"
if [ -f "$autostart/dotatrainer.desktop" ]; then
	sed 's,dotatrainer,gourdian,g; s,Dota Trainer,Gourdian,g' "$autostart/dotatrainer.desktop" > "$autostart/gourdian.desktop"
	rm -f "$autostart/dotatrainer.desktop"
fi
install -m 755 "$here/gourdian" "$bin/gourdian"
install -m 644 "$here/gourdian.desktop" "$apps/gourdian.desktop"
install -m 644 "$here/gourdian.png" "$icons/gourdian.png"
command -v update-desktop-database >/dev/null && update-desktop-database "$apps" 2>/dev/null || true

echo "Installed Gourdian to $bin/gourdian."
case ":$PATH:" in *":$bin:"*) ;; *) echo "Note: $bin is not on your PATH; the application menu entry works regardless." ;; esac
if ! command -v pw-play >/dev/null && ! command -v paplay >/dev/null && ! command -v aplay >/dev/null; then
	echo "For spoken tips install something to play sound with: PipeWire or alsa-utils, from your package manager."
	echo "The trainer downloads its natural voice (Piper, about 90 MB) the first time it runs."
fi
echo "Start it from your application menu, then add -gamestateintegration to Dota 2's launch options in Steam."
echo "The HUD shows over Dota in borderless window mode (Settings > Video > Display mode)."
