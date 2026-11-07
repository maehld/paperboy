# Paperboy

Paperboy is a small display service for ePaper and text displays. A page is a grid of widgets, and every widget comes from a plugin: a long-running program that draws it in exactly the size of its cell (ADR 0049). Paperboy composes the pages, chooses them per device with plans and delivers them to devices running the Paperboy firmware on ESPHome (ADR 0041; deep sleep, battery operation) over its own device API (ADR 0042).

Paperboy evolved from SolarAI's display service `displayd` (ADR 0028); the Git history of the carried-over files is preserved.

```text
Plugin (calendar, weather, …) ◀──JSON lines on stdin/stdout──▶ paperboy ◀──POST /api/device/sync── Display (Paperboy firmware)
```

## Quick start

1. Create an instance directory with the fixed layout (ADR 0037): `paperboy.yaml` plus `pages/`, `plans/` and, for your own plugins, `plugins/`. Copy `cmd/paperboy/paperboy.example.yaml` to `<instance>/paperboy.yaml`, configure the plugins and enter the MAC addresses of the displays (allow list; unknown devices are rejected and reported with their MAC in the log).
2. Copy pages from `examples/pages` and a plan from `examples/plans` into the instance, then set `plan: standard` on the device. Without a plan a device only shows a notice page.
3. Put the plugins into the instance's `plugins/`: unpack `paperboy-plugins-<os>-<arch>.zip` (attached to each release, otherwise an artifact of the *Plugins* workflow) in the instance directory, or build them yourself (`./scripts/package-plugins.sh` builds the zip locally). Then start:
   ```sh
   go run ./cmd/paperboy -config . -listen :8090
   # look at a page without a device:
   curl -o page.png http://127.0.0.1:8090/api/preview/e1001/uebersicht
   ```
   Alternatively as a container: `Dockerfile` and `compose.example.yaml`. The image contains only Paperboy; use the `linux/amd64` plugin zip in the mounted instance directory.
4. Flash the Paperboy firmware (ADR 0041, see [firmware/esphome/README.md](firmware/esphome/README.md)) and enter `http://<computer>:8090` as the Paperboy URL in the device's web UI. The device registers with its MAC address; unknown MACs are rejected and logged.

**Migrating from Paperboy v1:** v2 has no data API, no exec providers and no upload token (ADR 0049). Remove the `server` and `providers` sections from `paperboy.yaml` (both abort the start with a hint) and add `plugins` instead; the weather and calendar providers are plugins now and take their settings from `config` instead of flags. Rewrite the pages as grids (see below); old templates with `rows` and `blocks` are rejected and show a notice page. `state/data.json` and the `icons/` directory are no longer used. Devices keep their keys as long as `state/devices.json` is kept.

**Migrating to physical sizes (ADR 0050):** replace `width_mm` with `ppi` in every image display type (`ppi = pixels × 25.4 / mm` of the same side; a leftover `width_mm` aborts the start with a hint) and rewrite page styles in points: `text_size` is no longer a fraction (`0.06` is rejected; at 125 ppi, 0.06 of 480 px is about 16 pt), and `padding`, `rule` and `divider` are points now (8 px ≈ 4.5 pt, 1 px ≈ 0.6 pt at 125 ppi). Old pixel values there still parse but get bigger, so check them. Third-party plugins have to be rebuilt against protocol version 3 (`style.text_size` is gone; use `text_px` or `Request.TextPx`).

## Instance directory

Paperboy is configured with one instance directory, `-config <dir>` (default `.`). Its layout is fixed (ADR 0037, ADR 0049):

```text
<dir>/
  paperboy.yaml      configuration
  pages/             pages <page>.<display type>.yaml or <page>.yaml
  plans/             plans <name>.yaml
  plugins/           plugin programs, e.g. from paperboy-plugins-<os>-<arch>.zip
  firmware/          OTA images <hardware>/<hardware>-<version>.bin (ADR 0043)
  state/             written by Paperboy only
    devices.json     device register with API keys, page cursor and battery level
```

- `paperboy.yaml`, `pages/`, `plans/`, `plugins/` and `firmware/` are yours: Paperboy only reads them and never creates them.
- `state/` is created at startup and is the only directory Paperboy writes. Back up `state/devices.json` anyway: it holds the page cursor, battery level and last served page; without it the devices get new keys on their next sync (401, then registration by MAC).

