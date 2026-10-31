# Paperboy

Paperboy is a small display service for ePaper displays. It renders pages from YAML templates fed by a shared data document and delivers them to devices running the Paperboy firmware on ESPHome (ADR 0041; deep sleep, battery operation) over its own device API (ADR 0042). Data providers push JSON over HTTP or run as small programs in the instance's providers directory (ADR 0032, ADR 0036).

Paperboy evolved from SolarAI's display service `displayd` (ADR 0028); the Git history of the carried-over files is preserved.

```text
Provider ──PUT /api/data/{path}──▶ paperboy ◀──POST /api/device/sync── Display (Paperboy firmware)
Provider program ──stdout──▶ paperboy (exec provider, providers/)
```

## Quick start

1. Create an instance directory with the fixed layout (ADR 0037): `paperboy.yaml` plus `pages/`, `plans/`, `providers/` and `state/`. Copy `cmd/paperboy/paperboy.example.yaml` to `<instance>/paperboy.yaml`, set the token and enter the MAC addresses of the displays (allow list; unknown devices are rejected and reported with their MAC in the log).
2. Create a page template `pages/standard.e1001.yaml` and a plan `plans/standard.yaml`, then set `plan: standard` on the device. Without a plan a device only shows a notice page.
3. Start:
   ```powershell
   go run ./cmd/paperboy -config . -listen :8090
   # Push data, e.g.:
   curl -X PUT -H "Authorization: Bearer <token>" -d '{"pv_power": 4200}' http://127.0.0.1:8090/api/data/solar/current
   ```
   Alternatively as a container: `Dockerfile` and `compose.example.yaml`.
4. Flash the Paperboy firmware (ADR 0041, see [firmware/esphome/README.md](firmware/esphome/README.md)) and enter `http://<computer>:8090` as the Paperboy URL in the device's web UI. The device registers with its MAC address; unknown MACs are rejected and logged.

**Migrating from `displayd` or an older Paperboy:** in the instance directory create `state/`, move the old `state.json` to `state/devices.json` and `data.json` to `state/data.json` (usually out of `paperboy-data/` or `displayd-data/`), and move `pages/`, `plans/` and `providers/` to the instance directory. Remove the `directories` section from `paperboy.yaml` and start with `-config <dir>`; a `paperboy.json` is no longer read. Devices keep their key as long as `devices.json` is kept. The environment variable is `PAPERBOY_UPLOAD_TOKEN`; `DISPLAYD_UPLOAD_TOKEN` is still accepted.

## Instance directory

Paperboy is configured with one instance directory, `-config <dir>` (default `.`). Its layout is fixed (ADR 0037):

```text
<dir>/
  paperboy.yaml      configuration
  pages/             page templates <page>.<display type>.yaml
  plans/             plans <name>.yaml
  providers/         exec provider programs (ADR 0036)
  icons/             PNG pictograms for the icon block (ADR 0038)
  firmware/          OTA images <hardware>/<hardware>-<version>.bin (ADR 0043)
  state/             written by Paperboy only
    devices.json     device register with API keys, page cursor and battery level
    data.json        last state of the data document
```

- `paperboy.yaml`, `pages/`, `plans/`, `providers/`, `icons/` and `firmware/` are yours: Paperboy only reads them and never creates them.
- `state/` is created at startup and is the only directory Paperboy writes. Back up `state/devices.json` anyway: it holds the page cursor, battery level and last served page; without it the devices get new keys on their next sync (401, then registration by MAC). `state/data.json` is refilled by the providers, but keeps the last values across a restart.

## Configuration

`paperboy.yaml` is the only configuration format (ADR 0026) and lives in the instance directory. Parsing is strict: unknown keys abort the start, as do missing plans, duplicate MAC addresses and unsupported protocols.

```yaml
language: de            # de or en; default: system language
timezone: Europe/Berlin # IANA zone; default: system timezone
server:
  upload_token_env: PAPERBOY_UPLOAD_TOKEN    # or upload_token: "a long random token"
defaults:
  refresh: 5m          # regular sleep time; plans may override it
  page_timeout: 5m     # back to the first page after this
  battery: { warn_volts: 3.45, critical_volts: 3.3, hysteresis_volts: 0.1 }
firmware:
  update: auto         # auto or off (default); offers firmware/<hardware>/ images (ADR 0043)
  min_battery_volts: 3.6
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
    protocol: paperboy
    mac: "AA:BB:CC:DD:EE:01"
    plan: standard
    refresh: 10m                          # optional
    battery: { warn_volts: 3.5 }          # optional, 0 inherits the default
    firmware: hold                        # optional, keeps the device on its firmware
```

