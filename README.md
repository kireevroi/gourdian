# Dota Trainer

A live coach for Dota 2. While you play it:

- **Speaks tips** over the game: rune timings, a missing TP scroll, low HP, unspent gold, items stuck in the stash or backpack, unspent skill points, farm pace, core item timing goals, Roshan and Aegis timers, and more.
- **Draws a transparent HUD** over the game. You choose where it sits, how big and see-through it is, and which lines it shows.
- **Coaches for the position you actually play.** Before 2:30 you pick it with Ctrl+Shift+1…5, or the trainer works it out from the lane you stand in.
- **Sets personal targets** from your own match history: last hits at each checkpoint and timings for your core items.
- **Briefs you before the horn**: your record on the hero, today's targets and this week's goals.
- **Asks an AI coach** for situational advice every few minutes and when you die. The Claude Code and Codex CLIs run on your existing subscriptions; API keys and any OpenAI-compatible server work too.
- **Reviews every match** with the AI afterwards: three improvements, a focus for the next game and measurable goals for the week.
- **Runs your own rules**: switch built-in tips off, retune them, or build new ones with When / If / Then cards.
- **Records statistics as CSV files** and charts the trends.

It reads Valve's official Game State Integration feed, which only describes your own hero. It never reads game memory, injects into Dota, or sees enemy information. The HUD is a separate click-through window, as safe as a second monitor.

## Install

Run **DotaTrainer-Setup-<version>.exe**. The setup wizard installs to `%LOCALAPPDATA%\Programs\Dota Trainer` without admin rights, adds Start menu and (optionally) desktop shortcuts, can start the app when you sign in to Windows, and connects Dota 2. Setup quits a running copy before upgrading and keeps your settings and statistics. Uninstall from Windows Settings › Apps; you're asked whether to keep your statistics.

The installer and app are signed by **Dota Trainer**. The certificate is self-signed, so it's only trusted on a PC where `make cert` has been run. Other PCs show "Unknown publisher".

Requirements:

- Dota 2's launch options include `-gamestateintegration` (Steam › Dota 2 › Properties).
- Dota runs in **borderless window** mode, so the HUD can draw over it.
- For the AI coach: Claude Code or the Codex CLI logged in on Windows, or an API key. **Install and connect** on the AI page downloads both CLIs, checks them against their publishers' checksums and opens each login. They are native builds, so nothing else (no Node.js) is needed.

### Linux (Arch, EndeavourOS, CachyOS, Manjaro, Garuda)

Dota 2's native Linux client works with the trainer the same way: it reads the game over GSI, coaches, speaks, reviews and keeps statistics. Two ways to install:

- **Package** (Arch and derivatives): `cd packaging/arch && makepkg -si` builds from this source tree and installs `/usr/bin/dotatrainer`, a menu entry and the icon.
- **Release folder** (any distribution): `make linux-dist` produces `dist/dotatrainer-<version>-linux-x86_64.tar.gz`. Unpack it and run `./install.sh`, which installs into your home folder only (`~/.local/bin`, the menu entry, the icon). `./install.sh --remove` takes it out again.

Start it from the application menu. It finds Steam in `~/.local/share/Steam`, `~/.steam`, the Flatpak (`~/.var/app/com.valvesoftware.Steam`) and the Snap, writes the game-state config into Dota's folder, and keeps its data in `~/.config/dotatrainer`. Add `-gamestateintegration` to Dota's launch options as on Windows.

What's different from Windows:

- **In-game view.** The Windows HUD can't draw over games on Linux (Wayland doesn't allow it). Instead, in game press **Shift+Tab**, open Steam's web browser at `http://127.0.0.1:4570/overlay.html` and **pin** it: it stays over the game, shows the same rows as the HUD, and A−/A/A+ change the text size. The same page works on a second monitor on any system.
- **Voice** goes through speech-dispatcher (`spd-say`) or eSpeak NG, in English or Russian. Without either, tips are read by the dashboard tab. `sudo pacman -S espeak-ng` is enough.
- **Start when you log in** writes an XDG autostart entry (`~/.config/autostart/dotatrainer.desktop`), which KDE, GNOME, Xfce and the other desktops follow.
- **AI setup** installs the Linux builds of Claude Code and the Codex CLI the same way.
- API keys are kept in the data folder readable only by you; Windows encrypts them with your account instead.
- There's no tray icon: the menu entry starts the trainer and opens the dashboard, and choosing it again while it runs just opens the dashboard. `dotatrainer quit` stops it.