## Configuration

`paperboy.yaml` is the only configuration format (ADR 0026) and lives in the instance directory. Parsing is strict: unknown keys, duplicate MAC addresses and unsupported protocols abort the start. A plan that is missing or broken does not: it is logged and the device gets a notice page, because plan files may be added while the server runs (ADR 0047).

```yaml
language: de            # de or en; default: system language
timezone: Europe/Berlin # IANA zone; default: system timezone
plugins:
  calendar:
    command: [calendar]  # argv; looked up in plugins/, then on PATH
    timeout: 1m          # per request; default 30s
    env: { HTTPS_PROXY: "http://proxy:3128" }   # optional extra environment
    config:              # sent to the plugin once after its start
      caldav: https://caldav.icloud.com
      user: me@icloud.com
      password: ${PAPERBOY_CALDAV_PASSWORD}
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
    ppi: 125           # pixels per inch, required for image displays (800 px / 163 mm)
    min_refresh: 60s   # never report a shorter interval
    power: auto        # auto (default), battery or mains (ADR 0051)
devices:
  wohnzimmer:
    display_type: e1001
    protocol: paperboy
    mac: "44:1B:F6:85:AA:1C"
    plan: standard
    refresh: 10m                          # optional
    power: mains                          # optional, overrides the display type (E1001 on USB)
    battery: { warn_volts: 3.5 }          # optional, 0 inherits the default
    firmware: hold                        # optional, keeps the device on its firmware
```

- The map key under `devices` is the stable device ID (a-z, 0-9, `-`). It appears in logs, in `GET /api/devices` and in `state/devices.json`; existing MAC-keyed state is migrated automatically at startup.
- `plugins` declares the plugins (ADR 0049). `config` is the plugin's own section; string values expand `${VAR}` from Paperboy's environment (an unset variable aborts loading, `$$` is a literal `$`), so secrets stay out of the file. The plugin checks its keys and rejects unknown ones; a rejected configuration shows up as a notice in the plugin's cells and in the log. The name `paperboy` is reserved for the built-in plugin.
- `language` (`de` or `en`) selects the language of the built-in image texts, the number format and the language plugins draw in. Without it Paperboy uses the system language (Windows UI language, otherwise `LANG`/`LC_ALL`/`LC_MESSAGES`) and falls back to English. `timezone` (IANA name) applies to plans and built-in dates; without it the system timezone is used.
- `protocol` accepts only `paperboy` (the default) for now: devices running the Paperboy firmware (ADR 0042). `trmnl` was removed and aborts the start, as do the v1 sections `server` and `providers` (ADR 0049). `opendisplay` is rejected until plans cover it; `text-http`/`image-http` were replaced by the device API (ADR 0044) and abort the start.
- A display type with `kind: text` is a text display (ADR 0044): `size: [columns, rows]` in characters (at most 80 × 24) and `charset: utf8|ascii|hd44780` (default `utf8`); `gray_levels` and `ppi` are rejected for it.
- An image display type needs `ppi`, its pixel density (pixels per inch, 20–1000; ADR 0050). It does not depend on the orientation: compute it from either side, `ppi = pixels × 25.4 / millimetres` of the active area. Page styles are in points and are converted with it, so a widget has the same physical size on a small dense panel and a large coarse one.
- `plan` should name an existing `plans/<name>.yaml`; a missing or broken plan is logged and shows a notice page instead of aborting the start, so a plan file can be added while the server runs (ADR 0047). Without `plan` the device shows a notice page in the configured language („Kein Plan konfiguriert“ / “No plan configured”).
- `-config` names the instance directory (default `.`); Paperboy reads `<dir>/paperboy.yaml` and finds all other directories there. A file argument is rejected with an explanation, and a `.json` configuration is no longer read.
- The configuration is reloaded on change without a restart (ADR 0047): handlers pick up a modified `paperboy.yaml` on the next request, plugins whose section changed are restarted (their cached widgets are dropped), and a broken file keeps the last valid configuration while a missing one leaves the current configuration alone. With no usable file at startup in an existing instance directory the server still starts (degraded: no display types, no devices) and applies the file as soon as it loads, even when its mod time did not change; a wrong `-config` stays fatal.

