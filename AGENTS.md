# Project knowledge and working rules

## Goal and communication

Paperboy is a display service for ePaper and text displays (own firmware on ESPHome with deep sleep, ADR 0041; OpenDisplay is not supported by the service yet). Since v2 (ADR 0049) a page is a grid of widgets, and every widget comes from a plugin: a long-running program configured in `paperboy.yaml` that draws the widget in exactly the size of its cell. Paperboy composes the pages and delivers them according to a plan per device. Evolved from SolarAI's display service `displayd` (ADR 0028, 0029).

- Communicate with the user in German.
- Everything in the software is English: identifiers, comments, log messages, error messages, CLI help texts, commit messages and documentation (README, AGENTS.md, new ADRs) (ADR 0031). Paperboy must support multiple languages and number/date formats; the global `language` reaches the plugins with every request, and the built-in image texts (battery warning, stale bar, notices) follow it. ADRs 0012–0030 stay German.
- Adapt changes in structure and naming to the existing code.
- **Read `docs/design/` before you change behavior** and change the document that owns it in the same commit. The documents describe the current state; the ADRs describe why it became that state.
- For larger requests (e.g. removing features or options, multiple files/ADRs affected), briefly summarize before implementation (what changes, what stays, risks) and carry them out only after confirmation by the user.
- Paperboy does not import any SolarAI packages. Data sources are reached only by plugins (a SolarAI plugin talks to SolarAI over HTTP itself); Paperboy talks only to plugins, over stdin/stdout.

## Documentation

Each fact lives in exactly one place. Update the page that owns a behavior, not every page that mentions it.

| Where | Owns |
|---|---|
| `README.md` | Overview, quick start, documentation index, licence |
| `docs/setup.md`, `docs/content.md`, `docs/runtime.md`, `docs/releases.md`, `docs/opendisplay.md` | What the operator does: instance, configuration, pages, plans, web UI, device API, downloads |
| `docs/design/01-system.md` | Parts and processes, instance directory, configuration, reload, data flow, code map |
| `docs/design/02-content.md` | Pages, widgets, grid and style, text pages, plans, conditions, actions, the menu |
| `docs/design/03-devices.md` | Display types, device files, device API, state, power, delivery, firmware, hardware findings |
| `docs/design/04-operator-ui.md` | Dashboard, stats, downloads, previews, virtual display, editors, page and plan API, password |
| `docs/design/05-plugins.md` | Protocol, plugin host, versions, state and blobs, conditions, the shipped plugins |
| `docs/design/06-delivery.md` | Release trains, assets, workflows, the download client, cache, inventory |
| `docs/glossary.md` | The terms. One term per thing; a second word for a known thing is a bug |
| `docs/plugins/<plugin>.md` | One shipped plugin: config, widgets, args, data source |
| `docs/adr/` | Why a decision was made. Never rewritten, superseded by a new ADR |

- English documents follow ASD-STE100 as the house style: at most 25 words per sentence, active voice, present tense, imperative for steps, no filler words. `./scripts/check-docs.sh` checks sentence length and filler words of the design documents.
- A shipped plugin is documented in `docs/plugins/<plugin>.md`; `docs/plugins/README.md` lists them and the README links there. Update that file when a plugin changes.
- A new plugin also gets an example page in `examples/pages/`, is added to the tour plan `examples/plans/tour.yaml` and is listed in `examples/README.md`.
- Document significant decisions as ADRs under `docs/adr/` (template `0000-vorlage.md`, maintain the index `docs/adr/README.md`). ADRs 0012–0030 originate from SolarAI (0030 applies to both); new ADRs from 0032. Do not rewrite existing ADRs, supersede them with new ones.

## Versioning and decisions