## Using it

The app has no window of its own; it lives in the **system tray** near the clock. Start Dota 2 (restart it if it was open during the install).

| In game | Does |
|---|---|
| **Ctrl+Shift+1…5** | Pick your position, until 2:30 |
| **Ctrl+Shift+F10** | Move and resize the HUD: drag it, scroll for size, Ctrl+scroll for background, click Done |
| **Ctrl+Shift+F11** | Open the dashboard |
| **Ctrl+Shift+F9** | Hide or show the HUD |

The F-key shortcuts can be changed on the Settings page. **Left-click** the tray icon for the dashboard; **right-click** it for stats, HUD layout, hiding the HUD, recording, the app folder and Quit.

### Position

Tips, targets and the AI coach depend on your position. When a match starts on a hero, the trainer uses the position you played on it last time, then your most common position on it in your history, then a guess from the hero's roles. The HUD shows the choice until 2:30. If you don't pick one, the trainer watches which lane your hero stands in from 0:45 to 2:30 (up to 5:00 if it started late), and switches when that doesn't fit, saying so. Mid lane means mid. In a side lane, early wards or a support position mean the support position, otherwise the core one. A position you pick yourself is never overridden.

### Settings live on the dashboard

The dashboard is at http://127.0.0.1:4570 and opens in its own window. Its pages:

- **Live**: the tip feed, a briefing before the horn, hero stats, farm pace against your target, core item goals, timers, inventory, the popular build with what to buy next, this week's goals, habits to fix, recent matches and the last review. Position buttons and a voice switch sit at the top.
- **Stats**: charts and tables over your CSV statistics, weekly goals and every review.
- **Rules**: every tip rule, built-in and your own.
- **HUD**: widgets, order, look and position, with a live preview.
- **AI coach**: connections, who answers live tips and reviews, and what the coach knows about you.
- **Settings**: position per hero, voice, recording, match history import, start with Windows, break reminders, hotkeys and folders.

### HUD

The HUD shows only during a match, never takes focus and lets clicks through to the game. Its content comes from widgets you order and switch on the HUD page:

| Widget | Shows |
|---|---|
| Alerts | The latest tip in large type for a few seconds; choose every tip, warnings and up, or urgent only, and whether AI tips show |
| Position | Before 2:30, the position you're coached as |
| Briefing | Before the horn: your record on the hero, the last-hit target, core item goals and unfinished weekly goals |
| Focus | Your focus from the last review, before the horn and while dead |
| While dead | Respawn time and gold to spend |
| Timers | Runes, neutral items, Roshan and Tormentor, stack pulls; how many, which kinds, and how far ahead |
| Last-hit pace | Your last hits against the pace to your target |
| Next item | The next item in the popular build and whether you can buy it |
| Item goal | The next core item and its timing goal, from five minutes before it |
| Stats line | K/D/A, GPM and last hits (off by default) |

Look settings: width, text size, background opacity (0% shows text only), whole-HUD opacity and a text shadow. The HUD is drawn with real transparency, so backgrounds fade without blurring the text.

### Personal targets

- **Last hits**: for carry, mid and offlane, each checkpoint (5, 10, 15, 20 and 30 minutes) aims 10% above your median over your last 10 matches on that hero and position, once you have 3 of them. Otherwise it uses a fixed table for the position. Imported matches count.
- **Core items**: two items, your usual first big items on the hero if you have 3 or more matches with item data, otherwise the popular build's first mid- or late-game items costing 2,000 gold or more. The goal is the earliest OpenDota timing bucket that holds a tenth of games and wins at least as often as the item does overall, or a minute before your own median timing, whichever is later.
- The item goal rule gives a heads-up 2 minutes before a goal, marks the item late a minute after it (unless you finished a different item of similar cost instead), and says how your timing went.