## Behavior

- **Display types** (size, grayscale levels, panel width, minimum interval) determine the layout; `GET /api/profiles` lists them. Several devices can share a display type. Pages are delivered as PNG with 1 bit or 2 bits (4 gray levels).
- **Plans** (ADR 0025): every device points to a plan (`plans/<name>.yaml`) that selects its pages according to time rules. A device without a plan shows a notice page.
- **Interval**: `defaults.refresh` (at least 1 minute) or the device's `refresh`; a plan rule may override it (down to `1s`). The reported interval never goes below the floor of the device's power source (ADR 0051): battery devices keep one minute or the display type's `min_refresh` if that is longer; mains devices go down to `min_refresh`, by default `1s` for text and `10s` for image displays. Unchanged image → the display does not redraw.
- **Power source** (ADR 0051): `power: auto|battery|mains` on the display type, overridable per device; default `auto`. The firmware reports `power` (`battery` or `mains`) in register and sync: the Sticky measures USB power on GPIO9, the E1001 cannot detect it and reports `battery`, text displays report `mains`. `auto` uses the last report (`reported_power` in `/api/devices`, next to the effective `power`) and behaves as `battery` until the device reports one, so text displays with older firmware fall back to a one-minute floor until they are updated or set to `power: mains`. The sync answers `sleep: false` for mains devices: the ePaper firmware then stays awake with Wi-Fi up and syncs again after `refresh_s` instead of deep sleeping. A page whose composition did not change keeps its PNG and hash, so short intervals cause no redraw.
- **Device API** (ADR 0042): `POST /api/device/register` with `{"mac", "hardware", "firmware"}` (optionally `power`) returns `{"key", "device"}` for a configured MAC (an existing key is kept) and `403` otherwise. Every wake sends one `POST /api/device/sync` with `Authorization: Bearer <key>`: `reason` (`timer`, `button`, `touch`, `powercycle`), `events` (buttons by role `next`, `previous`, `home`, optionally `hold_ms`; touches with `x`/`y`), `shown` (hash on the panel), `battery` (`volts` required, `percent`, `mah`, `ma` optional), `rssi`, `power` and `log` entries. The answer names the image (`image.path` relative to the Paperboy URL, `image.hash`), the deep-sleep time `refresh_s`, `listen_s` (0 after a timer wake, 20 s after input), `sleep` (`false` for mains devices, ADR 0051), the server `time` in Unix seconds and `firmware` (an update offer, otherwise `null`). An unknown key gets `401` and the device registers again; an invalid body gets `400`.
- **Text displays** (ADR 0044): register and sync like ePaper displays, but the sync answers `"image": null` and `"text": {"lines", "hash"}`, and `listen_s` is always 0 (they run on mains power and never sleep). Between syncs they poll `GET /api/device/text?shown=<hash>` with their key every `refresh_s`, which the plan decides (ADR 0045; the firmware syncs at most once a minute and uses one minute until its first sync): the answer is `{"lines", "hash"}`, unchanged text gets `204`. The poll renders the page the cursor points to without advancing it and writes nothing to `state/devices.json`, but updates the shown content in memory for `/api/devices` and the dashboard; an image display gets `409`. Notices (no plan, broken page, critical battery) are word-wrapped into the lines.
- **Buttons**: events apply in order before rendering. `next` = next page, `previous` = one page back (wrapping), `home` = first page of the active rule. After `page_timeout` (5 min) back to the first page. Touches and long presses are logged but change nothing yet.
- **Stale widgets**: when a plugin fails, Paperboy keeps showing its last widgets and marks the page: a bar „Veraltet: calendar/agenda“ (“Outdated: …”) on image displays, `!` in the last column of the first line on text displays (`notices: none` turns both off). A widget that never rendered becomes a hatched notice inside its cell with the reason.
- **Battery warning** (ADR 0020): fed by `battery.volts` of the sync. With `battery` in `defaults` or on a device, every page below `warn_volts` gets the bar „Akku schwach – bitte laden“ (“Battery low – please charge”); below `critical_volts` a full-screen image „Akku fast leer“ (“Battery almost empty”) appears instead, and buttons then do not page. A level ends only `hysteresis_volts` above its threshold. Without `battery` no warning. All texts use the configured `language`.
- **Firmware updates** (ADR 0043): put OTA images into `firmware/<hardware>/<hardware>-<version>.bin` of the instance (`firmware/esphome/build.ps1 -Hardware sticky -Instance <dir>` does it). The highest version (compared part by part as numbers) is offered in the sync as `{"version", "path", "md5"}` when `firmware.update` is `auto`, the device has no `firmware: hold`, its reported version differs (a lower file is a rollback), its battery reading is at least `min_battery_volts` and not at warning level (text displays run on mains power and skip the battery condition), and the version has not failed three times on that device. A sync that still reports the old version after an offer counts as a failure; a changed image (new MD5) or clearing `firmware_failures` in `state/devices.json` allows new attempts. Images are served at `GET /firmware/<hardware>/<file>` without a token; other file names are ignored and listed in the log and on the dashboard. `GET /api/devices` adds `firmware_available` and `firmware_status` (`current`, `offered`, `available`, `failed`, `held`, `none`). Test the rollback with a broken image before using `update: auto`.
- **Device logs**: the sync carries firmware log entries (`level`, `message`, optional `source`, `time` and `id`). Paperboy writes one line per entry with device, level, source and age; an entry with a `time` or `id` that a repeated sync delivers again is logged only once.
- `GET /api/devices` shows every configured device with registration, hardware, firmware, battery voltage (and `battery_percent`/`battery_mah` from a fuel gauge), signal strength, wake reason and last sync (without keys), plus what it is showing (ADR 0039): `last_page`, `last_page_kind` (`page`, `notice` or `battery`), `last_page_hash`, `last_page_url` and `last_refresh` (text displays: `last_text` instead of `last_page_url`), the last render or plan error in `last_error`, and a derived `status` (`ok`, `late`, `never` or `unregistered`). `late` means the last sync is more than twice the reported refresh old; devices deep-sleep, so an idle device is not an offline one.
- **Dashboard:** `GET /` redirects to `/ui`, a read-only overview of all devices (ADR 0040). Each card shows the status, the plan and its active rule, a preview of the last served page (the lines for text displays), refresh, last contact, battery and the last render or plan error. It is a server-rendered Go template without JavaScript, refreshes itself every 30 seconds and needs no token. The header shows the running build: version (or `dev`), short commit with `+dirty` for local changes, and commit date from the VCS stamp; the Go toolchain is in the tooltip.
- **Plan preview:** the *preview* link on a device card opens `/ui/plan?device=<id>` (ADR 0054), a read-only view of the device's plan: every rule with its time window, `rotate` and `refresh`, the fallback, the rule that applies now (or at the time picked with `&at=YYYY-MM-DDTHH:MM`) and the next rule change. Each page is previewed for the device's display type; pages the device would skip show the reason, broken pages their error. The view does not change the device's page cursor or state.
- **Security:** devices are admitted by allow list and use their own key; the device API answers with plain HTTP status codes (`400`, `401`, `403`). The dashboard, the preview and `GET /api/plugins` need no token, so keep Paperboy in a trusted network. Only plugins named in `paperboy.yaml` run, so the configuration file is executable content.