- The map key under `devices` is the stable device ID (a-z, 0-9, `-`). It appears in logs, in `GET /api/devices` and in `state/devices.json`; existing MAC-keyed state is migrated automatically at startup.
- `language` (`de` or `en`) selects the language of the built-in image texts and the number format. Without it Paperboy uses the system language (Windows UI language, otherwise `LANG`/`LC_ALL`/`LC_MESSAGES`) and falls back to English. `timezone` (IANA name) applies to plans and built-in dates; without it the system timezone is used.
- `protocol` accepts only `paperboy` (the default) for now: devices running the Paperboy firmware (ADR 0042). `trmnl` was removed and aborts the start, as does the former `server.public_url` (images are fetched relative to the URL configured on the device). `opendisplay` is rejected until plans cover it, `text-http`/`image-http` and `kind: text` follow with ADR 0027.
- `plan` must name an existing `plans/<name>.yaml`; a missing file aborts the start, a broken plan shows a notice page. Without `plan` the device shows a notice page in the configured language („Kein Plan konfiguriert“ / “No plan configured”).
- `upload_token_env` names the variable that holds the token; `upload_token` puts it into the file. `PAPERBOY_UPLOAD_TOKEN` (and the legacy `DISPLAYD_UPLOAD_TOKEN`) override both.
- `-config` names the instance directory (default `.`); Paperboy reads `<dir>/paperboy.yaml` and finds all other directories there. A file argument is rejected with an explanation, and a `.json` configuration is no longer read.
- The configuration is read once at startup; reloading it without a restart is still open (TODO).

## Behavior