### Weekly goals and break reminders

Each match review sets one to three goals for the week from measurable metrics: last hits at a checkpoint, deaths, GPM, XPM, kills plus assists, or how often a habit's warning fired. After every real match the trainer says which goals you met, and a goal is done after five matches that meet it. The briefing, Live page and Stats page show progress.

After two losses in a row, three of the last four games lost, or an MMR drop of 50 or more within a session (matches less than 90 minutes apart), the trainer suggests a break. If you queue again within 10 minutes, it gives one calm-down reminder at the start. Turn this off under Settings › Break reminders.

## Rules

The Rules page lists every rule with how often it fired in your last 10 matches.

- **Built-in rules** can be switched off, limited to certain positions, given another severity, made silent or always spoken, and retuned: how long a problem must last before a warning, how often it repeats, the HP percentage, the gold threshold and so on. **Reset to default** undoes the changes.
- **Your own rules** are cards:
  - **When**: while the conditions hold (for some seconds), the moment they become true, when something happens (the horn, you die or respawn, level up, get a kill, get an item, Roshan dies, the Aegis is picked up), or at game times (first, every, until, and how early to warn).
  - **If**: all or any of a list of conditions on about 50 game values: clock, gold, HP and mana, level, K/D/A, last hits against pace, items owned, in the stash or ready, charges, abilities and ultimate ready, buyback, being in base, your position, and more.
  - **Then**: the text to show, what to say (or nothing), the severity, whether it counts as a mistake in your habits, and how often it may repeat. Text can include values like `{gold}`, `{clock}`, `{respawn}` or `{item_name:black_king_bar}`.
- While a match runs, each condition shows its current value and whether it holds. **Run on recording** replays a recorded match through the rule and lists every time it would have fired.
- Start from templates (Save for BKB, use Magic Wand when low, low mana, Lotus pool, shop while dead, ultimate is up, idle in base), duplicate rules, and export or import them as JSON.

Rules are stored in `trainer.data` with everything else. Built-in defaults come with the app, so updates can improve them unless you changed that rule.

## AI coach

The AI coach page lists every provider with its status and a fix button:

| Provider | Uses | Setup |
|---|---|---|
| Claude Code | your Claude subscription | Install and connect (downloads it, then opens `claude auth login`); **Switch account** signs out and signs in as someone else |
| OpenAI Codex CLI | your ChatGPT subscription | Install and connect (downloads it, then opens `codex login`); **Switch account** signs out and signs in as someone else |
| Anthropic API | an API key, billed per request | paste the key |
| OpenAI, OpenRouter, Google Gemini API, Groq, DeepSeek | an API key, billed per request | paste the key |
| Custom OpenAI-compatible URL | your own server, such as Ollama or LM Studio | enter the URL and, if needed, a key |

Choose a provider, model and thinking effort for live tips and for reviews separately, plus an optional fallback that answers while the main one is logged out or out of usage. **Test** sends a sample request and shows the answer and how long it took. API keys are encrypted for your Windows account (DPAPI) in `secrets.json` and never shown again in full.

**Check** on a provider forgets an old usage limit or logout before asking again, so a fresh account is picked up straight away. If a provider is logged out, rejects its key or hits a usage limit, the coach pauses instead of failing silently: the dashboard shows a banner with a login button, and the next match starts with one spoken notice. A logged-out provider is re-checked every 5 minutes and resumes by itself; a usage limit pauses it for 10 minutes. Reviews that couldn't be written are retried once it works again.

What gets sent: your hero's live stats, items, timers and recent tips, your per-minute timeline, your averages and recurring mistakes, your MMR log, this week's goals and your "About you" text. After a real match the trainer waits up to 30 minutes for OpenDota to parse the replay, so the review also gets your lane, lane opponents, both teams' heroes, percentiles against other players of the hero and item timings. Nothing else about other players is available to send.

