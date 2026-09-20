# Development

[← README](../README.md)

## Building

From WSL:

```sh
winget.exe install JRSoftware.InnoSetup --scope user   # once
make cert        # once: creates the signing certificate and trusts it on this PC; click Yes in the Windows dialog
make cert-github # once: lets the Release workflow sign with that certificate too
make test        # gofmt check, vet (Linux and Windows), tests with the race detector
make lint        # staticcheck
make installer   # signed installer in dist/ and your Downloads folder
make app         # build the installer and install it silently over the current version
```

To sign with a purchased certificate instead, change `$signArgs` in `installer/sign.ps1`. Bump `VERSION` for each release. The Makefile builds with `-buildvcs=false`, so a build doesn't depend on the state of git.

For experiments, start a trainer with `GOURDIAN_HOME=/some/folder` and its own `listen` port in that folder's `config.json`. Such a profile never touches Dota's game-state config. WSL can't reach an app listening on Windows' localhost, so run Windows-side commands with `"Gourdian.exe"`.

## Releases

**The tag is the version.** Pushing a tag `vX.Y.Z` releases X.Y.Z: the Release workflow in `.github/workflows` writes that version into `VERSION` before building, so the exe, the installer, the tarball and `gourdian version` all say the same thing no matter what the file in the tree says. Tagging in the GitHub UI therefore works as well as `make release`.

The usual way: bump `VERSION` and `pkgver` in `packaging/arch/PKGBUILD` (they have to agree, and `make test` checks it), commit, then `make release`. It runs the tests, tags the commit `v<VERSION>` and pushes it. The workflow tests that commit again, builds the installer on Windows (signed with the certificate that `make cert-github` stored in the `release` environment, which only `v*` tags can use) and the tarball and the `.deb` on Linux, and publishes them with checksums and attestations as a GitHub release. The CI workflow runs the tests and staticcheck on Linux and Windows for every push to `main`.

## Commands

| Command | What it does |
|---|---|
| `gourdian setup` | Prepare the app folder and connect Dota 2 (the installer runs this) |
| `gourdian quit` | Ask a running trainer to quit |
| `gourdian version` | Print the version |
| `gourdian run [-open] [-overlay] [-record]` | Run the trainer from a terminal instead of the app |
| `gourdian install [-dota DIR]` / `uninstall` | Add or remove only the GSI config in Dota 2 |
| `gourdian doctor` | Check the setup end to end |
| `gourdian overlay [-snapshot FILE] [-editing]` | Show the HUD, or render one frame with real transparency to a PNG |
| `gourdian stats` | Summarize trends and show where the CSVs are |
| `gourdian mmr 2450 [note]` | Log your MMR |
| `gourdian import [-n 50] [-account ID]` | Add recent matches from OpenDota |
| `gourdian simulate [-from -60] [-to 1590] [-speed N] [-random]` | Play a fake match into a running trainer |
| `gourdian replay FILE [-speed N]` | Replay a recording into a running trainer |

## How a game-state post becomes a tip

Dota posts the game state about twice a second. One request runs the whole pipeline:

1. **`POST /gsi` → `handleGSI`** (`internal/server/server.go`) parses the payload into a `gsi.State`, keeping every field it understands even if a new Dota version sends one it doesn't, and checks the token.
2. **The position** is chosen when a match starts on a different hero (`applyHeroRole`): what you played on that hero last, else your usual position on it, else a guess from OpenDota's hero roles.
3. **`engine.Update`** (`internal/coach`) runs every rule against the state and the one before it, and returns the tips that fired, the timeline samples, and a finished match when the game ends. Rules are data (`RuleSpec`: when, if, then), each wrapped in `recover`, so one bad rule can't stop the rest.
4. **Lane detection** may switch the position once the lanes are clear (`applyDetectedRole`), and says so in a tip.
5. **`deliver`** sends each tip to the dashboard's event stream, speaks it if the voice is on and the tip is important enough, and saves it. `emitTips` is the same thing for tips the trainer makes outside the engine, like the drill line.
6. **`recordMatch`**, when the game is over, saves the match and its item timings, then runs what depends on a finished game: the drill result, the MMR prompt, weekly-goal feedback, the tilt check, remembering the hero's position, and the AI review.

Everything slow hangs off that path rather than sitting in it: OpenDota lookups, the AI coach and the settings broadcast run as background work the server waits for when it shuts down (`Server.spawn`).

## Code map

| Path | Contents |
|---|---|
| `internal/gsi` | GSI payload types |
| `internal/dota` | The game's vocabulary: positions, lanes, clock formatting, map timings, shared thresholds |
| `internal/model` | The record of play: matches, timeline samples, tips, item timings, MMR, reviews, weekly goals |
| `internal/coach` | Rules engine, built-in rules, custom rule specs and fields, lane detection, personal targets |
| `internal/rules` | Rule storage (custom rules and edits to built-in ones) and templates |
| `internal/hud` | What the HUD shows, per widget |
| `internal/i18n` | Russian for the phrases the trainer builds, and a glossary of game terms |
| `internal/ai` | AI providers: Claude Code and Codex CLIs (with their installers), Anthropic API, OpenAI-compatible APIs |
| `internal/aicoach` | Coach prompts and answer schemas |
| `internal/aisvc` | Provider health, model lists and the setup wizard |
| `internal/secrets` | API keys encrypted with DPAPI |
| `internal/dotadata` | OpenDota items, heroes, builds, item timings, matches and rank, with a disk cache and rate limit |
| `internal/matchdata` | Parsed OpenDota matches folded into the statistics; history import |
| `internal/stats` | The data file (pure-Go SQLite), its migrations, queries and CSV export |
| `internal/server` | HTTP API, event stream, dashboard pages, the match pipeline, briefing, tilt check |
| `internal/overlay` | The HUD on Windows (Win32, per-pixel alpha) and Linux (X11), hotkeys, tray icon, dashboard window |
| `internal/platform` | WSL and Linux-desktop checks in one place |
| `internal/hotkey` | Shortcut parsing |
| `internal/autostart` | Starting with Windows or the Linux desktop |
| `internal/config` | Settings and where the app keeps its folder |
| `internal/speech` | Windows speech and Piper voices |
| `internal/install` | Steam and Dota discovery, GSI config |
| `internal/sim` | Simulator and recordings |
| `app.go`, `doctor.go`, `main.go` | App startup, commands and checks |
| `installer/` | Inno Setup script, signing scripts, build script, icons |