- **Display types** (size, grayscale levels, panel width, minimum interval) determine the layout; providers query them via `GET /api/profiles`. Several devices can share a display type. Pages are delivered as PNG with 1 bit or 2 bits (4 gray levels).
- **Plans** (ADR 0025): every device points to a plan (`plans/<name>.yaml`) that selects its pages according to time rules. A device without a plan shows a notice page.
- **Interval**: `defaults.refresh` or the device's `refresh`, at least 1 minute and never below the display type's `min_refresh`; a plan rule may override it. Unchanged image → the display does not redraw.
- **Device API** (ADR 0042): `POST /api/device/register` with `{"mac", "hardware", "firmware"}` returns `{"key", "device"}` for a configured MAC (an existing key is kept) and `403` otherwise. Every wake sends one `POST /api/device/sync` with `Authorization: Bearer <key>`: `reason` (`timer`, `button`, `touch`, `powercycle`), `events` (buttons by role `next`, `previous`, `home`, optionally `hold_ms`; touches with `x`/`y`), `shown` (hash on the panel), `battery` (`volts` required, `percent`, `mah`, `ma` optional), `rssi` and `log` entries. The answer names the image (`image.path` relative to the Paperboy URL, `image.hash`), the deep-sleep time `refresh_s`, `listen_s` (0 after a timer wake, 20 s after input), the server `time` in Unix seconds and `firmware` (an update offer, otherwise `null`). An unknown key gets `401` and the device registers again; an invalid body gets `400`.
- **Buttons**: events apply in order before rendering. `next` = next page, `previous` = one page back (wrapping), `home` = first page of the active rule. After `page_timeout` (5 min) back to the first page. Touches and long presses are logged but change nothing yet.
- **Data document** (ADR 0024): providers write JSON branches via `PUT /api/data/{path}` (bearer token; `PUT` replaces instead of merging, missing intermediate objects are created, 409 on type conflicts, 1 MB per write, optional `X-Data-Time` in RFC 3339). Exec providers write the same branches in process (see below). `GET` and `DELETE` on the same paths help with debugging. Each path keeps its write time, so stale data can be detected without comparing content. Page templates render from the document (see below).
- **Stale**: templates mark watched paths with `stale_after`; page templates show the bar „Keine neuen Daten seit …“ (“No new data since …”) when the data is older, in the configured language.
- **Battery warning** (ADR 0020): fed by `battery.volts` of the sync. With `battery` in `defaults` or on a device, every page below `warn_volts` gets the bar „Akku schwach – bitte laden“ (“Battery low – please charge”); below `critical_volts` a full-screen image „Akku fast leer“ (“Battery almost empty”) appears instead, and buttons then do not page. A level ends only `hysteresis_volts` above its threshold. Without `battery` no warning. All texts use the configured `language`.
- **Firmware updates** (ADR 0043): put OTA images into `firmware/<hardware>/<hardware>-<version>.bin` of the instance (`firmware/esphome/build.ps1 -Hardware sticky -Instance <dir>` does it). The highest version (compared part by part as numbers) is offered in the sync as `{"version", "path", "md5"}` when `firmware.update` is `auto`, the device has no `firmware: hold`, its reported version differs (a lower file is a rollback), its battery reading is at least `min_battery_volts` and not at warning level, and the version has not failed three times on that device. A sync that still reports the old version after an offer counts as a failure; a changed image (new MD5) or clearing `firmware_failures` in `state/devices.json` allows new attempts. Images are served at `GET /firmware/<hardware>/<file>` without a token; other file names are ignored and listed in the log and on the dashboard. `GET /api/devices` adds `firmware_available` and `firmware_status` (`current`, `offered`, `available`, `failed`, `held`, `none`). Test the rollback with a broken image before using `update: auto`.
- **Device logs**: the sync carries firmware log entries (`level`, `message`, optional `source`, `time` and `id`). Paperboy writes one line per entry with device, level, source and age; an entry with a `time` or `id` that a repeated sync delivers again is logged only once.
- `GET /api/devices` shows every configured device with registration, hardware, firmware, battery voltage (and `battery_percent`/`battery_mah` from a fuel gauge), signal strength, wake reason and last sync (without keys), plus what it is showing (ADR 0039): `last_page`, `last_page_kind` (`page`, `notice` or `battery`), `last_page_hash`, `last_page_url` and `last_refresh`, the last render or plan error in `last_error`, and a derived `status` (`ok`, `late`, `never` or `unregistered`). `late` means the last sync is more than twice the reported refresh old; devices deep-sleep, so an idle device is not an offline one.
- **Dashboard:** `GET /` redirects to `/ui`, a read-only overview of all devices (ADR 0040). Each card shows the status, the plan and its active rule, a preview of the last served page, refresh, last contact, battery and the last render or plan error. It is a server-rendered Go template without JavaScript, refreshes itself every 30 seconds and needs no token. The header shows the running build: version (or `dev`), short commit with `+dirty` for local changes, and commit date from the VCS stamp; the Go toolchain is in the tooltip.
- **Security:** Writing data and previewing pages require the token (`Authorization: Bearer`, alternatively via `PAPERBOY_UPLOAD_TOKEN`); devices via allow list and their own key. The device API answers with plain HTTP status codes (`400`, `401`, `403`).

## Page templates

Pages are rendered from the data document: `<page>.<display type>.yaml` in `pages/`. `rows` gives the relative row heights, every row holds exactly one block, and the block `name` is its region name (ADR 0032).

```yaml
title: Solar
watch:
  - { path: solar/current, stale_after: 30m, label: Solar }
notices: bar            # bar (default), badge or none
rows: [1, 3, 6, 3]
blocks:
  - name: kopf
    header:
      title: Solar
      subtitle: "{{ power .solar.current.latest.values.pv_power }} vom Dach"
      align: left
      status: { time: solar.current.latest.time, label: Stand }
      page_indicator: true
      rule: 2
  - name: kacheln
    tiles:
      columns: 4
      divider: 2
      items:
        - { label: PV, value: solar.current.latest.values.pv_power, format: power }
        - { label: Netz, value: solar.current.latest.values.grid_power, format: power, sign: [Einspeisung, Bezug] }
  - name: verlauf
    chart:
      points: solar.history.points
      title: Today
      unit: kW
      time: true
      axis: true
      ticks: 6h
      now: true
      y_max: auto
      series:
        - { key: pv_power, style: area, fill: shade }
        - { key: load_power, style: line, weight: 3 }
  - name: zeiten
    rows:
      - { label: Sonnenaufgang, value: solar.today.sunrise, format: time }
```

