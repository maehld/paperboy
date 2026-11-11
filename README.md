# Paperboy

Paperboy is a small display service for ePaper and text displays. A page is a grid of widgets, and every widget comes from a plugin: a long-running program that draws it in exactly the size of its cell (ADR 0049). Paperboy composes the pages, chooses them per device with plans and delivers them to devices running the Paperboy firmware on ESPHome (ADR 0041; deep sleep, battery operation) over its own device API (ADR 0042).

Paperboy evolved from SolarAI's display service `displayd` (ADR 0028); the Git history of the carried-over files is preserved.

```text
Plugin (calendar, weather, …) ◀──JSON lines on stdin/stdout──▶ paperboy ◀──POST /api/device/sync── Display (Paperboy firmware)
```

## Quick start

1. Create an instance directory with the fixed layout (ADR 0037): `paperboy.yaml` plus `displays/`, `pages/`, `plans/`, `devices/` and, for your own plugins, `plugins/`. Copy `cmd/paperboy/paperboy.example.yaml` to `<instance>/paperboy.yaml`, configure the plugins, copy the display types from `examples/displays` (or create them in the editor) and create one file per display under `devices/` (ADR 0070): the MAC address is the allow list, unknown devices are rejected and appear as unclaimed hardware in the devices tab, where *Claim* asks first and then opens a new device file with the MAC prefilled (ADR 0080).
2. Copy pages from `examples/pages`, a plan from `examples/plans` and a device from `examples/devices` into the instance, then set `plan: standard` on the device (or assign it in the editor). Without a plan a device only shows a notice page.
3. Put the plugins into the instance's `plugins/`: unpack `paperboy-plugins-<os>-<arch>.zip` (attached to each release, otherwise an artifact of the *Plugins* workflow) in the instance directory, or build them yourself (`./scripts/package-plugins.sh` builds the zip locally), or let Paperboy fetch them with a `download` section (see [Downloads](#downloads)). The zip creates versioned programs such as `plugins/weather-v1.4.0` (ADR 0075). Then start:
   ```sh
   go run ./cmd/paperboy -config . -listen :8090
   # look at a page without a device:
   curl -o page.png http://127.0.0.1:8090/api/preview/e1001/uebersicht
   ```
   Alternatively as a container: `Dockerfile` and `compose.example.yaml`. The image contains only Paperboy; use the `linux/amd64` plugin zip in the mounted instance directory.
