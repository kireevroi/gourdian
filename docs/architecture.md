# Architecture

[← README](../README.md)

Gourdian is about 22,000 lines of Go across 25 packages under `internal/`, plus a `main` package at the repository root. This page describes what that structure gets right, where it has stopped keeping up, and the order in which to fix it.

## What holds up

**The dependency graph is acyclic and it layers.** Every package sits at a definite depth, and nothing reaches sideways:

| Depth | Packages | Imports from `internal/` |
|---|---|---|
| 0 | `buildinfo` `dota` `gsi` `hidewin` `hotkey` `i18n` `model` `platform` `secrets` | none |
| 1 | `ai` `autostart` `config` `dotadata` `install` `speech` `stats` | depth 0 only |
| 2 | `aisvc` `coach` `matchdata` | depths 0–1 |
| 3 | `aicoach` `hud` `rules` `sim` | depths 0–2 |
| 4 | `overlay` | depths 0–3 |
| 4 | `server` | 22 of the other 24 |
| 5 | `main` | 16 |

That shape is the hard part of an architecture and it's intact. The game's vocabulary (`dota`), the GSI payload (`gsi`) and the record of play (`model`) know nothing about anything else, and everything above them is built in terms of them. No refactoring below should be allowed to break it.

**A flat `internal/` is not the problem.** Nesting packages in Go buys nothing mechanically — `internal/ai/provider` is exactly as private as `internal/aiprovider`, and the standard library is mostly flat. Depth is a navigation aid, nothing more. It is the least valuable change available here, so it comes last.

## What has stopped keeping up

### The `main` package lives at the repository root

963 lines across `main.go`, `app.go`, `doctor.go` and the `console_*.go` pair sit in `package main` next to `go.mod`. Go's convention is `cmd/<binary>/`, and the convention exists for a reason that bites here: nothing in `package main` can be imported, so the command dispatch, the `run()` wiring and every doctor check are unreachable from a test in any other package. Fourteen commands and the entire process startup sequence are currently untestable except through the binary.

### `internal/server` is the application, not a server

32 files, 5,744 lines, and it imports 22 of the 24 other internal packages. Real domain logic hangs off `*Server` alongside the HTTP handlers:

| Function | What it actually is |
|---|---|
| `tiltReason` (`tilt.go`) | Behavioural analysis over match history and MMR |
| `mmrBefore`, `confirmRanked` (`mmr.go`) | MMR reconciliation across matches |
| `itemGoals` (`targets.go`) | Deriving personal item targets from history |
| `readPickHelp` (`picks.go`) | Draft advice from your own record |
| `drill`, `drillResult` (`drill.go`) | Scoring recent matches against a habit |

`Server` is a 45-field struct with nine separate mutexes guarding unrelated state — the HUD queue, the role lock, recording, background tasks, MMR, overlay problems. Each of those is a small domain with its own invariants, forced to share one type because that is where the handler happened to live. This is what makes the package hard to work in, and it is unrelated to how deep the folder sits.

### Names have collided

`ai`, `aicoach` and `aisvc` are three packages you must open to tell apart. `dota`, `dotadata`, `matchdata`, `model`, `stats` and `gsi` are six packages for game facts and the record of play, and only `gsi` says plainly what it holds. `internal/install` (Steam discovery and GSI config) reads as though it were about `installer/` (Inno Setup and signing), which it is not.

## Target layout

Flat, with names that carry their meaning:

```
cmd/gourdian/          main(), command dispatch, rsrc_windows_amd64.syso
internal/
  cli/                 the commands: run, doctor, install, simulate, stats, mmr, import, overlay, …
  app/                 process wiring: config → logger → opendota → speech → coach → stats → server → listener → overlay

  gsi/                 GSI payload types                              (unchanged)
  dota/                positions, lanes, clock, map timings           (unchanged)
  model/               matches, samples, tips, MMR, reviews, goals    (unchanged)

  coach/               rules engine, built-in rules, lane detection   (unchanged)
  rules/               custom rule storage and templates              (unchanged)
  hud/                 what the HUD shows, per widget                 (unchanged)
  i18n/                Russian phrases and the glossary               (unchanged)

  aiprovider/          Claude Code and Codex CLIs, Anthropic, OpenAI  (was ai)
  aiprompt/            coach prompts and answer schemas               (was aicoach)
  aiconnect/           provider health, model lists, setup wizard     (was aisvc)
  secrets/             API keys encrypted with DPAPI                  (unchanged)

  opendota/            API client, disk cache, rate limit             (was dotadata)
  ingest/              folding OpenDota matches into the statistics   (was matchdata)
  stats/               the data file, migrations, queries, CSV        (unchanged)

  tilt/                the tilt check                                 (out of server)
  mmr/                 MMR reconciliation and prompts                 (out of server)
  targets/             personal targets from history                  (out of server)
  picks/               draft advice                                   (out of server)
  drill/               habit drills and scoring                       (out of server)
  briefing/            the pre-game briefing                          (out of server)
  focus/               position detection and hero-role memory        (out of server)

  server/              HTTP API, event stream, embedded dashboard     (transport only)
  overlay/             Win32 and X11 HUD, hotkeys, tray               (unchanged)
  speech/              Windows speech and Piper voices                (unchanged)

  config/ platform/ hotkey/ autostart/ gsiconfig/ hidewin/ buildinfo/ sim/
```