## Pages

A page is a grid of cells, and every cell shows one widget (ADR 0049): `pages/<page>.<display type>.yaml`, or `pages/<page>.yaml` for all display types without their own variant.

```yaml
title: Übersicht
grid: [6, 8]                 # columns, rows
style: { padding: 4.5, rule: 0.6, text_size: 16, divider: 0.6 }   # points
notices: show                # show (default) or none
cells:
  - { at: [0, 0], span: [6, 1], widget: paperboy/header }
  - { at: [0, 1], span: [3, 3], widget: weather/current, args: { clothing: true } }
  - { at: [3, 1], span: [3, 3], widget: evcc/site }
  - { at: [0, 4], span: [3, 4], widget: calendar/agenda, args: { lines: 7 }, style: { padding: 2 } }
  - { at: [3, 4], span: [3, 4], widget: weather/days, args: { days: 3 } }
```

- `widget` is `<plugin>/<widget>`; `args` go to the widget unchanged. `at` is the top-left cell (column, row, from 0), `span` the size in cells (default `[1, 1]`). Cells must stay inside the grid and must not overlap; empty cells stay white.
- On an image display the grid divides the pixels. Paperboy asks every plugin for its widgets in exactly the pixel size of the cell and the panel's gray levels, quantizes the PNG to the palette (nearest gray, no dithering) and pastes it 1:1, so nothing is ever scaled. A plugin that answers with another size gets a notice in its cell.
- `style` is in points (1 pt = 1/72 inch, ADR 0050) and is passed to every widget, converted to pixels with the display type's `ppi`: `padding` (inner margin, default 4.5 pt ≈ 1.6 mm), `rule` (separator width, default 0.6 pt ≈ 0.2 mm, never thinner than 1 px) and `text_size` (base text height, 4–144 pt, default 16 pt, at most 0.12 of the display height), so widgets of different plugins match. A cell's `style` overrides it. `divider` (points, at least 1 px) draws lines between the cells (Paperboy, not the widgets).
- The **built-in plugin** `paperboy` needs no configuration: `paperboy/header` (args `title` – default the page title –, `subtitle`, `clock`, `date` – weekday and date before the clock, in the longest of four forms that fits, e.g. „Samstag, 7. November 2026“, „Sa, 7.11.2026“, „Sa, 7.11.“, „7.11.“, left out when none fits –, `page_indicator`, `align`), `paperboy/text` (`text` with `\n` for line breaks, `align`, `size`, `bold`) and `paperboy/clock` (`format: time|date|datetime`).
- `kind: image` (default) or `kind: text` says which displays a page is for. A plan may serve image and text displays alike: when a device pages through a rule, pages that do not fit its display type are skipped – no variant and no shared page, the other kind, or a text grid that is not the display's character grid. Each skip is logged once per display type and page. When no page of the rule fits, the device gets a notice „Keine passende Seite“. A broken page is not skipped; it shows its error as a notice.
- Preview without a device: `GET /api/preview/<display type>/<page>` (PNG, or `text/plain` for text displays; optional `?at=YYYY-MM-DDTHH:MM` renders the page as at that time in the configured timezone). `GET /api/plugins` lists every plugin with its widgets, the sizes they are designed for, their arguments and the plugin's config keys.
- **Widget gallery:** `/ui/plugins` (linked from the dashboard) shows every widget of every plugin on a chosen display type, in each size the plugin declares, with its arguments and the plugin's config keys; text displays show the lines. Sizes are spans on a reference grid of 6 × 8 cells (`?grid=` changes it), and a size below the widget's minimum is flagged. Each widget has a form generated from its arguments (choices for `bool` and arguments with allowed values, text fields with the default as placeholder, lists comma-separated, optionally a span); *Apply* reloads the gallery with those arguments for that widget only, an invalid value shows as the widget's notice, and below the previews a ready-to-copy page cell lists the arguments that differ from their default. A single widget is available as `GET /api/widget/<plugin>/<widget>?display=<type>` with `span=3x2` (and `grid=6x8`) or `width`/`height` in pixels, and arguments as `arg.<name>=<value>` (JSON values keep their type, e.g. `arg.days=3`); it answers a PNG or `text/plain`, and a failing widget the notice a page would show, with the reason in `X-Widget-Error`. Neither needs a token.