4. Flash the Paperboy firmware (ADR 0041, see [firmware/esphome/README.md](firmware/esphome/README.md)); the release images are in `paperboy-firmware.zip`, or let Paperboy fetch them (see [Downloads](#downloads)). Enter `http://<computer>:8090` as the Paperboy URL in the device's web UI. The device registers with its MAC address; unknown MACs are rejected and logged.

**Migrating from Paperboy v1:** v2 has no data API, no exec providers and no upload token (ADR 0049). Remove the `server` and `providers` sections from `paperboy.yaml` (both abort the start with a hint) and add `plugins` instead; the weather and calendar providers are plugins now and take their settings from `config` instead of flags. Rewrite the pages as grids (see below); old templates with `rows` and `blocks` are rejected and show a notice page. `state/data.json` and the `icons/` directory are no longer used. Device state moved from `state/devices.json` to `state/devices/<MAC>.json` and plugin state from the plugin-written directory to `state/plugins/<id>/`; both change without migration (ADR 0080, 0081), so devices get new keys on their next sync and plugins start empty unless the files are moved by hand.

**Migrating to physical sizes (ADR 0050):** replace `width_mm` with `ppi` in every image display type (`ppi = pixels × 25.4 / mm` of the same side; a leftover `width_mm` aborts the start with a hint) and rewrite page styles in points: `text_size` is no longer a fraction (`0.06` is rejected; at 125 ppi, 0.06 of 480 px is about 16 pt), and `padding`, `rule` and `divider` are points now (8 px ≈ 4.5 pt, 1 px ≈ 0.6 pt at 125 ppi). Old pixel values there still parse but get bigger, so check them. Third-party plugins have to be rebuilt against protocol version 3 (`style.text_size` is gone; use `text_px` or `Request.TextPx`).

**Migrating to program names (ADR 0060):** remove `command:` from every plugin section — it aborts the start with a hint. A plugin's program is now named after the plugin: `plugins/weather`, `plugins/weather-v2.1.0` (`.exe` on Windows), or `weather` on `PATH`; the unversioned file wins and stays yours (an unpacked plugin zip works as before), and the optional `version` key pins a downloaded one. Firmware images move from `firmware/<hardware>/<hardware>-<version>.bin` to `firmware/<hardware>-v<version>.bin`; the old layout is still read, so nothing has to move, and devices get the new path with the offer.

**Migrating to display type files (ADR 0071):** the `display_types:` section of `paperboy.yaml` moved to `displays/<name>.yaml`, one file per display type; a leftover section aborts the start with a hint. Split the map into files without the map level, e.g. `displays/e1001.yaml` with `kind: image`, `size: [800, 480]`, `gray_levels: 4`, `ppi: 125`. The name (the file name) is what page variants and `display_type:` in the device files use, so it does not change. Renaming a display type later is done in the display type editor (ADR 0079): the file moves, the page variants and device files keep the old name and are updated there.

**Migrating to plugin protocol version 4 (ADR 0057):** the action menu moves the protocol to version 4. Paperboy checks the version in every answer (ADR 0058) and refuses a plugin that reports version 3 or none, with the expected version in the error; unpack the current plugin zip after updating Paperboy. Plans without `actions` need no change.

**Migrating to plugin protocol version 5 (ADR 0081):** plugin state is part of the protocol now, and blobs replace the writable `state/plugins/<plugin>/` directory. Paperboy refuses version 4 plugins, so unpack the current plugin zip; the plugin state starts empty (for myskoda: the last vehicle answer and the render image) and is rebuilt on the next load. From version 5 on the protocol only grows additively, so later Paperboy releases keep the plugins of this one working.

**Migrating devices to `devices/` (ADR 0070):** the `devices:` section of `paperboy.yaml` moved into `devices/<id>.yaml`, one file per device. A leftover section aborts the start with a hint. For every entry, create `devices/<id>.yaml` with the keys of the entry (without the map level) and remove the section, or create the devices in the editor at `/ui/editor` and delete the section afterwards:

**Migrating to plugin versions (ADR 0075):** every plugin carries its own version now (`cmd/<plugin>/VERSION`), and the zip names the programs `<plugin>-v<version>` instead of `<plugin>`. A program that an older zip unpacked as `plugins/weather` still wins over every download and is never updated — delete it once if Paperboy should fetch that plugin. A `version:` pin names a plugin version (`v1.4.0`), not a release tag; a pin that still names a tag fails with "no release carries v…".

## Releases

GitHub releases are the distribution point (ADR 0056). Each tag names a tested combination of the three deliverables:

| Tag | Contains |
|---|---|
| `vX.Y.Z` | Paperboy binaries, plugin zips and the firmware bundle |
| `plugins-vX.Y.Z` | plugin zips only |
| `firmware-vX.Y.Z` | firmware bundle only (`X.Y.Z` is the highest `fw_version` in the bundle) |

The tag names the release, not what is inside: every plugin and every firmware image carries its own version, raised only when it changed (ADR 0043, 0075).

| Asset | Use |
|---|---|
| `paperboy-linux-amd64`, `paperboy-linux-arm64`, `paperboy-windows-amd64.exe` | the Paperboy binary for that platform |
| `paperboy-plugins-<os>-<arch>.zip` | unpack in the instance directory: creates `plugins/` with `<plugin>-v<version>` of every shipped plugin |
| `paperboy-firmware.zip` | unpack in the instance directory: creates `firmware/` with `<hardware>-v<version>.bin` of every hardware type (ADR 0043, 0059, 0060, 0063) |
| `SHA256SUMS` | checksums of all assets of the release |

The container image is published separately as `ghcr.io/maehld/paperboy` (`X.Y.Z`, `X.Y` and `latest`). A release is created by pushing a tag, for example `git tag v2.1.0 && git push origin v2.1.0`; the *Release* workflow builds everything and attaches it to one release.

## Downloads

With a `download` section Paperboy fetches the plugin programs and firmware images from the releases itself (ADR 0060) and verifies every file against the release's `SHA256SUMS`:

```yaml
download:
  source: https://github.com/maehld/paperboy  # optional, this is the default
  token: ${GITHUB_TOKEN}                     # optional: rate limits, private forks
  interval: 24h                              # optional: between checks, at least 1h
```

- It runs at startup and then after `interval` (default 24 h), in the background; the *Downloads* page (`/ui/downloads`) has a **Check now** button that starts a check at once. An instance without internet keeps working as before, and nothing else needs the section.
- **Plugins**: a configured plugin whose program is missing gets the newest one as `plugins/<plugin>-v<version>` (`weather-v1.4.0`, `weather-v1.4.0.exe` on Windows). A downloaded program is updated when the plugin zip ships a higher version of that plugin — an unchanged plugin is left alone, however many releases pass (ADR 0075). The old file stays and only that plugin restarts. A `version` pin in the plugin section names a plugin version (`v1.4.0`) and is never moved.
- **Firmware**: every image of the bundle lands in `firmware/<hardware>-v<version>.bin` (ADR 0061); a `firmware.version` pin picks the release that carries exactly that image, which is also how a rollback is pinned. Devices are still only offered an update with `firmware.update: auto`.
- A program **without** a version in `plugins/` (`weather`) always wins - it is your build - and is neither replaced nor pinned; download it by hand or delete it to hand the plugin back to the releases.
- Existing files are never overwritten or deleted, so versions pile up and are removed by hand. Deleting a newer plugin file rolls back to the older one for the rest of the server run; a `version` pin keeps the older one across restarts, and a firmware pin keeps the older image on offer.
- The *Downloads* page shows the source, the last check, the running check, the program and version behind every plugin, the images in `firmware/` and the firmware each device reports; the devices page only warns when the last check failed.

## Instance directory

Paperboy is configured with one instance directory, `-config <dir>` (default `.`). Its layout is fixed (ADR 0037, ADR 0049):

```text
<dir>/
  paperboy.yaml      configuration
  displays/          display types <name>.yaml (ADR 0071)
  pages/             pages <page>.<display type>.yaml or <page>.yaml
  plans/             plans <name>.yaml
  devices/           devices <id>.yaml (ADR 0070)
  plugins/           plugin programs <plugin>-v<version>, e.g. from paperboy-plugins-<os>-<arch>.zip
  firmware/          OTA images <hardware>-v<version>.bin (ADR 0043, 0060)
  state/             written by Paperboy only
    devices/         state of every MAC: <MAC>.json with API key, page cursor, battery and the address of the last contact (ADR 0080, 0082)
    plugins/         plugin state <id>/state.json and the declared blobs <id>/blobs/<name> (ADR 0081)
    download/        cached release list and downloads (ADR 0060)
    editor/          backups of pages, plans, devices and display types written or deleted by the editor (ADR 0066, 0072)
```

- `paperboy.yaml`, `displays/`, `pages/`, `plans/`, `devices/`, `plugins/` and `firmware/` are yours. Paperboy only reads them; only the download feature (ADR 0060) adds plugin and firmware files there. The editor writes display types, pages, plans and devices through the API (ADR 0066, 0070, 0071).
- `state/` is created at startup and is the only directory Paperboy writes. Back up `state/devices/` anyway: it holds the API keys, page cursors, battery levels, the address of the last contact and last served pages; without it the devices get new keys on their next sync (401, then registration by MAC). A state file whose MAC no device file uses is unclaimed hardware, shown in the devices tab of the editor with *Claim* (asks first) and *Forget*; the state follows the MAC, so renaming or deleting a device file does not touch it.

## Configuration

`paperboy.yaml` is the only configuration format (ADR 0026) and lives in the instance directory. Parsing is strict: unknown keys, duplicate MAC addresses and unsupported protocols abort the start. A plan that is missing or broken does not: it is logged and the device gets a notice page, because plan files may be added while the server runs (ADR 0047).

```yaml
language: de            # de or en; default: system language
timezone: Europe/Berlin # IANA zone; default: system timezone
plugins:
  calendar:              # the program is plugins/calendar-v1.0.0 or calendar on PATH (ADR 0060, 0075)
    version: v1.0.0      # optional: pin plugins/calendar-v1.0.0, else the highest version
    timeout: 1m          # per request; default 30s
    env: { HTTPS_PROXY: "http://proxy:3128" }   # optional extra environment
    config:              # sent to the plugin once after its start
      caldav: https://caldav.icloud.com
      user: me@icloud.com
      password: ${PAPERBOY_CALDAV_PASSWORD}
defaults:
  refresh: 5m          # regular sleep time; plans may override it
  page_timeout: 5m     # back to the first page after this
  menu_show_duration: 30s  # how long the device menu stays open (ADR 0057), 5s–60s
  battery: { warn_volts: 3.45, critical_volts: 3.3, hysteresis_volts: 0.1 }
firmware:
  update: auto         # auto or off (default); offers the images in firmware/ (ADR 0043)
  min_battery_volts: 3.6
  version: v1.0.0      # optional: pin the offered image version (ADR 0060)
download:              # optional: fetch plugins and firmware from the releases (ADR 0060)
  source: https://github.com/maehld/paperboy
  token: ${GITHUB_TOKEN}
  interval: 24h        # optional: between checks, at least 1h (ADR 0062)
# No display types here: they are files under displays/ (ADR 0071), see below.
# No devices here: they are files under devices/ (ADR 0070), see below.
```

- The devices moved out of this file into `devices/<id>.yaml`, one file per device (ADR 0070); a leftover `devices:` section aborts the start with a hint. The device files are listed under [Devices](#devices).
- `plugins` declares the plugins (ADR 0049). `config` is the plugin's own section; string values expand `${VAR}` from Paperboy's environment (an unset variable aborts loading, `$$` is a literal `$`), so secrets stay out of the file. The plugin checks its keys and rejects unknown ones; a rejected configuration shows up as a notice in the plugin's cells and in the log. The name `paperboy` is reserved for the built-in plugin.
- `language` (`de` or `en`) selects the language of the built-in image texts, the number format and the language plugins draw in. Without it Paperboy uses the system language (Windows UI language, otherwise `LANG`/`LC_ALL`/`LC_MESSAGES`) and falls back to English. `timezone` (IANA name) applies to plans and built-in dates; without it the system timezone is used.
- `-config` names the instance directory (default `.`); Paperboy reads `<dir>/paperboy.yaml` and finds all other directories there. A file argument is rejected with an explanation, and a `.json` configuration is no longer read.
- The configuration is reloaded on change without a restart (ADR 0047): handlers pick up a modified `paperboy.yaml` or a changed file under `displays/` or `devices/` on the next request, plugins whose section changed are restarted (their cached widgets are dropped), and a broken file keeps the last valid configuration while a missing one leaves the current configuration alone. With no usable file at startup in an existing instance directory the server still starts (degraded: no display types, no devices) and applies the file as soon as it loads, even when its mod time did not change; a wrong `-config` stays fatal.

## Display types

A display type is `displays/<name>.yaml` (ADR 0071). The file name is the name pages and devices use: it is the suffix of `pages/<page>.<name>.yaml` and the `display_type:` of a device. The files are parsed as strictly as pages and plans, a broken one aborts the start and names the file:

```yaml
kind: image
size: [800, 480]
gray_levels: 4     # 1 or 4
ppi: 125           # pixels per inch, required for image displays (800 px / 163 mm)
min_refresh: 60s   # never report a shorter interval
power: auto        # auto (default), battery or mains (ADR 0051)
```

- A display type with `kind: text` is a text display (ADR 0044): `size: [columns, rows]` in characters (at most 80 × 24) and `charset: utf8|ascii|hd44780` (default `utf8`); `gray_levels` and `ppi` are rejected for it.
- An image display type needs `ppi`, its pixel density (pixels per inch, 20–1000; ADR 0050). It does not depend on the orientation: compute it from either side, `ppi = pixels × 25.4 / millimetres` of the active area. Page styles are in points and are converted with it, so a widget has the same physical size on a small dense panel and a large coarse one.
- Display types are created, edited and renamed at `/ui/editor?tab=displays` (ADR 0071, ADR 0079); the files stay the source of truth, so a hand edit is picked up on the next request. A rename moves `displays/<name>.yaml` and nothing else: the confirmation names the page variants and devices that keep the old name, and those stay editable — a device whose display type is missing answers with 503 until its file is fixed, and an orphaned page variant opens through a configured type of the same kind and can be saved as that type's variant. Deleting works in the editor (ADR 0073): the confirmation names the page variants and devices that keep pointing at the missing type, and a copy stays under `state/editor/`.
- At least one display type has to exist for the instance to start; a file that does not parse or validate aborts the start and names the file. No device has to use a type, and a page variant of a type that is not configured is listed with its error instead of disappearing.

## Devices

A device is `devices/<id>.yaml` (ADR 0070). The file name is the stable device ID (a-z, 0-9, `-`); it appears in logs and in `GET /api/devices`, while its state lives under its MAC in `state/devices/` (ADR 0080). The files are parsed as strictly as pages and plans, a broken one aborts the start and names the file:

```yaml
display_type: e1001
protocol: paperboy      # the default; devices running the Paperboy firmware (ADR 0042)
mac: "AA:BB:CC:DD:EE:01"
plan: standard          # plans/standard.yaml; without one a notice page
refresh: 10m            # optional, else defaults.refresh
power: mains            # optional, overrides the display type (E1001 on USB, ADR 0051)
battery: { warn_volts: 3.5 }   # optional; empty values inherit defaults.battery
firmware: hold          # optional: keep the device on its firmware (ADR 0043)
actions: allow          # optional: allow (default) or deny the device menu (ADR 0057)
menu_show_duration: 30s # optional, overrides defaults.menu_show_duration
```

- `protocol` accepts only `paperboy` (the default) for now: devices running the Paperboy firmware (ADR 0042). `trmnl` was removed and aborts the start, as do the v1 sections `server` and `providers` (ADR 0049). `opendisplay` is rejected until plans cover it; `text-http`/`image-http` were replaced by the device API (ADR 0044) and abort the start.
- `plan` should name an existing `plans/<name>.yaml`; a missing or broken plan is logged and shows a notice page instead of aborting the start, so a plan file can be added while the server runs (ADR 0047). Without `plan` the device shows a notice page in the configured language („Kein Plan konfiguriert“ / “No plan configured”).
- Devices are created and assigned in the editor at `/ui/editor` (ADR 0070); the files stay the source of truth, so a hand edit is picked up on the next request.
- Paperboy keeps the source address of the last register, sync or text poll in the device's state file (`client_ip`, ADR 0082) and shows it on the dashboard, in the devices tab and in `GET /api/devices`. It comes from the connection, never from a header, so a reverse proxy in front of Paperboy would show its own address.

## Behavior

- **Display types** (`displays/<name>.yaml`, ADR 0071: size, grayscale levels, pixel density, minimum interval) determine the layout; `GET /api/profiles` lists them. Several devices can share a display type. Pages are delivered as PNG with 1 bit or 2 bits (4 gray levels).
- **Plans** (ADR 0025): every device points to a plan (`plans/<name>.yaml`) that selects its pages according to time rules. A device without a plan shows a notice page.
- **Interval**: `defaults.refresh` (at least 1 minute) or the device's `refresh`; a plan rule may override it (down to `1s`). The reported interval never goes below the floor of the device's power source (ADR 0051): battery devices keep one minute or the display type's `min_refresh` if that is longer; mains devices go down to `min_refresh`, by default `1s` for text and `10s` for image displays. Unchanged image → the display does not redraw.
- **Power source** (ADR 0051): `power: auto|battery|mains` on the display type, overridable per device; default `auto`. The firmware reports `power` (`battery` or `mains`) in register and sync: the Sticky measures USB power on GPIO9, the E1001 cannot detect it and reports `battery`, text displays report `mains`. `auto` uses the last report (`reported_power` in `/api/devices`, next to the effective `power`) and behaves as `battery` until the device reports one, so text displays with older firmware fall back to a one-minute floor until they are updated or set to `power: mains`. The sync answers `sleep: false` for mains devices: the ePaper firmware then stays awake with Wi-Fi up and syncs again after `refresh_s` instead of deep sleeping. A page whose composition did not change keeps its PNG and hash, so short intervals cause no redraw.
- **Device API** (ADR 0042, ADR 0064): `POST /api/device/register` with `{"mac", "hardware", "firmware"}` (optionally `power`) returns `{"key", "device"}` for a configured MAC (an existing key is kept) and `403` otherwise. Every wake sends one `POST /api/device/sync` with `Authorization: Bearer <key>`: `reason` (`timer`, `button`, `touch`, `powercycle`, `menu`, `result`), `events` (buttons by role `next`, `previous`, `home`, optionally `hold_ms`; touches with `x`/`y`; each with a monotonically increasing `id`, and a repeated ID is ignored), `shown` (hash on the panel), `battery` (`volts` required, `percent`, `mah`, `ma` optional), `rssi`, `power` and `log` entries. The answer names the image (`image.path` relative to the Paperboy URL, `image.hash`), the deep-sleep time `refresh_s`, `listen_s` (0 after a timer wake, 20 s after input), `sleep` (`false` for mains devices, ADR 0051), the server `time` in Unix seconds, `firmware` (an update offer, otherwise `null`) and `wake` (`{reason, after_s}`, a one-shot wake for the menu deadline or an action result, otherwise absent). An unknown key gets `401` and the device registers again; an invalid body gets `400`.
- **Text displays** (ADR 0044): register and sync like ePaper displays, but the sync answers `"image": null` and `"text": {"lines", "hash"}`, and `listen_s` is always 0 (they run on mains power and never sleep). Between syncs they poll `GET /api/device/text?shown=<hash>` with their key every `refresh_s`, which the plan decides (ADR 0045; the firmware syncs at most once a minute and uses one minute until its first sync): the answer is `{"lines", "hash"}`, unchanged text gets `204`. The poll renders the page the cursor points to without advancing it and writes nothing to the device state file, but updates the shown content in memory for `/api/devices` and the dashboard; an image display gets `409`. Notices (no plan, broken page, critical battery) are word-wrapped into the lines.
- **Buttons**: events apply in order before rendering. `next` = next page, `previous` = one page back (wrapping), long `next`/`previous` = first page of the active rule, `home` = the device menu (ADR 0057, ADR 0067, see [Actions](#actions)): short `home` always opens it — even when the plan offers no actions, then it is an empty banner —, `next`/`previous` move the selection, `home` confirms, long `home` closes it and keeps the page. After `page_timeout` (5 min) back to the first page. Touches operate the device menu when it is open (the Sticky sends taps, ADR 0064, issue #54); outside a menu they are only logged. The server decides at 700 ms whether a press was long; the firmware only measures and reports `hold_ms`.
- **Stale widgets**: when a plugin fails, Paperboy keeps showing its last widgets and marks the page: a bar „Veraltet: calendar/agenda“ (“Outdated: …”) on image displays, `!` in the last column of the first line on text displays (`notices: none` turns both off). A widget that never rendered becomes a hatched notice inside its cell with the reason.
- **Battery warning** (ADR 0020): fed by `battery.volts` of the sync. With `battery` in `defaults` or on a device, every page below `warn_volts` gets the bar „Akku schwach – bitte laden“ (“Battery low – please charge”); below `critical_volts` a full-screen image „Akku fast leer“ (“Battery almost empty”) appears instead, and buttons then do not page. A level ends only `hysteresis_volts` above its threshold. Without `battery` no warning. All texts use the configured `language`.
- **Firmware updates** (ADR 0043): put OTA images into `firmware/<hardware>-v<version>.bin` of the instance (`firmware/esphome/build.ps1 -Hardware sticky -Instance <dir>` does it). The highest version (compared part by part as numbers; a `firmware.version` pin narrows it to exactly that image, ADR 0060) is offered in the sync as `{"version", "path", "md5"}` when `firmware.update` is `auto`, the device has no `firmware: hold`, its reported version differs (a lower file is a rollback), its battery reading is at least `min_battery_volts` and not at warning level (text displays run on mains power and skip the battery condition), and the version has not failed three times on that device. A sync that still reports the old version after an offer counts as a failure; a changed image (new MD5) or clearing `firmware_failures` in the device's state file `state/devices/<MAC>.json` allows new attempts. Images are served at `GET /firmware/<file>` without a token; other file names are ignored and listed in the log and on the dashboard. `GET /api/devices` adds `firmware_available` and `firmware_status` (`current`, `offered`, `available`, `failed`, `held`, `none`). Test the rollback with a broken image before using `update: auto`.
- **Device logs**: the sync carries firmware log entries (`level`, `message`, optional `source`, `time` and `id`). Paperboy writes one line per entry with device, level, source and age; an entry with a `time` or `id` that a repeated sync delivers again is logged only once.
- `GET /api/devices` shows every configured device with registration, hardware, firmware, battery voltage (and `battery_percent`/`battery_mah` from a fuel gauge), signal strength, wake reason and last sync (without keys), plus what it is showing (ADR 0039): `last_page`, `last_page_kind` (`page`, `notice` or `battery`), `last_page_hash`, `last_page_url` and `last_refresh` (text displays: `last_text` instead of `last_page_url`), the last render or plan error in `last_error`, and a derived `status` (`ok`, `late`, `never` or `unregistered`). `late` means the last sync is more than twice the reported refresh old; devices deep-sleep, so an idle device is not an offline one.
- **Dashboard:** `GET /` redirects to `/ui`, a read-only overview of all devices (ADR 0040). Each card shows the status, the plan and its active rule, a preview of the last served page (the lines for text displays), refresh, last contact, battery and the last render or plan error. It is a server-rendered Go template without JavaScript, refreshes itself every 30 seconds and needs no token. The header shows the running build: version (or `dev`), short commit with `+dirty` for local changes, and commit date from the VCS stamp; the Go toolchain is in the tooltip.
- **Plan preview:** the *preview* link on a device card opens `/ui/plan?device=<id>` (ADR 0054), a read-only view of the device's plan: every rule with its time window, `rotate` and `refresh`, the fallback, the rule that applies now (or at the time picked with `&at=YYYY-MM-DDTHH:MM`) and the next rule change. Each page is previewed for the device's display type; pages the device would skip show the reason, broken pages their error. The view does not change the device's page cursor or state.
- **Security:** devices are admitted by allow list and use their own key; the device API answers with plain HTTP status codes (`400`, `401`, `403`). The dashboard, the editor, the preview, `GET /api/plugins` and the page and plan API need no token, so keep Paperboy in a trusted network; whoever reaches `/ui` can change the pages and plans. Only plugins named in `paperboy.yaml` run, so the configuration file is executable content.

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
- The **built-in plugin** `paperboy` needs no configuration and provides `paperboy/header`, `paperboy/text` and `paperboy/clock` (see [docs/plugins/paperboy.md](docs/plugins/paperboy.md)).
- `kind: image` (default) or `kind: text` says which displays a page is for. A plan may serve image and text displays alike: when a device pages through a rule, pages that do not fit its display type are skipped – no variant and no shared page, the other kind, or a text grid that is not the display's character grid. Each skip is logged once per display type and page. When no page of the rule fits, the device gets a notice „Keine passende Seite“. A broken page is not skipped; it shows its error as a notice.
- Preview without a device: `GET /api/preview/<display type>/<page>` (PNG, or `text/plain` for text displays; optional `?at=YYYY-MM-DDTHH:MM` renders the page as at that time in the configured timezone). `GET /api/plugins` lists every plugin with its widgets, the sizes they are designed for, their arguments and the plugin's config keys.
- **Widgets:** `/ui/plugins` (linked from the dashboard) opens the widgets page, the page editor without a page: pick a widget in the palette – or open it without one and it starts with the built-in header – and it is drawn in its cell size on the chosen display type, with its size, arguments and style in the inspector and the finished cell in the snippet panel, ready to copy into a page. Sizes are spans on a reference grid of 6 × 8 cells (`?grid=` changes it). A single widget is available as `GET /api/widget/<plugin>/<widget>?display=<type>` with `span=3x2` (and `grid=6x8`) or `width`/`height` in pixels, and arguments as `arg.<name>=<value>` (JSON values keep their type, e.g. `arg.days=3`); it answers a PNG or `text/plain`, and a failing widget the notice a page would show, with the reason in `X-Widget-Error`. Neither needs a token.
- **Page editor:** `/ui/editor` (linked from every view) lists every page once: for the selected display type it shows the file that resolves (its variant, else the shared page) with kind, grid, title and parse error and the variants the page has as links to their own file; a row opens its page, and *New page* asks for the name and the display type in a dialog (ADR 0083). `/ui/editor/page?name=<page>&display=<type>` builds one page for a display type (ADR 0068): title, grid (fixed to the character grid on text displays), style, the widget palette from `/api/plugins` filtered by the display's kind – click a widget and then a free cell, or drag it –, the span, the position, the arguments and the style of the selected cell. A placed box is moved with the pointer (it stays where it was grabbed) or by its position and resized by the handles on its right edge, bottom edge and corner; a size or position that would overlap another box is not applied (ADR 0084). A live draft preview shows the result. Nothing is written before *Save*: the editor sends the model with the hash it read, so a file changed on disk answers `409` and asks for a reload; a shared page stays the shared file until *Save as … variant*. A file that does not parse is listed with its error and opens read-only, because the API takes a model, not text. *Delete* and *Rename* sit in the editor's action bar: Delete removes the file the editor shows (and, on a variant's link, the file neither the row nor the display type switch can reach) after naming the plans that use the page (ADR 0073); Rename moves the shared page and every variant as one set and names the plans that keep the old name (ADR 0079); a variant of a display type that is no longer configured opens through a configured type of the same kind and is saved as that type's variant.
- **Plan editor:** the same overview lists the plans next to the pages and creates a new plan in a dialog (ADR 0073): the file is written by the first save and needs at least one rule with a page. `/ui/editor/plan?name=<plan>` edits one (ADR 0069): rules in their order with name, time window (`from`/`to` as times of day, days as checkboxes), pages in their order with `rotate` and `refresh`, the fallback, and the actions the device menu offers, built from the plugins' actions with their arguments and confirmation. Rules that an earlier rule always overrides are marked, pages that the plan's own devices would skip are named, each device links to its `/ui/plan` preview and to its device page, and a save works like the page editor's. *Delete* removes the plan after naming the devices that use it. *Rename* moves the file and names the devices that keep the old name (ADR 0079).
- **Device editor:** the overview also lists the files under `devices/` with display type, MAC, address and plan and creates new ones in a dialog; hardware that registered with an unknown MAC appears below them as unclaimed, with its hardware, firmware, address and first and last contact, *Claim* (asks first, then opens a new device file with the MAC prefilled) and *Forget* (deletes its state file, ADR 0080). `/ui/editor/device?id=<id>` edits one (ADR 0070): display type, MAC address, plan chosen from the plan files, `refresh`, the device menu (`actions`, `menu_show_duration`), the power source, the firmware hold and the battery thresholds, with a link to the device's plan preview and the current file text. Assigning a plan is this single small write, so `paperboy.yaml` is never touched; a save works like the page editor's, and a new device file needs a display type and a MAC address. *Delete* removes the file; the device stops syncing until a file with its MAC exists again (ADR 0073), and its state waits under the MAC. *Rename* moves only the file; the state stays with the MAC, so the page cursor, battery reading and menu survive (ADR 0079, 0080).
- **Display type editor:** the *Displays* tab lists the files under `displays/` with kind, size, `ppi` or `charset`, gray levels and power, and creates new types in a dialog; `/ui/editor/display?name=<name>` edits one (ADR 0071) with the fields of its kind, the physical size computed from `ppi`, the page variants and devices that reference it, and the current file text. The `ppi` field has a helper that converts the panel's diagonal in inches or one side of its active area in millimetres into `ppi` from the pixel size in the form, and says so when the two disagree, because an inch diagonal is usually rounded. Saving works like the other editors. *Rename* moves only the file and names the page variants and devices that keep the old name (ADR 0079); *Delete* removes the file after naming the page variants and devices that reference it (ADR 0073).

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

**Protocol** ([`docs/plugins/protocol.md`](docs/plugins/protocol.md), `pkg/plugin`): one JSON object per line on stdin, one answer per line on stdout with the same `id`. Actions are `configure`, `state`, `describe`, `render` (`levels` 2 or 4, `widgets: [{widget, width, height, args, style}]` → base64 PNGs), `text` (the same in characters → `lines`), `actions` (the state of concrete plan actions) and `action` (run one). Every request carries `language`, `timezone`, `now` and the `page` (`title`, `number`, `count`, display `width`/`height`, and for image displays `ppi`). Protocol version 5 (ADR 0050, 0057, 0081): each widget's `style` holds `padding`, `rule` and `text_px` in pixels, already converted from the page's points; Go plugins use `Request.TextPx`, `PtPx` and `MMPx` for physical sizes. `describe` lists widgets and, optionally, actions, plus the plugin's own `version` (ADR 0075); an action request names a concrete `action` and its `args` and gets `actions` states (title, disabled reason, confirmation) or a `result` (`ok`, `message`, `changed`). Between answers a plugin may print `{"event": "changed", "changed": ["agenda"]}`. Right after `configure` a protocol 5 plugin gets one `state` request with the stored JSON and its declared `blobs` (base64); every answer may send new values back (missing = unchanged, `null` = forget the state), and large values such as an image are declared as `blobs` in `describe` (ADR 0081). Paperboy writes `state/plugins/<id>/state.json` and `state/plugins/<id>/blobs/<name>` itself. A plugin keeps no files itself: `PAPERBOY_STATE_DIR` is gone. It ends when stdin closes. Every plugin also sees `PAPERBOY_LANGUAGE` and `PAPERBOY_TIMEZONE` in its environment. Every answer names the protocol version the plugin was built against (`"version": 5`). Paperboy requires at least 5, accepts newer versions because the protocol only grows additively, and refuses a plugin below that or without a version, with the mismatch as the cell's error and one log line (ADR 0058). Go plugins get this from `pkg/plugin` automatically; plugins in other languages must print the field.

A Go plugin implements `plugin.Handler` (`Describe`, `Configure`, `Render`), optionally `Texter`, `Preparer`, `Starter` (background work with a `notify` callback), `Ager` (`max_age`), `Actioner` (actions for the device menu) and `Stater`/`Blober` (state and files Paperboy keeps between restarts), calls `plugin.Serve` and draws with `pkg/render` – the fonts, gray levels, fills and icon masks Paperboy itself uses, so its widgets look like the rest of the page. Plugins in other languages speak the same JSON lines.

Shipped plugins (one zip per platform: `paperboy-plugins-linux-amd64.zip` and `paperboy-plugins-windows-amd64.zip`; unpacked in the instance directory they create `plugins/` with `<plugin>-v<version>`, each version raised only when that plugin changed, ADR 0075). Each plugin has its own documentation under `docs/plugins/`:

- [`evcc`](docs/plugins/evcc.md) – PV, home, grid, battery, energy flow, loadpoints and forecast from [evcc](https://evcc.io); the first shipped plugin with actions, `evcc/mode` switches a charge mode from the device menu.
- [`departures`](docs/plugins/departures.md) - real-time public transport departures from an EFA installation.
- [`myskoda`](docs/plugins/myskoda.md) - battery, charging plans, parking position and the vehicle image from the [MyŠkoda Public API](https://public.api.connect.skoda-auto.cz/docs).
- [`weather`](docs/plugins/weather.md) – current weather and forecast from [Open-Meteo](https://open-meteo.com/).
- [`calendar`](docs/plugins/calendar.md) – agenda, week and month from CalDAV and iCalendar.
- [`pokedex`](docs/plugins/pokedex.md) – a Pokémon card from [PokéAPI](https://pokeapi.co).
- [`xkcd`](docs/plugins/xkcd.md) – [xkcd](https://xkcd.com) comics.
- [`countdown`](docs/plugins/countdown.md) – days to a date, optionally with a progress bar.
- [`paperboy`](docs/plugins/paperboy.md) – built in, no zip needed: header, text and clock.
- A SolarAI plugin is still to come; it talks to SolarAI over HTTP itself, and SolarAI needs to know nothing about Paperboy.

## Plans

Which pages a device shows is decided by a plan, `<name>.yaml` in `plans/`. A device points to it with `plan: wohnzimmer` in its `devices/<id>.yaml`; several devices may share a plan. A plan only names pages, so it works for devices of different display types. A plan is created and edited in the browser at `/ui/editor` (ADR 0069, 0072): the *Plans* tab creates one, `/ui/editor/plan?name=<plan>` edits it, and the device's page assigns it (ADR 0070). Deleting a plan keeps a copy under `state/editor/`; the confirmation names the devices still using it.

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

### Actions

A plan may offer **actions** that plugins provide (ADR 0057): things a user starts from the device menu, such as switching the evcc charge mode. Actions belong to the plan, not to a page, so every rule and page of a plan offers the same list.

```yaml
actions:
  - action: evcc/mode
    args: { loadpoint: 0, mode: now }
    id: charge-now
    title: "Charge now"          # optional; otherwise the plugin's title
  - action: evcc/mode
    args: { loadpoint: 1, mode: now }
    id: charge-now-2             # required: evcc/mode appears twice
  - paperboy/refresh             # short form for actions without args
```

- `action` is `plugin/action`; `args` are the action's arguments from the plugin's `describe`.
- `id` is optional and plan-local. It becomes mandatory for **every** entry of an action as soon as the same `action` appears more than once, and IDs must be unique. The menu keeps its selection by `id`, or by `plugin/action` when there is no `id`, so reordering the list or changing `args`/`title` does not change the identity.
- Exact duplicates of `action` and `args` are rejected. An unknown plugin, action or argument disables the entry with a reason instead of breaking the plan. A plugin that reports another protocol version is refused as a whole (ADR 0058), not used without its actions.
- A page-level or rule-level `actions` key is rejected with a hint.

#### The device menu

Short `home` always opens the menu — also when the plan offers no actions, then it shows a "Keine Aktionen" ("No actions") banner instead of the action grid. A device with `actions: deny`, a text display, a critical battery and a missing plan have no menu. Paperboy draws it as an overlay over the top half of the current page — one row of `X`, up to three actions and `<`/`>`, stacked in the same order on portrait displays. The page below stays visible, and the menu pauses rotation and `page_timeout`.

- `next`/`previous` move the selection and wrap; `<`/`>` page through the list in steps of three.
- `home` runs the selected action. If the plugin asks for a confirmation (`confirm` in its `describe`), Paperboy shows a confirmation screen first and runs the action only after the device reports that screen as painted (`shown` hash): the first `home` opens the menu or the confirmation, never runs an action blind.
- A menu without a runnable action, and `home` on a disabled one, show the plugin's reason as a banner.
- A long `home` closes the menu and keeps the page. On a device with `touch: true` the menu draws a control strip under the actions: `X` closes it, `<`/`>` page the list, and `next` past the last action selects the `X` — on a button-only device the strip is not drawn at all (ADR 0082). A tap on a confirming action opens its confirmation first; a tap never opens the menu.
- With the menu closed, a long `next` or `previous` goes to the first page of the active rule. Long presses inside the menu are ignored, so the direction buttons only move the selection there.
- Unavailable actions stay visible, hatched, with the plugin's reason. A plugin that is down keeps its actions visible as unavailable instead of hiding them.
- The menu closes itself after `menu_show_duration` (default 30 s, at most 60 s) and asks for a one-shot wake at that deadline (`wake` in the sync answer, ADR 0064), so it closes without waiting for the next regular refresh. The empty banner closes after ten seconds at the latest; any input dismisses it.
- An action gets about five seconds inside the sync. If it is not done by then a thin banner at the top says "running", and Paperboy schedules one result wake (`reason: result`) for the outcome. The outcome itself is a banner too ("Erledigt: …" or "Fehlgeschlagen: …") without cells or a close box; the next input or the next regular sync clears it.
- A device with `actions: deny` keeps the old meaning of `home` (page 1); the dashboard marks it as denied.
- Plugin actions set target states (e.g. `mode: now`), never toggles, so a repeated sync is safe.

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

The dashboard header shows version, commit and commit date. The commit and its date come from Go's VCS stamping, which works as long as the Git repository is present; the container image therefore keeps `.git` in the build context and installs the git binary in its build stage (the alpine Go image ships none).

The version name cannot come from VCS stamping: Go stamps any working-tree build with a pseudo-version derived from the commit (`v0.0.0-…`), even when the exact commit is tagged. Release builds therefore pass the tag in, `-ldflags "-X github.com/maehld/paperboy/internal/paperboy.version=v2.0.0"`, which `release.yml`, the container image (`--build-arg VERSION=…`) and `scripts/build.ps1 -Version` do. A build without it reports `dev` plus the short commit. `go version -m <binary>` shows what was stamped.

The workflows follow the three deliverables (ADR 0056): *CI* runs formatting, vet, tests and a cross-build on every change, *Plugins* and *Firmware* build their artifacts (the firmware only when `firmware/esphome/` changes), *Docker* publishes the image and *Release* collects everything into one GitHub release on a tag.

A build without a repository or with `-buildvcs=false` reports `dev`. `go version -m <binary>` shows what was stamped. Untagged builds show `dev` plus commit; `git tag v0.1.0` gives release builds a version name.

```text
cmd/paperboy/            Startup, options and example configuration
cmd/weather/             Weather plugin: Open-Meteo (ADR 0049)
cmd/calendar/            Calendar plugin: CalDAV and iCalendar (ADR 0048, 0049)
cmd/evcc/                evcc plugin: energy flow, charging point, forecast
cmd/xkcd/                xkcd plugin: comic with optional title and alt text
cmd/countdown/           Countdown plugin: days to a date with a progress bar
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

## Licence

Paperboy is published under the [MIT licence](LICENSE). Third-party material keeps its own licence:

- The OpenMoji pictograms in `cmd/weather/icons/` and `cmd/evcc/icons/` are [CC BY-SA 4.0](cmd/weather/icons/README.md); the licence travels with the plugin zip.
- The firmware in `firmware/esphome/` is a Paperboy configuration for [ESPHome](https://esphome.io). The images built from it contain ESPHome's runtime, whose C++ parts are GPLv3; the sources are in this repository. The bundled Go fonts (`pkg/render`) come with their own BSD-style licence.
- The examples and the plugin test fixtures are fictional; xkcd comics are CC BY-NC 2.5 and are fetched at runtime, never shipped.
