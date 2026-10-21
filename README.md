# Paperboy

Paperboy is a small display service for ePaper displays. It renders pages from YAML templates fed by a shared data document and delivers them to devices running the unmodified [TRMNL firmware](https://github.com/usetrmnl/trmnl-firmware) (deep sleep, battery operation). Data providers and Paperboy know each other only via HTTP (ADR 0032).

Paperboy evolved from SolarAI's display service `displayd` (ADR 0028); the Git history of the carried-over files is preserved.

```text
Provider ──PUT /api/data/{path}──▶ paperboy ◀──GET /api/display── Display (TRMNL)
```

## Quick start

1. Create the configuration: copy `cmd/paperboy/paperboy.example.yaml` to `paperboy.yaml`, set the token and enter the MAC addresses of the displays (allow list; unknown devices are rejected and reported with their MAC in the log).
2. Create a page template `paperboy-data/pages/standard.e1001.yaml` and a plan `paperboy-data/plans/standard.yaml`, then set `plan: standard` on the device. Without a plan a device only shows a notice page.
3. Start:
   ```powershell
   go run ./cmd/paperboy -config paperboy.yaml -listen :8090
   # Push data, e.g.:
   curl -X PUT -H "Authorization: Bearer <token>" -d '{"pv_power": 4200}' http://127.0.0.1:8090/api/data/solar/current
   ```
   Alternatively as a container: `Dockerfile` and `compose.example.yaml`.
4. Flash the firmware: E1001 via the [Seeed wiki flasher](https://wiki.seeedstudio.com/reterminal_e10xx_trmnl/) (1.6.7 or later), Sticky via the [Sticky playground](https://www.seeedstudio.com/sticky/playground/) (TRMNL). In the Wi-Fi setup portal, enter `http://<computer>:8090` as the server URL.

**Migrating from `displayd`:** the data directory can be reused (`-data displayd-data`), and devices need no new setup as long as address and port stay the same. The configuration must be converted to YAML; `paperboy.json` is no longer read. The environment variable is now called `PAPERBOY_UPLOAD_TOKEN`; `DISPLAYD_UPLOAD_TOKEN` is still accepted.

## Configuration

`paperboy.yaml` is the only configuration format (ADR 0026). Parsing is strict: unknown keys abort the start, as do missing plans, duplicate MAC addresses and unsupported protocols.

```yaml
language: de            # de or en; default: system language
timezone: Europe/Berlin # IANA zone; default: system timezone
server:
  # public_url: https://paper.example.com   # only needed behind a reverse proxy
  upload_token_env: PAPERBOY_UPLOAD_TOKEN    # or upload_token: "a long random token"
directories:                                  # relative to this file
  pages: pages
  plans: plans
  data: paperboy-data
defaults:
  refresh: 5m          # regular sleep time; plans may override it
  page_timeout: 5m     # back to the first page after this
  battery: { warn_volts: 3.45, critical_volts: 3.3, hysteresis_volts: 0.1 }
display_types:
  e1001:
    kind: image
    size: [800, 480]
    gray_levels: 4     # 1 or 4
    width_mm: 163
    min_refresh: 60s   # never report a shorter interval
devices:
  wohnzimmer:
    display_type: e1001
    protocol: trmnl
    mac: "AA:BB:CC:DD:EE:01"
    plan: standard
    refresh: 10m                          # optional
    battery: { warn_volts: 3.5 }          # optional, 0 inherits the default
```

- The map key under `devices` is the stable device ID (a-z, 0-9, `-`). It appears in logs, in `GET /api/devices` and in `state.json`; existing MAC-keyed state is migrated automatically at startup.
- `language` (`de` or `en`) selects the language of the built-in image texts and the number format. Without it Paperboy uses the system language (Windows UI language, otherwise `LANG`/`LC_ALL`/`LC_MESSAGES`) and falls back to English. `timezone` (IANA name) applies to plans and built-in dates; without it the system timezone is used.
- `protocol` accepts only `trmnl` for now. `opendisplay` is rejected until plans cover it, `text-http`/`image-http` and `kind: text` follow with ADR 0027.
- `plan` must name an existing `plans/<name>.yaml`; a missing file aborts the start, a broken plan shows a notice page. Without `plan` the device shows a notice page in the configured language („Kein Plan konfiguriert“ / “No plan configured”).
- `upload_token_env` names the variable that holds the token; `upload_token` puts it into the file. `PAPERBOY_UPLOAD_TOKEN` (and the legacy `DISPLAYD_UPLOAD_TOKEN`) override both.
- `-config` defaults to `paperboy.yaml`; passing a `.json` file is rejected with a hint. `-data` overrides `directories.data`, the page and plan directories then default to `<data>/pages` and `<data>/plans`.
- The configuration is read once at startup; reloading it without a restart is still open (TODO).

## Behavior

- **Display types** (size, grayscale levels, panel width, minimum interval) determine the layout; providers query them via `GET /api/profiles`. Several devices can share a display type. Pages are delivered as PNG with 1 bit or 2 bits (4 gray levels).
- **Plans** (ADR 0025): every device points to a plan (`plans/<name>.yaml`) that selects its pages according to time rules. A device without a plan shows a notice page.
- **Interval**: `defaults.refresh` or the device's `refresh`, at least 1 minute and never below the display type's `min_refresh`; a plan rule may override it. Unchanged image → the display does not redraw.
- **Button**: short press = next page, hold approx. 2 s (1–5 s) = overview, longer than 5 s = reset Wi-Fi (firmware). A real double click triggers the same function, but is hardly achievable from deep sleep. After `page_timeout` (5 min) back to the overview. The TRMNL firmware does not support three separate buttons.
- **Data document** (ADR 0024): providers write JSON branches via `PUT /api/data/{path}` (bearer token; `PUT` replaces instead of merging, missing intermediate objects are created, 409 on type conflicts, 1 MB per write, optional `X-Data-Time` in RFC 3339). `GET` and `DELETE` on the same paths help with debugging. Each path keeps its write time, so stale data can be detected without comparing content. Page templates render from the document (see below).
- **Stale**: templates mark watched paths with `stale_after`; page templates show the bar „Keine neuen Daten seit …“ (“No new data since …”) when the data is older, in the configured language.
- **Battery warning** (ADR 0020): The TRMNL firmware reports only the voltage (`Battery-Voltage`). With `battery` in `defaults` or on a device, every page below `warn_volts` gets the bar „Akku schwach – bitte laden“ (“Battery low – please charge”); below `critical_volts` a full-screen image „Akku fast leer“ (“Battery almost empty”) appears instead, and buttons then do not page. A level ends only `hysteresis_volts` above its threshold. Without `battery` no warning. All texts use the configured `language`.
- `GET /api/devices` shows registered devices with battery voltage, signal strength, wake reason and last request (without keys).
- **Security:** Writing data and previewing pages require the token (`Authorization: Bearer`, alternatively via `PAPERBOY_UPLOAD_TOKEN`); devices via allow list and their own key. Paperboy never sets `reset_firmware` and never responds with status 500.

## Page templates

Pages are rendered from the data document: `<page>.<display type>.yaml` in the `pages` directory (`directories.pages`, default `<data>/pages`). `rows` gives the relative row heights, every row holds exactly one block, and the block `name` is its region name (ADR 0032).

```yaml
title: Solar
watch:
  - { path: solar/current, stale_after: 30m, label: Solar }
rows: [1, 3, 6, 3]
blocks:
  - name: kopf
    header:
      title: Solar
      subtitle: "{{ power .solar.current.latest.values.pv_power }} vom Dach"
      time: solar.current.latest.time
  - name: kacheln
    tiles:
      - { label: PV, value: solar.current.latest.values.pv_power, format: power }
      - { label: Netz, value: solar.current.latest.values.grid_power, format: power, sign: [Einspeisung, Bezug] }
  - name: verlauf
    chart: { points: solar.history.points, series: [pv_power, load_power], unit: kW }
  - name: zeiten
    rows:
      - { label: Sonnenaufgang, value: solar.today.sunrise, format: time }
```

- Data paths accept dots or slashes (`solar.current` equals `solar/current`); missing values render as `—`.
- `format` formats the value: `power` (W or kW), `energy` (kWh), `percent`, `temp`, `time` (RFC 3339 → HH:MM), `abs`; `sign: [negative, positive]` appends the matching word.
- Free texts are Go templates over the document with the same helpers, e.g. `{{ percent .solar.current.latest.values.battery_soc }}`.
- `watch` entries add the „Keine neuen Daten seit …“ bar once the path is older than `stale_after`.
- `language` in the configuration controls the number format (decimal comma for `de`, dot for `en`) and all built-in texts. Page templates have no locale of their own (ADR 0033); a `locale` key is rejected.
- Preview with the upload token: `GET /api/preview/e1001/solar`.

Implemented blocks are `header`, `tiles`, `rows`, `chart` and `text`; `bar`, hatching and separators follow.

## Plans

Which pages a device shows is decided by a plan, `<name>.yaml` in the `plans` directory (`directories.plans`, default `<data>/plans`). A device points to it with `plan: wohnzimmer`; several devices may share a plan. A plan only names pages, so it works for devices of different display types.

```yaml
rules:
  - name: morgen
    when: { from: "07:00", to: "09:00", days: [mo, di, mi, do, fr] }
    pages: [kalender-wetter, solar-overview]
  - name: tag
    when: { from: "09:00", to: "19:00" }
    pages: [solar-gross, solar-battery, solar-history]
    rotate: true
    refresh: 5m
  - name: nacht
    when: { from: "19:00", to: "07:00" }
    pages: [tueren]
    refresh: 30m
fallback: nacht
```

- The first matching rule wins. A `when` without `from`/`to` always matches; `from` is inclusive, `to` exclusive. `to < from` wraps over midnight and then belongs to the day the window starts. Days accept German and English short names.
- Times use the global `timezone` from the configuration (ADR 0033); plans have no timezone of their own, and a `timezone` key is rejected.
- `pages` is the paging order within the rule. `rotate: true` moves on at every timed refresh, otherwise only a button changes the page. Buttons page within the active rule only, holding returns to its first page, and after `page_timeout` the device shows the first page again unless the rule rotates.
- `refresh` replaces the device interval. The reported interval is capped to the next rule boundary (but never below the display type's `min_refresh`), so a sleeping device wakes up for a rule change.
- `fallback` names the rule used when nothing matches; without a match and without a fallback the device gets a notice page.
- Plans are reloaded on change without a restart; a broken file keeps the last valid version. Missing templates for a device's display type also lead to a notice page, so devices never see a server error.
- Plans currently serve TRMNL devices. Data conditions (`when.data`) follow later and are rejected when loading a plan.

## OpenDisplay

Delivery to OpenDisplay devices is currently **not available**: the image-upload pipeline it relied on was removed, and plans for OpenDisplay are a TODO. The protocol implementation (`internal/opendisplay`, including Bluetooth) and the test program below remain in the repository.

## OpenDisplay test program

```powershell
# Read firmware and panel configuration
go run ./cmd/odtest -host 192.168.1.50
# Additionally send a test image (optional -fast, -png preview.png)
go run ./cmd/odtest -host 192.168.1.50 -send
# Via Bluetooth: scan for devices, then as above by name or address
go run ./cmd/odtest -scan
go run ./cmd/odtest -ble ODE46FD8 [-send] [-buttons]
```

Bluetooth (ADR 0016) exists only in the test program. On a reTerminal Sticky under Windows a full image (48 KB) takes about 30 s, via Wi-Fi a few seconds. After disconnecting, the display takes a few seconds until it is found again (if necessary `-scan-time 20s`).

## Development

Requires Go 1.27 or newer.

```powershell
go test ./...
go vet ./...
.\scripts\build.ps1   # paperboy and odtest for Linux amd64/arm64 and Windows into bin/
```

```text
cmd/paperboy/            Startup, options and example configuration
cmd/odtest/              Test program for OpenDisplay devices (TCP and Bluetooth)
internal/paperboy/       YAML configuration, TRMNL API, plans, data document, battery warning
internal/data/           Shared data document for page templates (ADR 0024)
internal/page/           Page templates: YAML parsing and rendering from the data document
internal/plan/           Display plans: time rules that select pages per device (ADR 0025)
internal/opendisplay/    OpenDisplay protocol; odble = Bluetooth transport
docs/adr/                Architecture decisions
```