- Data paths accept dots or slashes (`solar.current` equals `solar/current`); missing values render as `—`.
- `format` formats the value: `power` (W or kW), `energy` (kWh), `percent` (already in percent), `ratio` (fraction 0..1 → percent), `temp`, `time` (RFC 3339 → HH:MM), `duration` (seconds → `42 min`, `2 h`, `3 h 20 min`, ADR 0035), `abs`; `sign: [negative, positive]` appends the matching word.
- `tiles` and `rows` also accept the object form with `padding`, `items` and, for `tiles`, `columns` (grid columns) and `divider`. A tile's optional `detail` is a third text line; `label_weight`, `value_weight`, `detail_weight`, `label_size`, `value_size`, `detail_size`, `align` and `shrink` style its elements.
- `columns` splits a region into weighted columns, each holding exactly one block. Nested `columns` are rejected.
- `bar` draws a value as a filled bar or battery symbol: `min`, `max`, `orientation: vertical|horizontal`, `border`, `cap`, `fill`, `label` and label styling.
- `icon` draws one PNG pictogram from `icons/` (ADR 0038). `source` is a text template, so the page chooses the picture from the data: `icon: { source: '{{ .weather.current.symbol }}.png', size: 0.9 }`. `align`, `size` (fraction of the region height), `padding` and an optional `label` below the picture are supported; the shorthand `icon: sun.png` works too. The picture is reduced to a mask (opaque dark pixels are ink), cropped to its ink and scaled to the region, so the same asset renders sharp on 1-bit and 4-gray panels. A missing file is a render error and leads to a notice page; `../` and absolute paths are rejected. A `source` that expands to nothing (the data is not there yet) draws no icon and keeps the page intact.
- Style fields are `weight: regular|bold`, `size`, `shrink`, `align: left|center|right`, `padding` and `rule`/`divider` in pixels. `size` is a fraction of the block height; in `tiles` it is relative to the cell height, in `rows` to the item height and in `text` to the line height. `shrink` defaults to `true`.
- `chart`: `time: true` plots points by their RFC 3339 `time` from local midnight to midnight; `axis`, `ticks`, `now`, `y_max: auto|<number>` and `legend` control axes, time ticks, the "now" marker, the scale and the legend. Series use `style: area|line`, `fill: shade|hatch|solid|none` and `weight` in pixels; the string shorthand remains accepted.
- `header` accepts `title`, `subtitle`, `align`, `status: { time, label }`, `page_indicator` and `rule`. The status area is right-aligned and formats `status.time` as `HH:MM`; `page_indicator` appends the page number when the plan has more than one page.
- `notices: bar|badge|none` decides how stale data appears: a bottom bar (default), a badge in the header status area, or nothing. `badge` requires a header with `status`.
- Free texts are Go templates over the document with the same helpers, e.g. `{{ percent .solar.current.latest.values.battery_soc }}` or `{{ ratio .solar.derived.balance.autarky }}`. They also provide `now`, `page`, `pages`, `stale "path"`, `below` and `above`; `stale` reports whether a watched path is older than its `stale_after`, and `time` also accepts a `time.Time` (`{{ time now }}`).
- `watch` entries add the „Keine neuen Daten seit …“ bar once the path is older than `stale_after` (or the badge with `notices: badge`).
- `language` in the configuration controls the number format (decimal comma for `de`, dot for `en`) and all built-in texts. Page templates have no locale of their own (ADR 0033); a `locale` key is rejected.
- Preview with the upload token: `GET /api/preview/e1001/solar`.

Implemented blocks are `header`, `tiles`, `rows`, `chart`, `text`, `columns`, `bar` and `icon`.

The four SolarAI reference pages (overview detailed and compact, battery, daily balance and daily history) are in `examples/pages`, with an example plan in `examples/plans/solar.yaml`. The weather page (`wetter.e1001.yaml`, `wetter.sticky.yaml`) belongs to the Open-Meteo provider and uses the icons in `examples/icons` (`sun`, `cloud`, `rain`, `snow`, `thunder`, `tshirt`, `pullover`). Copy them into the `pages/` and `icons/` directories of your instance and rename the display-type suffix if needed; the golden-image tests in `internal/page` render them with a fixed data document.

