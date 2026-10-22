# Project knowledge and working rules

## Goal and communication

Paperboy is a display service for ePaper displays (TRMNL firmware with deep sleep; OpenDisplay is not supported by the service yet). Providers push JSON into a shared data document (ADR 0032) or run as exec provider programs from the data directory (ADR 0036); Paperboy renders pages from YAML templates and delivers them according to a plan per device. Evolved from SolarAI's display service `displayd` (ADR 0028, 0029).

- Communicate with the user in German.
- Everything in the software is English: identifiers, comments, log messages, error messages, CLI help texts, commit messages and documentation (README, AGENTS.md, new ADRs) (ADR 0031). Paperboy must support multiple languages and number/date formats; language and formatting belong in the templates (ADR 0024), not in code. The image texts currently built in (battery warning, stale bar, placeholder) remain German until templates exist. Existing ADRs 0012–0030 stay German.
- Adapt changes in structure and naming to the existing code.
- For larger requests (e.g. removing features or options, multiple files/ADRs affected), briefly summarize before implementation (what changes, what stays, risks) and carry them out only after confirmation by the user.
- Paperboy does not import any SolarAI packages; suppliers and Paperboy talk to each other only via HTTP.

## Versioning and decisions

- Git repository `github.com/maehld/paperboy`, branch `main`. After **every completed work step** create a commit with an **English** commit message in the imperative (ADR 0031). Run the appropriate checks before committing.
- Document significant decisions as ADRs under `docs/adr/` (template `0000-vorlage.md`, maintain the index `docs/adr/README.md`). ADRs 0012–0030 originate from SolarAI (0030 applies to both); new ADRs from 0032. Do not rewrite existing ADRs, supersede them with new ones.
- `paperboy.yaml` (contains the token), the old `paperboy.json`/`displayd.json`, `paperboy-data/`/`displayd-data/` and `.env` are excluded via `.gitignore` and must not be committed or deleted.

## Architecture

Go **1.27 or newer**. Dependencies: `golang.org/x/image` **v0.24.0** (ADR 0013), `go.yaml.in/yaml/v3` **v3.0.5** (ADR 0024) and `tinygo.org/x/bluetooth` **v0.16.0** only in the package `internal/opendisplay/odble` (ADR 0016; the Paperboy binary does not contain it). Further dependencies only with their own ADR.

