# Project knowledge and working rules

## Goal and communication

Paperboy is a display service for ePaper displays (TRMNL firmware with deep sleep, OpenDisplay). Suppliers such as [SolarAI](https://github.com/maehld/solarai) upload pages as PNG; Paperboy delivers them to the devices. Evolved from SolarAI's display service `displayd` (ADR 0028, 0029).

- Communicate with the user in German.
- Everything in the software is English: identifiers, comments, log messages, error messages, CLI help texts, commit messages and documentation (README, AGENTS.md, new ADRs) (ADR 0031). Paperboy must support multiple languages and number/date formats; language and formatting belong in the templates (ADR 0024), not in code. The image texts currently built in (battery warning, stale bar, placeholder) remain German until templates exist. Existing ADRs 0012–0030 stay German.
- Adapt changes in structure and naming to the existing code.
- For larger requests (e.g. removing features or options, multiple files/ADRs affected), briefly summarize before implementation (what changes, what stays, risks) and carry them out only after confirmation by the user.
- Paperboy does not import any SolarAI packages; suppliers and Paperboy talk to each other only via HTTP.

## Versioning and decisions

- Git repository `github.com/maehld/paperboy`, branch `main`. After **every completed work step** create a commit with an **English** commit message in the imperative (ADR 0031). Run the appropriate checks before committing.
- Document significant decisions as ADRs under `docs/adr/` (template `0000-vorlage.md`, maintain the index `docs/adr/README.md`). ADRs 0012–0030 originate from SolarAI (0030 applies to both); new ADRs from 0032. Do not rewrite existing ADRs, supersede them with new ones.
- `paperboy.json`/`displayd.json` (contain tokens), `paperboy-data/`/`displayd-data/` and `.env` are excluded via `.gitignore` and must not be committed or deleted.

## Architecture

Go **1.27 or newer**. Dependencies: `golang.org/x/image` **v0.24.0** (ADR 0013), `go.yaml.in/yaml/v3` **v3.0.5** (ADR 0024) and `tinygo.org/x/bluetooth` **v0.16.0** only in the package `internal/opendisplay/odble` (ADR 0016; the Paperboy binary does not contain it). Further dependencies only with their own ADR.

| Path | Responsibility |
|---|---|
| `cmd/paperboy/` | Startup and options (`-config paperboy.json`, `-data paperboy-data`, `-listen`), example `paperboy.example.json` |
| `internal/paperboy/` | JSON configuration (token, profiles, device allow list by MAC, day/night intervals). Upload `PUT /api/images/{profile}/{page}` (bearer token or `PAPERBOY_UPLOAD_TOKEN`, formerly `DISPLAYD_UPLOAD_TOKEN`, still accepted; pages contiguous from 0, PNG is normalized to a 1-/2-bit palette and stored atomically under `-data`). TRMNL API: `/api/setup` (only known MAC, key in `state.json`), `/api/display` (`filename` = content hash, `refresh_rate`, button: `Update-Source` ≠ timer → next page, header `special_function` → overview via `restart_playlist`, back after `page_timeout`), `/api/log`, `/images/{key}.png`, `GET /api/profiles`, `GET /api/devices`, data document via `PUT/GET/DELETE /api/data[/{path}]` (bearer token), page preview `GET /api/preview/{profile}/{page}` (renders `pages/<page>.<profile>.yaml`, config key `pages`, default `<data>/pages`). Stale uploads (`stale_after`) get a notice bar |
| `internal/paperboy/battery.go` | **Battery warning (ADR 0020):** optional section `battery` (global/per device: `warn_volts` → bar, `critical_volts` → full-screen image without paging, `hysteresis_volts`), level in `state.json` (`battery_level`), file name contains level + voltage in 0.1 V (`-low34`, `<profile>-battery-33`) |
| `internal/data/` | **Data document (ADR 0024, first stage):** shared JSON tree with path API (`PUT` replaces instead of merging, missing intermediate objects are created, 409 on type conflicts, segments `a-z 0-9 _ -`, 1 MB limit, optional `X-Data-Time`, future times capped), write timestamp per path for stale detection, atomic `data.json` under `-data`. The entry point for templates and exec providers |
| `internal/page/` | **Page templates (ADR 0024, second stage):** YAML `pages/<page>.<profile>.yaml` with `rows` (relative heights) and one block per row (`header`, `tiles`, `rows`, `chart`, `text`), block `name` = region name (ADR 0032). Values are data paths plus `format` (`power`, `energy`, `percent`, `temp`, `time`, `abs`), free texts are Go templates with the same helpers, `locale` selects decimal comma, `watch` paths with `stale_after` add the notice bar. Renders to a 1-/2-bit paletted image; strict parsing, blocks `bar`/hatching/separators still pending |
| `internal/paperboy/opendisplay.go` | **OpenDisplay (ADR 0018):** device with `"type": "opendisplay"`, `address`, optional `buttons`; `pusher` TCP only, state key `opendisplay:<name>`, no MAC/TRMNL API. Not yet tested on hardware |
| `internal/opendisplay/` | OpenDisplay sender (frames, reading configuration, direct write, pixel formats B/W and 4 gray levels); unencrypted. Transport exchangeable via `Transport`: TCP built in, BLE in `odble` (GATT service/characteristic `0x2446`, payload = MTU − 3) |
| `cmd/odtest/` | Test program: `go run ./cmd/odtest -host <IP> [-send] [-buttons]`, via Bluetooth `-scan` or `-ble <name>` |
| `Dockerfile`, `compose.example.yaml` | Container image `paperboy` |

## Essential rules for TRMNL devices

- **Never set `reset_firmware` and never send status 500** – otherwise the firmware deletes its credentials. Unknown keys → status 202.
- Devices store server address, API key and MAC. Keep address/port and `state.json`, otherwise a new setup is required.

## Hardware findings

- **E1001** (MAC 44:1B:F6:85:AA:1C, TRMNL firmware, confirmed 2026-10-17): setup, fetching and paging via button work; a button press reports `Update-Source: EXT0`, battery voltage e.g. `4.08`. Holding the button 1–5 s (`BUTTON_MEDIUM_HOLD_TIME` 1000 ms, like double click) sends `special_function` → overview; a real double click from deep sleep is practically impossible to trigger; holding > 5 s resets Wi-Fi. 4 gray levels look good. OpenDisplay: ID 0 = green button, 1 = the one to its left, 2 = the one left of that.
- **Sticky** (MAC B8:1F:3F:D8:6A:FC, TRMNL) likewise confirmed: button reports `EXT0`, paging and holding → overview work. The first fetch after setup also reports `EXT0` and therefore does not page (`LastSeen` empty).
- **Battery:** Sticky „Küche“ (“Kitchen”) 3.40 V after a long time without charging, E1001 „Wohnzimmer“ (“Living room”) 3.75 V; fluctuation approx. 0.02 V.
- **OpenDisplay firmware** (`cmd/odtest`): BLE under Windows on Sticky ODE46FD8 successful (reading configuration, test image 48 KB in approx. 31–33 s; not discoverable for a few seconds after disconnecting). Wi-Fi on reTerminal Sticky and E1001 successful (test image in native orientation). `-buttons` polls the 16-byte status block (`0x0044`) cyclically; Sticky (10.1.2.133): button on status byte 5, touch bytes 0–4. Byte 5 shows the last pressed button (ID 0 = top button, 1 = page up, 2 = page down) with **its own 4-bit counter per button**; short presses are often visible only in the counter → press = change of (ID, counter). Touch: contacts 1–5 = touched, 6 = released (coordinates remain); touch flags 0b111 (invert x/y, swap); coordinate range not yet calibrated.

## Planned work

ADR 0024 is implemented in its first two stages: the data document with path API, write timestamps per path and atomic storage (`internal/data`), and page templates that render from it (`internal/page`) with a preview endpoint (`GET /api/preview/{profile}/{page}`). Still open from ADRs 0024–0027: wiring templates into `/api/display` (images from uploads stay the device path until then), the block types `bar`, hatching and separators, plans per display, YAML configuration (`paperboy.yaml`, then migration of `state.json`), text displays and HTTP devices.

ADR 0032 fixes the provider model on top of that: providers are external processes that push JSON into the shared document, pages are an operator-owned skeleton with named regions, and image fragments as well as Go plugins are rejected. Exec providers (local binaries that print JSON instead of posting it) are a possible later addition; the data document is the seam for that, no ADR yet.

## Startup and checks

```powershell
go run ./cmd/paperboy -config paperboy.json -data paperboy-data -listen :8090
go test ./...
go vet ./...
.\scripts\build.ps1   # paperboy and odtest into bin/<os>-<arch>/
```

- Apply `gofmt` to changed Go files.
- Prefer tests for protocol behavior and state transitions.

Keep this document up to date with relevant architecture or behavior changes.