- Git repository `github.com/maehld/paperboy`, branch `main`. After **every completed work step** create a commit with an **English** commit message in the imperative (ADR 0031). Run the appropriate checks before committing.
- **Work goes through issues and pull requests by default:** create (or reuse) an issue, work on a branch, open a PR and leave merging to the user. For small changes ask whether a direct commit to `main` is fine; without an explicit yes keep the default.
- **Work in a worktree, never in the main checkout:** create one for the issue (`worktree_create`), keep the checkout on `main` untouched and do not reuse another worktree's branch.
- **Rebase onto `main` right before opening the pull request** (`git fetch origin` then `git rebase origin/main`), so the PR starts from the current base and the conflict resolution happens in the branch, not on the base.
- **Pull requests are always squash-merged**, so one PR becomes exactly one commit on `main` whose body keeps the PR commits as bullet points. The issue number goes at the end of the commit message: the subject ends with `[#<issue>]` and the message ends with `Closes #<issue>` before trailers such as `Co-authored-by`, e.g. `Reload configuration on change [#1]` (commit `8958866`).
- When work on an issue starts (also by a subagent), assign the issue to the user (`gh issue edit <n> --add-assignee @me`).
- **Releases (ADR 0056):** one repository with three release trains. Tags select what a release contains: `vX.Y.Z` (Paperboy binaries, plugin zips, firmware images), `plugins-vX.Y.Z` and `firmware-vX.Y.Z` for a single deliverable. Only `release.yml` creates a release; *CI*, *Plugins* and *Firmware* build artifacts for changes and pull requests. Assets: `paperboy-<os>-<arch>[.exe]`, `paperboy-plugins-<os>-<arch>.zip`, `paperboy-firmware.zip` (one bundle with the images of every hardware type, unpacks to `firmware/`, ADR 0059, 0060, 0063), and `SHA256SUMS`. Paperboy's version comes from Go's VCS stamping, so release builds need full history (`fetch-depth: 0`). **Every plugin has its own version in `cmd/<plugin>/VERSION`** (embedded, ADR 0075): the zip names the programs `<plugin>-v<version>`, raise the version in the same commit as the change, and the *Plugins* workflow fails a pull request that changes `cmd/<plugin>` without raising it (`scripts/check-plugin-versions.sh`). A change to `pkg/plugin` or `pkg/render` raises every plugin's version, because every program contains them; other shared `pkg/` code and `go.mod`/`go.sum` only warn (ADR 0107). Do not split the repository: the firmware may move to its own repository only after it has run on real hardware, and plugins only behind an SDK for third parties (ADR 0056). Details in `docs/design/06-delivery.md`.
- `paperboy.yaml`, the local instance content (`displays/`, `pages/`, `plans/`, `devices/`, `plugins/`, the v1 leftovers `providers/` and `icons/`, `state/`), the old `paperboy.json`/`displayd.json`, `paperboy-data/`/`displayd-data/` and `.env` are excluded via `.gitignore` and must not be committed or deleted.

## Architecture rules

The structure is in `docs/design/`; these are the rules that a change must not break.

- **Go 1.27 or newer.** Dependencies: `golang.org/x/image` **v0.24.0** (ADR 0013), `go.yaml.in/yaml/v3` **v3.0.5** (ADR 0024, 0026) and `tinygo.org/x/bluetooth` **v0.16.0** only in `internal/opendisplay/odble` (ADR 0016; the Paperboy binary does not contain it). Further dependencies only with their own ADR.
- **No data API and no token.** The operator UI has a password (`ui.password`, ADR 0096); the device API keeps its per-device key (ADR 0042). The two route tables `deviceRoutes` and `uiRoutes` in `Handler` keep them apart, and a test pins the prefix of the device routes.
- **A device never gets an error page.** Without a plan, with a missing or broken page or a broken plan it gets a notice page; a failing widget only marks its own cell. A broken configuration file keeps the last valid one.
- **A plugin request never runs under the state lock.** The configuration snapshot is taken before locking, the conditions are asked before locking, page selection happens under the lock, and composing, drawing and writing happen outside it. Interactions with `state/` are atomic, and state belongs to the MAC, not to a file name (ADR 0080).
- **The instance directory is the configuration.** Files are parsed strictly with a hint for every rejected key, and the same parser validates the runtime and the editor API.
- **A page is a grid of widgets on a 6 × 8 reference grid.** Sizes are physical (points, `ppi`), a widget draws exactly its cell, and a widget that cannot draw there answers `ErrTooSmall` instead of leaving the cell empty.
- **A text page is a character grid of boxes (ADR 0111, 0118).** It has no pixel `grid`; every box names `at`, `span` and either a declared plugin value or literal text, and an `overflow` of strip, wrap or scroll. Paperboy formats, aligns and maps the content to the charset. The `text` action stays in the protocol until the last plugin declares values. The editor and the virtual display draw the text grid with the vendored pixel font (ADR 0116).
- **Every plugin is a separate program** with its own version in `cmd/<plugin>/VERSION`; the protocol is public in `pkg/plugin` and only grows additively.

