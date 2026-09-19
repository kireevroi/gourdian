Gourdian
========

Start:   the "Gourdian" shortcut on the desktop or in the Start menu.
         It runs in the system tray (bottom-right corner, near the clock).
Tray:    left-click opens the dashboard; right-click for stats, the HUD, recording and Quit.

In game:
  Ctrl+Shift+1..5   pick your position (until 2:30)
  Ctrl+Shift+F10    move and resize the HUD (scroll for size, Ctrl+scroll for background)
  Ctrl+Shift+F11    open the dashboard
  Ctrl+Shift+F9     hide or show the HUD

All settings are on the dashboard: http://127.0.0.1:4570
Dota 2 must run in borderless window mode for the HUD to show over the game.

What's in this folder
  Gourdian.exe       the app
  trainer.data       your matches, tips, reviews, goals and rules
  config.json        settings (change them on the dashboard)
  secrets.json       AI API keys, encrypted for your Windows account
  stats\             CSV copies for spreadsheets (Settings > Export CSV)
  recordings\        each match's game data, for testing rules
  logs\              trainer.log from the last run, trainer.prev.log from the one before
  cache\             hero, item, build and timing data from OpenDota
  LICENSE.txt        Gourdian's license (MIT)
  THIRD_PARTY_NOTICES.txt   the licenses of the libraries and font it includes

Commands (run from a terminal in this folder)
  "Gourdian.exe" doctor    check the setup
  "Gourdian.exe" stats     trend summary

Uninstall from Windows Settings > Apps. You'll be asked whether to keep your statistics.