### Text pages

A page for a text display (display type `kind: text`, ADR 0044) says `kind: text`; its grid is the character grid and must match the display size, and cells are measured in characters:

```yaml
kind: text
grid: [20, 4]
cells:
  - { at: [0, 0], span: [20, 1], widget: paperboy/header }
  - { at: [0, 1], span: [20, 1], widget: weather/current }
  - { at: [0, 2], span: [20, 2], widget: calendar/agenda }
```

Plugins answer with lines for the cell; Paperboy converts them to the display type's `charset`, cuts them to the cell width and pads them. `ascii` transliterates (ä → ae, ° → o, € → EUR), `hd44780` keeps `ä ö ü ß ° µ` for the firmware's ROM mapping, writes `Ä Ö Ü` as `A O U` (the ROM has no capital umlauts and the width must stay) and transliterates the rest. Set `charset: hd44780` for HD44780 displays; with the default `utf8` the firmware has to guess. A widget without a text variant shows its error in the cell.

The pages in `examples/pages` (`uebersicht`, `kalender`, `wetter`, `laden` and the 20 × 4 variants `uebersicht.lcd2004` and `laden.lcd2004`) use the shipped plugins; `examples/plans/standard.yaml` pages through them.

## Plugins

A plugin is a long-running program that provides widgets (ADR 0049). Paperboy starts it on first use, sends it its `config` section once (`configure`), and then asks it for widgets; it keeps the results until the plugin's `max_age` (default 5 minutes) or until the plugin reports a change, and renders widgets that are still in use again in the background, so a device never waits for a plugin. A plugin that exits or does not answer within `timeout` is restarted; meanwhile its last widgets are shown as stale, and a failing plugin is not asked again for a minute.