## Essential rules for devices

- **No upload endpoints and no data API:** pages are composed from plugin widgets and selected by plans.
- **Device API (ADR 0042):** plain HTTP status codes; `401` makes the firmware drop its key and register again by MAC, so a lost `state/devices/` only costs the page cursor and the battery level. A device with a `Problem` answers 503, never 401 (ADR 0079). Image paths are relative; the device keeps the Paperboy URL it was configured with. Page and notice downloads carry the device key and only reach the device's own display type (ADR 0097).
- **Firmware:** compiled for `sticky`, `e1001`, `e1003` and `lcd2004` with the pinned ESPHome. No secrets; the only update path is `ota: http_request` with Paperboy's offer (no native ESPHome OTA, no web UI upload; rescue over USB).

## Planned work

Paperboy v2 (ADR 0049) replaced the data document (ADR 0024), the region model (ADR 0032), the block templates with their typography, formats and icons (ADR 0034, 0035, 0038, 0046), exec providers (ADR 0036) and text-line templates (ADR 0044) with plugins that provide widgets on a grid. The ADRs stay as history; the README describes only v2. The spike that led there is documented in `docs/spikes/plugin-fragments.md`. Existing installations migrate by hand; there is no migration code.

Still in place from v1: the device API (ADR 0042) with text displays (ADR 0044, 0045), plans (ADR 0025), configuration reload (ADR 0047), device status and dashboard (ADR 0039, 0040), firmware distribution (ADR 0043) and the battery warning (ADR 0020). ADR 0041's firmware compiles for all four hardware types but is not yet flashed on every device; the rollback test with a deliberately broken image succeeded, so `firmware.update: auto` may be used on real devices. CalDAV against the real iCloud was tested on 2026-11-05 with the v1 provider; the plugin mode uses the same code but has not run against iCloud yet.

Open: value lists (`for-each`) and the plugin migrations of ADR 0111; a SolarAI plugin (it talks to SolarAI over HTTP; SolarAI knows nothing about Paperboy), plans/delivery for OpenDisplay, push delivery to text displays and faster menu steps through partial e-paper updates for overlay screens (the Sticky can do black-and-white partial refreshes, the E1001 in grayscale mode cannot; issue #151).

## Startup and checks

```powershell
go run ./cmd/paperboy -config . -listen :8090
go test ./...
go vet ./...
.\scripts\build.ps1             # paperboy, odtest and the plugins into bin/<os>-<arch>/
./scripts/package-plugins.sh dist  # plugin zip for the host platform
./scripts/check-docs.sh         # sentence length and filler words of the design documents
npm install --no-save jsdom && node scripts/check-ui.mjs  # smoke-runs virtual.js in jsdom
```

- Apply `gofmt` to changed Go files. `node --check` only parses a script; `scripts/check-ui.mjs` also runs it, so an undefined identifier fails.
- **Windows Firewall blocks per binary path (confirmed 2026-10-22):** `go run` starts the server from a new `%LOCALAPPDATA%\go-build\<hash>\…\paperboy.exe` each time, so every build is a new program for the firewall. A dismissed prompt leaves a permanent **inbound Block** rule for exactly that build: displays on the LAN get "connection refused" or timeouts, while `127.0.0.1` (SolarAI) keeps working. Check with `Get-NetFirewallApplicationFilter | ? Program -like "*paperboy.exe"` and prefer a stable binary from `.\scripts\build.ps1` (`bin\<os>-<arch>\paperboy.exe`) that is allowed once, instead of `go run`.
- **UTF-8 on Windows:** Windows PowerShell 5 reads UTF-8 files as ANSI, so a `Get-Content … | Set-Content …` round-trip turns „Für“ into „FÃ¼r“. Edit files with the dedicated tools, or read and write them with `[System.IO.File]::ReadAllText`/`WriteAllText` and `UTF8Encoding($false)`; never pipe source files through `Get-Content`/`Set-Content`.
- Prefer tests for protocol behavior and state transitions.

Keep this document up to date with relevant architecture or behavior changes.
