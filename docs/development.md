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

Bump `VERSION` (and `pkgver` in `packaging/arch/PKGBUILD`), commit, then `make release`. It runs the tests, tags the commit `v<VERSION>` and pushes it; the Release workflow in `.github/workflows` tests it again, builds the installer on Windows (signed with the certificate that `make cert-github` stored in the `release` environment, which only `v*` tags can use) and the tarball on Linux, and publishes them with checksums and attestations as a GitHub release. The CI workflow runs the tests on Linux and Windows for every push to `main`.

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

## Code map

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