**Protocol** (`pkg/plugin`): one JSON object per line on stdin, one answer per line on stdout with the same `id`. Actions are `configure`, `describe`, `render` (`levels` 2 or 4, `widgets: [{widget, width, height, args, style}]` → base64 PNGs) and `text` (the same in characters → `lines`). Every request carries `language`, `timezone`, `now` and the `page` (`title`, `number`, `count`, display `width`/`height`, and for image displays `ppi`). Protocol version 3 (ADR 0050): each widget's `style` holds `padding`, `rule` and `text_px` in pixels, already converted from the page's points; Go plugins use `Request.TextPx`, `PtPx` and `MMPx` for physical sizes. Between answers a plugin may print `{"event": "changed", "changed": ["agenda"]}`. It ends when stdin closes. In `describe`, each argument and config key has `type`, `default`, `required`, `description` and optionally `enum`, the allowed values (ADR 0055); the Go helper rejects other values in `Values.Check` and per widget before rendering. Every plugin also sees `PAPERBOY_LANGUAGE` and `PAPERBOY_TIMEZONE` in its environment.

A Go plugin implements `plugin.Handler` (`Describe`, `Configure`, `Render`), optionally `Texter`, `Preparer`, `Starter` (background work with a `notify` callback) and `Ager` (`max_age`), calls `plugin.Serve` and draws with `pkg/render` – the fonts, gray levels, fills and icon masks Paperboy itself uses, so its widgets look like the rest of the page. Plugins in other languages speak the same JSON lines.

Shipped plugins (one zip per platform: `paperboy-plugins-linux-amd64.zip` and `paperboy-plugins-windows-amd64.zip`; unpacked in the instance directory they create `plugins/`):