## Providers

Data reaches the document by one of two routes:

- **HTTP:** any service writes branches with `PUT /api/data/{path}` (bearer token). SolarAI does this for `solar/...`.
- **Exec providers** (ADR 0036): a small program in the providers directory prints the new value of its branch as JSON, and Paperboy runs it on an interval and writes the result.

```yaml
providers:
  weather:
    command: [weather, -lat, "52.52", -lon, "13.40"]
    path: weather         # the branch the provider owns
    interval: 15m         # default 15m, at least 10s
    timeout: 30s          # default 30s, not longer than the interval
```

- The program lives in `providers/` of the instance directory; `command` is an argument vector, never a shell string. A relative name is looked up there first, then on `PATH`.
- Exit code 0 and valid JSON → the branch is replaced. Any failure (non-zero exit, timeout, output above 1 MB, invalid JSON) is logged and leaves the previous data alone, so the stale notice appears instead of an error page.
- The write time is the run time, unless the output contains a top-level `updated` RFC 3339 string: then that data time applies, as with `X-Data-Time` on the HTTP API.
- Every provider sees `PAPERBOY_LANGUAGE` and `PAPERBOY_TIMEZONE`; the language is global (ADR 0033), providers do not carry their own.
- Only programs named in `paperboy.yaml` run, so the configuration file is executable content.

**Weather:** `cmd/weather` maps [Open-Meteo](https://open-meteo.com/) (no API key) to the branch `weather` and is a ready-made provider:

```powershell
go build -o providers/weather.exe ./cmd/weather   # or ./scripts/build.ps1
providers/weather.exe -lat 52.52 -lon 13.40 | ConvertFrom-Json | Select-Object updated
```

It writes `current` (temperature, apparent temperature, humidity, precipitation, probability, wind and Beaufort, WMO code, symbol name and condition text), `today` (min/max, probability, sunrise, sunset), `days` (five days with weekday label and condition text) and `hours` (the local day as chart points). `examples/pages/wetter.e1001.yaml` and `wetter.sticky.yaml` render it, including a clothing pictogram: below 15 °C felt temperature or above wind force 3 the page shows `pullover.png`, otherwise `tshirt.png`.

## Plans

Which pages a device shows is decided by a plan, `<name>.yaml` in `plans/`. A device points to it with `plan: wohnzimmer`; several devices may share a plan. A plan only names pages, so it works for devices of different display types.

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
- Plans serve devices on the Paperboy firmware. Data conditions (`when.data`) follow later and are rejected when loading a plan.

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
.\scripts\build.ps1   # paperboy, odtest and weather for Linux amd64/arm64 and Windows into bin/
```

The dashboard header shows version, commit and commit date, straight from Go's VCS stamping. `go build` and `scripts/build.ps1` embed them as long as the Git repository is present. The container image therefore keeps `.git` in the build context and installs the git binary in its build stage (the alpine Go image ships none); the final image still contains only the binary.

A build without a repository or with `-buildvcs=false` reports `dev`. `go version -m <binary>` shows what was stamped. Untagged builds show `dev` plus commit; `git tag v0.1.0` gives release builds a version name.

```text
cmd/paperboy/            Startup, options and example configuration
cmd/odtest/              Test program for OpenDisplay devices (TCP and Bluetooth)
cmd/weather/             Weather provider: Open-Meteo to the weather branch (ADR 0036)
internal/paperboy/       YAML configuration, device API, plans, data document, battery warning
internal/data/           Shared data document for page templates (ADR 0024)
internal/page/           Page templates: YAML parsing and rendering from the data document
internal/plan/           Display plans: time rules that select pages per device (ADR 0025)
internal/provider/       Exec providers: run configured programs that print JSON (ADR 0036)
internal/opendisplay/    OpenDisplay protocol; odble = Bluetooth transport
firmware/esphome/        Paperboy firmware on ESPHome for Sticky and E1001 (ADR 0041)
docs/adr/                Architecture decisions
```