`gsiconfig/` is `install/`, renamed so it no longer reads as the Windows installer.

## Migration

Four stages, each its own pull request, each leaving `make test` green.

### Stage 1 — `cmd/gourdian/`

Purely mechanical, and the one unambiguous convention fix.

| From | To |
|---|---|
| `main.go` — `main`, dispatch, `reorderFlags` | `cmd/gourdian/main.go` |
| `main.go` — `run` and the wiring inside it | `internal/app/` |
| `main.go` — `installCmd`, `simulate`, `replay`, `statsCmd`, `importCmd`, `mmrCmd`, `overlayCmd`, `startOverlay` | `internal/cli/` |
| `app.go` — `setupCmd`, `runApp`, `versionCmd`, `stopRunningTrainer`, `trainerCall`, `trainerFetch` | `internal/cli/` |
| `doctor.go` — `doctor`, `checker`, `windowsCommand` | `internal/cli/doctor.go` |
| `console_windows.go`, `console_other.go` | `internal/cli/` |
| `rsrc_windows_amd64.syso` | `cmd/gourdian/` |

Build files to follow:

- `Makefile` lines 13, 17, 78 and 90: the build target `.` becomes `./cmd/gourdian`.
- `Makefile` line 21: `make winres` gains `--out cmd/gourdian/rsrc`, so go-winres writes the `.syso` next to the `main` package. `--in` keeps its default of `winres/winres.json`, resolved from the repository root, so `winres/` does not move.
- `packaging/arch/PKGBUILD` line 23: the same `.` → `./cmd/gourdian`.
- `.github/workflows/ci.yml` needs nothing; it runs `go test ./...`.

Commit or discard the working-tree change to `rsrc_windows_amd64.syso` before starting, so the move is a clean `git mv`.

The payoff beyond convention: `run()`, the command dispatch and the doctor checks become importable, and therefore testable without building and launching the binary.

### Stage 2 — break up `internal/server`

The one that actually pays. For each domain in the table above, the same shape:

1. Move the logic and the state it guards into its own package, as a type with its own mutex — `tilt.Checker`, `mmr.Ledger`, `targets.Cache`, `picks.Advisor`, `drill.Runner`, `briefing.Builder`, `focus.Tracker`.
2. Give it an interface for what it needs from `stats`, `opendota` and `coach`, so it can be tested with a fake rather than a live store.
3. Leave the `http.HandlerFunc` in `server`, reduced to decoding the request, one call, and `writeJSON`.
4. `Server` holds each as a field, and loses the corresponding mutex.

Work one domain per commit and the diff stays reviewable. Order them easiest-first — `tilt` is pure functions over slices, `focus` is the most entangled with the GSI path, so do `tilt`, `picks`, `drill`, `targets`, `briefing`, `mmr`, `focus`.

`server.go`'s `handleGSI` and the match pipeline in `matchflow.go` and `record.go` stay: that pipeline *is* the application's main loop, and it belongs with the transport that feeds it.

Afterwards `Server` should be roughly a dozen fields, and `internal/server` should be about 2,000 lines of handlers, the hub and the embedded dashboard.

### Stage 3 — renames

`git mv` plus one `sed` over imports, then `gofmt`. `ai` → `aiprovider`, `aicoach` → `aiprompt`, `aisvc` → `aiconnect`, `dotadata` → `opendota`, `matchdata` → `ingest`, `install` → `gsiconfig`. Nothing but import lines and package clauses change, so review it as one commit.

`model` stays. It is vague, but it is imported nearly everywhere and the rename buys less than it costs.

### Stage 4 — optional grouping

Only if `internal/` still feels unwieldy after stages 1–3, and knowing it touches every import in the repository for navigation alone: group into `game/`, `coach/`, `ai/`, `data/`, `ui/` and `sys/`. Stage 3's names were chosen so that this stage is a pure move — `aiprovider` becomes `ai/provider` and reads correctly either way.

## Non-goals

- **No new abstraction layers.** No repository interfaces over `stats`, no service locator, no dependency-injection container. Stage 2 splits a large type into smaller ones; it does not add indirection between them.
- **The layering above stays as it is.** Nothing acquires an import that would create a cycle or let a lower package learn about a higher one.
- **Behaviour does not change.** Every stage is a refactor. `make test` — gofmt, `go vet` for Linux and Windows, and the tests under `-race` — is the gate, and no stage should need a test rewritten to pass, only moved.