Claude CLI calls run with tools, MCP servers and settings files off. Codex runs read-only in an empty folder, with the answer's shape enforced by a JSON schema.

Installs come from the publishers themselves: Claude Code from `downloads.claude.ai`, verified against the SHA-256 in its release manifest and then run with `claude install`; the Codex CLI from its GitHub release, saved in the app folder's `tools\`. Both are single native programs, so the trainer never installs Node.js or touches anything else on the PC. The Gemini CLI is not offered, because it is the one that would need Node.js; the Google Gemini API key works the same as any other key.

## Statistics

Everything lives in `trainer.data` in the app folder: matches, per-minute samples, tips, item timings, MMR, reviews, goals and your rules. **Settings › Export CSV** writes it all to the `stats\` folder for spreadsheets:

| File | One row per | Useful for |
|---|---|---|
| `matches.csv` | match | results, KDA, GPM/XPM, last hits at checkpoints, death times, each habit's warnings (`mistakes_<rule>`), rank, and after OpenDota parses it: lane, net worth, damage, wards, stacks, teamfight participation, percentiles, enemy heroes |
| `timeline.csv` | game minute | gold, last hits, GPM, level and deaths over time |
| `tips.csv` | tip | what the coach told you and when |
| `items.csv` | core item bought | item timings, seen live (`gsi`) or from OpenDota |
| `mmr.csv` | MMR entry | your rating over time |
| `reviews.csv` | reviewed match | the AI's summary, improvements and next-game focus |
| `goals.csv` | weekly goal | the goals reviews set |

`source` in `matches.csv` is `live`, `opendota` (imported), `practice` (lobby and bot games, which Dota reports as match 0) or `sim`. Practice and simulated matches are left out of trends, habits and AI history unless you tick them in.

- **Stats page**: MMR, rolling win rate, last hits at 10:00, GPM, deaths, last-hit curves, mistakes per match, weekly goals, reviews and a hero table.
- **Import**: Settings › Match history adds your last 50 matches from OpenDota (your Friend ID is learned in your first match). OpenDota needs Expose Public Match Data turned on in Dota.
- **Recordings**: every match's game data is saved to `recordings\` (the newest 20 are kept), so rules can be tested on real games.

## Patch timings

Map timings are built into each version; updating the app brings new patch timings. This version follows **patch 7.41f**, checked against Valve's patch notes (the `dota2.com/datafeed/patchnotes` feed) and Liquipedia:

| What | When | Since |
|---|---|---|
| Bounty runes | 0:00, then every 4:00 | 7.38 (was every 3:00) |
| Water runes | 2:00 and 4:00 | |
| Power runes | 6:00, then every 2:00 | |
| Shrines of Wisdom (replaced wisdom runes) | every 7:00; stand in yours for 3 s, an enemy reverses the countdown | 7.38, 7.41 |
| Lotus pools | a lotus every 3:00, six at most | |
| Tormentor | 20:00, then 10:00 after it dies | 7.39 (was 15:00) |
| Neutral items | crafted from Madstone; tiers at 5, 15, 25, 35 and 60 minutes, tier 1 costs 5 Madstone | 7.38 |
| Roshan | respawns 8–11 minutes after he dies; the Aegis lasts 5:00 | |
| Skill points | one per level; talents have their own points at 10, 15, 20, 25 and 27–30 | 7.40 |

"Keep buyback gold from 30:00" is coaching advice, not a game rule.

The AI coach is given the item build professional players use on your hero (OpenDota's item popularity, "analyzed from professional games") and is told to recommend only items from it.

## Troubleshooting

- **Dashboard says "Waiting for Dota 2"**: the Setup card lists what's missing, with a Fix button for Dota's game-state config. Restart Dota after fixing it.
- **No HUD over the game**: Dota must be in borderless window mode. If a hotkey is taken by another program, Settings says so; pick another one.
- **AI coach paused**: follow the banner, or open the AI coach page and click Check.
- **Something went wrong**: read `logs\trainer.log` in the app folder, or run `"Dota Trainer.exe" doctor` from a terminal there.
- **Coaching in another language**: add "Answer in Russian" (or any language) under "How the coach should talk to you". The AI's tips and reviews follow it; built-in tips stay in English, and Windows only speaks languages with an installed voice.
- **Left a game early?** Matches that stop sending updates for 3 minutes are still recorded, with the result marked unknown.

## Building

From WSL:

```sh
winget.exe install JRSoftware.InnoSetup --scope user   # once
make cert        # once: creates the signing certificate; click Yes in the Windows dialog
make test        # gofmt check, vet (Linux and Windows), tests with the race detector
make lint        # staticcheck
make installer   # signed installer in dist/ and your Downloads folder
make app         # build the installer and install it silently over the current version
```

To sign with a purchased certificate instead, change `$signArgs` in `installer/sign.ps1`. Bump `VERSION` for each release. `go build` needs `-buildvcs=false` here because the home directory is a root-owned git repository; the Makefile passes it.

For experiments, start a trainer with `DOTATRAINER_HOME=/some/folder` and its own `listen` port in that folder's `config.json`. Such a profile never touches Dota's game-state config. WSL can't reach an app listening on Windows' localhost, so run Windows-side commands with `"Dota Trainer.exe"`.

### Commands

| Command | What it does |
|---|---|
| `dotatrainer setup` | Prepare the app folder and connect Dota 2 (the installer runs this) |
| `dotatrainer quit` | Ask a running trainer to quit |
| `dotatrainer version` | Print the version |
| `dotatrainer run [-open] [-overlay] [-record]` | Run the trainer from a terminal instead of the app |
| `dotatrainer install [-dota DIR]` / `uninstall` | Add or remove only the GSI config in Dota 2 |
| `dotatrainer doctor` | Check the setup end to end |
| `dotatrainer overlay [-snapshot FILE] [-editing]` | Show the HUD, or render one frame with real transparency to a PNG |
| `dotatrainer stats` | Summarize trends and show where the CSVs are |
| `dotatrainer mmr 2450 [note]` | Log your MMR |
| `dotatrainer import [-n 50] [-account ID]` | Add recent matches from OpenDota |
| `dotatrainer simulate [-from -60] [-to 1590] [-speed N] [-random]` | Play a fake match into a running trainer |
| `dotatrainer replay FILE [-speed N]` | Replay a recording into a running trainer |

### Code map

| Path | Contents |
|---|---|
| `internal/gsi` | GSI payload types |
| `internal/coach` | Rules engine, built-in rules, custom rule specs and fields, lane detection, targets, snapshot |
| `internal/rules` | `rules.json` storage and rule templates |
| `internal/hud` | What the HUD shows, per widget |
| `internal/ai` | AI providers: Claude Code and Codex CLIs (with their installers), Anthropic API, OpenAI-compatible APIs |
| `internal/aicoach` | Coach prompts and answer schemas |
| `internal/secrets` | API keys encrypted with DPAPI |
| `internal/dotadata` | OpenDota items, heroes, builds, item timings, matches and rank, with disk cache |
| `internal/matchdata` | Parsed OpenDota matches into stats; history import |
| `internal/stats` | CSV storage, weekly goals |
| `internal/server` | HTTP API, event stream, dashboard pages, AI health, briefing, tilt check |
| `internal/overlay` | Win32 HUD (per-pixel alpha), hotkeys, tray icon, dashboard window |
| `internal/hotkey` | Shortcut parsing |
| `internal/config` | Settings, built-in patch timings |
| `internal/speech` | Windows speech |
| `internal/install` | Steam and Dota discovery, GSI config |
| `internal/sim` | Simulator and recordings |
| `app.go`, `doctor.go`, `main.go` | App startup, commands and checks |
| `installer/` | Inno Setup script, signing scripts, build script, icons |