- **`evcc`** – [evcc](https://evcc.io) over its REST API (`/api/state`), polled in the background (`refresh`, default `30s`, at least `10s`) with a change event when a shown value moves. Config: `url` (e.g. `http://evcc:7070`). Widgets: `site` (PV, home, grid with import/export, home battery or grid price; `columns: 2|4`), `flow` (energy flow diagram: boxes for PV, grid, home, home battery with state of charge and up to three active loadpoints, joined by arrows whose direction shows the flow and whose width grows with the power; idle links are dashed, so it reads in two gray levels; sizes follow the display ppi), `flow` (energy flow diagram: boxes for PV, grid, home, home battery with state of charge and up to three active loadpoints, joined by arrows whose direction shows the flow and whose width grows with the power; idle links are dashed, so it reads in two gray levels; sizes follow the display ppi), `loadpoint` (`index`, default 0: vehicle, mode, state-of-charge bar with the limit as a tick, range, charge power and phases, remaining time, session energy and solar share) and `forecast` (solar forecast as an area and the grid price as steps for the next `hours`, default 24; image only). `site`, `flow` and `loadpoint` have text variants (`flow`: PV, home, one `Car` line per active loadpoint, grid with + for import and - for export, battery with state of charge and + discharging / - charging).

- **`departures`** – real-time public transport departures from an EFA installation (ADR 0053), by default the Westfalenfahrplan (Lippe Mobil, OWL) without an API key. Config: `stop` (required: stop name, resolved once, or stop ID like `de:00000:1001`), `lines` and `directions` (filters; `directions` matches words in the destination), `max` (default 8), `refresh` (default `60s`, at least `30s`), `walk_minutes` (hide departures you cannot reach any more), `base` (another EFA installation, e.g. `https://efa.vrr.de/standard`) and `backend` (only `efa`). Widget `departures`: line, destination, minutes until departure (a clock time from one hour on; `time: auto|minutes|clock`), delay `+N`, cancelled trips struck through with „Ausfall“, `header: true` adds the stop name; the text variant fits 20 × 4 displays (`examples/pages/abfahrten.lcd2004.yaml`). It reports a change when a departure, a delay or a cancellation changes. The Westfalenfahrplan publishes no terms of use for this interface; keep the refresh moderate and check the terms of any other installation you point it at.
- **`weather`** – [Open-Meteo](https://open-meteo.com/), no API key. Config: `lat`, `lon` (required), `refresh` (default `15m`), `base`. Widgets: `current` (pictogram, temperature, condition, apparent temperature, wind force, today's range; `clothing: true` adds a T-shirt or pullover – pullover below 15 °C felt or above wind force 3), `days` (`days: 1..5`, pictogram and range per day), `hours` (today's temperature with precipitation probability; image only). The pictograms come from [OpenMoji](https://openmoji.org) (CC BY-SA 4.0, see `cmd/weather/icons/README.md`).
- **`calendar`** – CalDAV and iCalendar (ADR 0048). Config: `caldav`, `user`, `password` (use `${VAR}`), `calendars` (names; default all, an unknown name fails and lists the available ones), `ics` (links, `webcal://` works), `ics_files`, `days` (default 14), `max_events`, `refresh` (default `15m`). For iCloud create an app-specific password at [account.apple.com](https://account.apple.com) and use the Apple ID as `user`; Nextcloud works with e.g. `https://cloud.example.org/remote.php/dav`. Recurring events are expanded by the plugin; unsupported rules (hourly, `BYWEEKNO`, `BYYEARDAY`) skip that event with a warning. Widgets: `agenda` (`lines`, `headings`, `location`; `days` limits the range to today plus the following days, 1 to the configured `days` (values outside are clamped), e.g. `days: 2` for today and tomorrow; `empty` (default true when `days` is set) keeps days without events with „Keine Termine“/"No events"; day headings, all-day events first, events running into the range from earlier days count, ended events disappear, „+N weitere“ on overflow) and `month` (month grid with titles or event bars; `titles: false`) and `week` (one column per day; `days` 1-14, default 7, e.g. 5 for a work week; `start: week|today`, where the start of the week is Monday for German and Sunday otherwise; headings with weekday and date, today inverted, all-day and multi-day events as bars across their days, timed events with start time and ellipsized title, "+N weitere" on overflow; the text variant lists one line per day starting today). The plugin reads the calendars in the background and reports changes.
- **`pokedex`** — [PokéAPI](https://pokeapi.co), no API key. Config: `pokemon` (name or national dex number), `mode` (`fixed`, the default when `pokemon` is set, or `random`), `rotate` (how often a random Pokémon changes, default `24h`, at least `1h`; aligned to local midnight and stable across restarts), `generations` (e.g. `[1, 2, 3]`, limits the random pick), `base`. Widget `card` (image and text): artwork, name in the request language (species names, English as fallback), number, types, height and weight, the six base stats as bars and the flavor text in the request language. The artwork is downloaded at runtime and reduced to the display's gray levels with error diffusion; it is never stored or shipped. Responses are cached in memory, requests are serialized with a pause between them, and the plugin reports a change when the Pokémon rotates. Pokémon and their artwork belong to Nintendo, Game Freak and The Pokémon Company; the plugin only fetches them for private display.
- **`xkcd`** - [xkcd](https://xkcd.com) comics from its JSON API (`info.0.json`), the image downloaded from the comic's `img` URL in the background with a change event when another comic is shown. Config: `comic` (`latest`, default, `random` or a comic number), `refresh` (how often `latest` is checked, default `1h`, at least `1m`), `rotate` (how often `random` picks a new comic, default `24h`), `base`. Widget `comic` (`title: true` adds number and title above, `alt: true` the alt text below the comic): the image is scaled to fit the cell with its aspect ratio kept (Catmull-Rom on gray values, transparent areas white), then quantized to the display's gray levels, so very wide strips and very tall comics only shrink; text sizes follow the page's base text. The text variant shows `#number title` and the wrapped alt text. The comics are by Randall Munroe and licensed under [CC BY-NC 2.5](https://creativecommons.org/licenses/by-nc/2.5/); Paperboy does not redistribute them, but keep the attribution and non-commercial terms in mind when you show them.
- A SolarAI plugin is still to come; it talks to SolarAI over HTTP itself, and SolarAI needs to know nothing about Paperboy.

## Plans

Which pages a device shows is decided by a plan, `<name>.yaml` in `plans/`. A device points to it with `plan: wohnzimmer`; several devices may share a plan. A plan only names pages, so it works for devices of different display types.

```yaml
rules:
  - name: morgen
    when: { from: "07:00", to: "09:00", days: [mo, di, mi, do, fr] }
    pages: [kalender, uebersicht]
  - name: tag
    when: { from: "09:00", to: "19:00" }
    pages: [uebersicht, wetter, solar]
    rotate: true
    refresh: 5m
  - name: nacht
    when: { from: "19:00", to: "07:00" }
    pages: [uebersicht]
    refresh: 30m
fallback: nacht
```

- The first matching rule wins. A `when` without `from`/`to` always matches; `from` is inclusive, `to` exclusive. `to < from` wraps over midnight and then belongs to the day the window starts. Days accept German and English short names.
- Times use the global `timezone` from the configuration (ADR 0033); plans have no timezone of their own, and a `timezone` key is rejected.
- `pages` is the paging order within the rule. `rotate: true` moves on at every timed refresh, otherwise only a button changes the page. Buttons page within the active rule only, holding returns to its first page, and after `page_timeout` the device shows the first page again unless the rule rotates.
- `refresh` replaces the device interval. The reported interval is capped to the next rule boundary (but never below the floor of the device's power source), so a sleeping device wakes up for a rule change.
- `fallback` names the rule used when nothing matches; without a match and without a fallback the device gets a notice page.
- Plans are reloaded on change without a restart; a broken file keeps the last valid version. A missing or broken page also leads to a notice page, so devices never see a server error.
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
.\scripts\build.ps1             # paperboy, odtest and the plugins for Linux amd64/arm64 and Windows into bin/
./scripts/package-plugins.sh dist  # the plugin zip for the host platform (GOOS/GOARCH select another)
```

The dashboard header shows version, commit and commit date, straight from Go's VCS stamping. `go build` and `scripts/build.ps1` embed them as long as the Git repository is present. The container image therefore keeps `.git` in the build context and installs the git binary in its build stage (the alpine Go image ships none); the final image contains only the Paperboy binary. The *Plugins* workflow builds the plugin zips.

A build without a repository or with `-buildvcs=false` reports `dev`. `go version -m <binary>` shows what was stamped. Untagged builds show `dev` plus commit; `git tag v0.1.0` gives release builds a version name.

```text
cmd/paperboy/            Startup, options and example configuration
cmd/weather/             Weather plugin: Open-Meteo (ADR 0049)
cmd/calendar/            Calendar plugin: CalDAV and iCalendar (ADR 0048, 0049)
cmd/evcc/                evcc plugin: energy flow, charging point, forecast
cmd/xkcd/                xkcd plugin: comic with optional title and alt text
cmd/departures/          Departures plugin: EFA real-time departures (ADR 0053)
cmd/odtest/              Test program for OpenDisplay devices (TCP and Bluetooth)
pkg/plugin/              Plugin protocol and Go helper (public API)
pkg/render/              Fonts, palettes and drawing shared by Paperboy and plugins (public API)
internal/paperboy/       YAML configuration, device API, plans, battery warning, dashboard
internal/page/           Pages: grid parsing and composition from widgets
internal/widget/         Plugin processes, widget cache and background refresh
internal/builtin/        Built-in plugin "paperboy": header, text, clock
internal/plan/           Display plans: time rules that select pages per device (ADR 0025)
internal/opendisplay/    OpenDisplay protocol; odble = Bluetooth transport
firmware/esphome/        Paperboy firmware on ESPHome for Sticky and E1001 (ADR 0041)
docs/adr/                Architecture decisions
```
