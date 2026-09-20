# Linux

[← README](../README.md)

Dota 2's native Linux client works with the trainer the same way: it reads the game over GSI, coaches, speaks, reviews and keeps statistics. Tested on Arch, EndeavourOS, CachyOS, Manjaro and Garuda.

## Install

- **Package** (Debian, Ubuntu and derivatives): `sudo apt install ./gourdian_<version>_amd64.deb` from the Releases page installs `/usr/bin/gourdian`, a menu entry and the icon; `sudo apt remove gourdian` takes them out again. `make linux-deb` builds the same package from this source tree, `DEBARCH=arm64 make linux-deb` one for ARM.
- **Package** (Arch and derivatives): `cd packaging/arch && makepkg -si` builds from this source tree and installs the same files.
- **Release folder** (any distribution): `make linux-dist` produces `dist/gourdian-<version>-linux-x86_64.tar.gz`. Unpack it and run `./install.sh`, which installs into your home folder only (`~/.local/bin`, the menu entry, the icon). `./install.sh --remove` takes it out again.

Start it from the application menu. It finds Steam in `~/.local/share/Steam`, `~/.steam`, the Flatpak (`~/.var/app/com.valvesoftware.Steam`) and the Snap, writes the game-state config into Dota's folder, and keeps its data in `~/.config/dotatrainer` (the app's name before 1.5). Add `-gamestateintegration` to Dota's launch options as on Windows.

## What's different from Windows

- **In-game view.** The Windows HUD can't draw over games on Linux (Wayland doesn't allow it). Instead, in game press **Shift+Tab**, open Steam's web browser at `http://127.0.0.1:4570/overlay.html` and **pin** it: it stays over the game, shows the same rows as the HUD, and A−/A/A+ change the text size. The same page works on a second monitor on any system.
- **Voice** goes through speech-dispatcher (`spd-say`) or eSpeak NG, in English or Russian. Without either, tips are read by the dashboard tab. `sudo apt install espeak-ng` (or `sudo pacman -S espeak-ng`) is enough.
- **Start when you log in** writes an XDG autostart entry (`~/.config/autostart/gourdian.desktop`), which KDE, GNOME, Xfce and the other desktops follow.
- **AI setup** installs the Linux builds of Claude Code and the Codex CLI the same way.
- API keys are kept in the data folder readable only by you; Windows encrypts them with your account instead.
- There's no tray icon: the menu entry starts the trainer and opens the dashboard, and choosing it again while it runs just opens the dashboard. `gourdian quit` stops it.