| Path | Responsibility |
|---|---|
| `cmd/paperboy/` | Startup and options (`-config paperboy.yaml`, optional `-data` override, `-listen`), example `paperboy.example.yaml` |
| `internal/paperboy/` | YAML configuration (ADR 0026: top-level `language` and `timezone` (ADR 0033), `server` with token or `upload_token_env`, `directories` relative to the file, `defaults`, `display_types`, `devices` as map with stable IDs, `providers` for exec providers (ADR 0036); strict validation, named plans must exist, `state.json` migrated from MAC keys to device IDs, reloading the configuration still TODO). TRMNL API: `/api/setup` (only known MAC, key in `state.json`), `/api/display` (`filename` = content hash, `refresh_rate` never below the display type's `min_refresh`, button: `Update-Source` ≠ timer → next page, header `special_function` → first page via `restart_playlist`, back after `page_timeout`), `/api/log`, rendered template pages `/pages/{profile}/{page}/{hash}.png`, notice pages `/hints/{hash}.png`, `GET /api/profiles`, `GET /api/devices`, data document via `PUT/GET/DELETE /api/data[/{path}]` (bearer token), page preview `GET /api/preview/{profile}/{page}` (renders `pages/<page>.<profile>.yaml`, config key `directories.pages`, default `<data>/pages`). Devices show the pages of their plan (`internal/plan`); without a plan or with a broken plan they get a notice page. There is no image upload anymore; OpenDisplay delivery is not implemented (TODO). Device handlers never render or write files while holding the state lock: page selection happens under the lock, rendering, drawing and `state.json` writes do not. A panicking page render is logged with its stack and answered with a notice page, so a broken template can never hang the server or block other devices and the data API |
| `internal/provider/` | **Exec providers (ADR 0036):** programs from `directories.providers` (default `<data>/providers`) declared under `providers` in `paperboy.yaml`; argv only, JSON on stdout (max 1 MB) replaces the branch, write time from a top-level `updated` (RFC 3339) or the run time, failures keep the old data and only log (first failure and recovery per streak), timeout per run, `PAPERBOY_LANGUAGE`/`PAPERBOY_TIMEZONE` in the environment. One provider owns exactly one path; overlapping paths are a configuration error |
| `cmd/weather/` | Weather provider for exec providers: Open-Meteo (no API key) → `weather` branch with `current`, `today`, `days` and `hours` (local day as chart points, WMO code plus text in the global language) |
| `internal/paperboy/battery.go` | **Battery warning (ADR 0020):** optional section `battery` (`defaults`, per device field-wise override: `warn_volts` → bar on plan pages, `critical_volts` → generated full-screen page via `/hints/...` without paging, `hysteresis_volts`), level in `state.json` (`battery_level`) |
| `internal/data/` | **Data document (ADR 0024, first stage):** shared JSON tree with path API (`PUT` replaces instead of merging, missing intermediate objects are created, 409 on type conflicts, segments `a-z 0-9 _ -`, 1 MB limit, optional `X-Data-Time`, future times capped), write timestamp per path for stale detection, atomic `data.json` under `-data`. The entry point for templates and exec providers |
| `internal/page/` | **Page templates (ADR 0024, second stage; ADR 0034):** YAML `pages/<page>.<profile>.yaml` with `rows` (relative heights) and one block per row (`header`, `tiles`, `rows`, `chart`, `text`, `columns`, `bar`), block `name` = region name (ADR 0032). Values are data paths plus `format` (`power`, `energy`, `percent`, `ratio`, `temp`, `time`, `abs`, `duration` — seconds as `42 min`/`3 h 20 min`, ADR 0035), free texts are Go templates with the same helpers. ADR 0034 adds typography (`weight`, `size` as a fraction of block/cell/item/line height, `shrink`, `align`), the box model (`padding`, `rule`, `divider`), one-level `columns`, `bar` with fills, tile `detail`, header `status` and `page_indicator`, chart time axis (`time`, `axis`, `ticks`, `now`, `y_max`, `legend`, series `style`/`fill`/`weight`) and the functions `now`, `page`, `pages`, `stale`, `below`, `above`, `ratio` (`time` also accepts `time.Time`). `notices: bar|badge|none` chooses how stale watched paths appear. The global `language` sets the decimal comma and the built-in texts (ADR 0033); templates have no locale of their own. Missing branches of a template become empty objects, so unset data renders as an em dash instead of failing the page. Renders to a 1-/2-bit paletted image; strict parsing and deterministic render tests, golden-image tests for the reference pages under `examples/pages` |
| `internal/plan/` | **Plans per display (ADR 0025):** `plans/<name>.yaml` (config key `directories.plans`, default `<data>/plans`) with `rules` (`when` with `from`/`to`/`days`, `pages`, `rotate`, `refresh`), `fallback`. Times use the global `timezone` (ADR 0033); a `timezone` key in a plan is rejected. First matching rule wins, `from` inclusive, `to` exclusive, midnight wraparound belongs to the start day, German and English day names, `time/tzdata` embedded. Time conditions only (`when.data` is rejected until data conditions exist). The server reloads a plan when its file changes and keeps the last valid version on errors; `UntilNextBoundary` caps the refresh to the next rule change |
| `internal/opendisplay/` | OpenDisplay sender (frames, reading configuration, direct write, pixel formats B/W and 4 gray levels); unencrypted. Used by `cmd/odtest` only; the service does not deliver to OpenDisplay devices yet (TODO). Transport exchangeable via `Transport`: TCP built in, BLE in `odble` (GATT service/characteristic `0x2446`, payload = MTU − 3) |
| `cmd/odtest/` | Test program: `go run ./cmd/odtest -host <IP> [-send] [-buttons]`, via Bluetooth `-scan` or `-ble <name>` |
| `Dockerfile`, `compose.example.yaml` | Container image `paperboy` |

## Essential rules for TRMNL devices

- **Never set `reset_firmware` and never send status 500** – otherwise the firmware deletes its credentials. Unknown keys → status 202.
- **No upload endpoints:** pages come from templates selected by plans. A device without a plan, with a missing template or a broken plan gets a notice page, never an error.
- Devices store server address, API key and MAC. Keep address/port and `state.json`, otherwise a new setup is required.

## Hardware findings

- **E1001** (MAC 44:1B:F6:85:AA:1C, TRMNL firmware, confirmed 2026-10-17): setup, fetching and paging via button work; a button press reports `Update-Source: EXT0`, battery voltage e.g. `4.08`. Holding the button 1–5 s (`BUTTON_MEDIUM_HOLD_TIME` 1000 ms, like double click) sends `special_function` → overview; a real double click from deep sleep is practically impossible to trigger; holding > 5 s resets Wi-Fi. 4 gray levels look good. OpenDisplay: ID 0 = green button, 1 = the one to its left, 2 = the one left of that.
- **Sticky** (MAC B8:1F:3F:D8:6A:FC, TRMNL) likewise confirmed: button reports `EXT0`, paging and holding → overview work. The first fetch after setup also reports `EXT0` and therefore does not page (`LastSeen` empty).
- **Battery:** Sticky „Küche“ (“Kitchen”) 3.40 V after a long time without charging, E1001 „Wohnzimmer“ (“Living room”) 3.75 V; fluctuation approx. 0.02 V.
- **OpenDisplay firmware** (`cmd/odtest`): BLE under Windows on Sticky ODE46FD8 successful (reading configuration, test image 48 KB in approx. 31–33 s; not discoverable for a few seconds after disconnecting). Wi-Fi on reTerminal Sticky and E1001 successful (test image in native orientation). `-buttons` polls the 16-byte status block (`0x0044`) cyclically; Sticky (10.1.2.133): button on status byte 5, touch bytes 0–4. Byte 5 shows the last pressed button (ID 0 = top button, 1 = page up, 2 = page down) with **its own 4-bit counter per button**; short presses are often visible only in the counter → press = change of (ID, counter). Touch: contacts 1–5 = touched, 6 = released (coordinates remain); touch flags 0b111 (invert x/y, swap); coordinate range not yet calibrated.

## Planned work

ADR 0024 is implemented in its first two stages: the data document with path API, write timestamps per path and atomic storage (`internal/data`), and page templates that render from it (`internal/page`) with a preview endpoint (`GET /api/preview/{profile}/{page}`). ADR 0025 is implemented for TRMNL devices: plans under `plans/<name>.yaml` with time rules, paging and rotation within a rule, refresh capped to the next rule boundary and notice pages instead of errors (`internal/plan`); a device without a plan gets a notice page. ADR 0026 is implemented except reloading: `paperboy.yaml` with top-level `language` and `timezone` (ADR 0033), `server`, `directories`, `defaults`, `display_types` and devices as a map with stable IDs, strict validation, and `state.json` migrated from MAC keys to device IDs. The image-upload pipeline was removed; OpenDisplay delivery is marked non-functional in the service (TODO). ADR 0033 configures the language and timezone globally; page templates and plans no longer carry `locale` or `timezone`. Still open from ADRs 0024–0027: configuration hot reload, plans/delivery for OpenDisplay, data conditions in plans (`when.data`), text displays and HTTP devices.

ADR 0032 fixes the provider model on top of that: providers are external processes that push JSON into the shared document, pages are an operator-owned skeleton with named regions, and image fragments as well as Go plugins are rejected. Exec providers (local binaries that print JSON instead of posting it) are a possible later addition; the data document is the seam for that, no ADR yet.

ADR 0034 is implemented: page templates gain typography (`weight`, `size`, `shrink`, `align`), the box model (`padding`, `rule`, `divider`), the blocks `columns` (one level deep) and `bar`, tile `detail`, header `status` and `page_indicator`, the notice modes `bar|badge|none`, a chart with time axis, axes, ticks, "now" marker, area fills and series weights, and the template functions `now`, `page`, `pages`, `stale`, `below`, `above`, `ratio` and `time` for `time.Time`. The four SolarAI reference pages live in `examples/pages` (detailed and compact variants) and are covered by golden-image tests. The four existing installation templates remain valid; their typography follows the new defaults.

ADR 0035 adds the format `duration` (seconds as `42 min`, `2 h`, `3 h 20 min`) and moves the charge direction out of formatted texts: providers deliver `{seconds, charging}` and the template words it. The battery reference pages use it.

ADR 0036 adds exec providers: programs in `<data>/providers` declared under `providers` in `paperboy.yaml` print the new value of one data branch as JSON, and Paperboy runs them on an interval and writes the result in process (`internal/provider`). Failures keep the previous data; the write time comes from a top-level `updated` or the run time; `PAPERBOY_LANGUAGE`/`PAPERBOY_TIMEZONE` reach every provider so language and timezone stay global. `cmd/weather` is the first provider (Open-Meteo, no API key) and feeds the `weather` branch; `examples/pages/wetter.*.yaml` render it. The chart now connects samples by timestamp distance (90 min) instead of pixel columns, so hourly forecast points draw a continuous curve.

## Startup and checks

```powershell
go run ./cmd/paperboy -config paperboy.yaml -listen :8090
go test ./...
go vet ./...
.\scripts\build.ps1   # paperboy, odtest and weather into bin/<os>-<arch>/
```

- Apply `gofmt` to changed Go files.
- **Windows Firewall blocks per binary path (confirmed 2026-10-22):** `go run` starts the server from a new `%LOCALAPPDATA%\go-build\<hash>\…\paperboy.exe` each time, so every build is a new program for the firewall. A dismissed prompt leaves a permanent **inbound Block** rule for exactly that build: displays on the LAN get "connection refused" or timeouts, while `127.0.0.1` (SolarAI) keeps working. Check with `Get-NetFirewallApplicationFilter | ? Program -like "*paperboy.exe"` and prefer a stable binary from `.\scripts\build.ps1` (`bin\<os>-<arch>\paperboy.exe`) that is allowed once, instead of `go run`.
- **UTF-8 on Windows:** Windows PowerShell 5 reads UTF-8 files as ANSI, so a `Get-Content … | Set-Content …` round-trip turns „Für“ into „FÃ¼r“. Edit files with the dedicated tools, or read and write them with `[System.IO.File]::ReadAllText`/`WriteAllText` and `UTF8Encoding($false)`; never pipe source files through `Get-Content`/`Set-Content`.
- Prefer tests for protocol behavior and state transitions.

Keep this document up to date with relevant architecture or behavior changes.
