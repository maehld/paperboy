<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/logo/paperboy-lockup-inverted.svg">
    <img src="docs/logo/paperboy-lockup.svg" alt="Paperboy" width="320">
  </picture>
</p>

# Paperboy

Paperboy is a display service for ePaper displays and text displays. It runs on your computer, composes pages and sends them to the displays in your network.

A page is a grid of cells, and each cell shows a widget. Every widget comes from a plugin: a long-running program that draws the widget in the exact size of its cell (ADR 0049). Paperboy composes the page, selects it for each device with a plan and delivers it over the device API (ADR 0042). The devices run the Paperboy firmware on ESPHome (ADR 0041) with deep sleep, so a battery device wakes up only for its next sync.

```text
Plugin (calendar, weather, …) ◀──JSON lines on stdin/stdout──▶ paperboy ◀──POST /api/device/sync── Display (Paperboy firmware)
```

## Quick start

1. Create the instance directory with the fixed layout (ADR 0037): `paperboy.yaml`, `displays/`, `pages/`, `plans/`, `devices/` and, for your own plugins, `plugins/`.
2. Configure `paperboy.yaml`: copy `cmd/paperboy/paperboy.example.yaml` to `<instance>/paperboy.yaml` and adapt the `plugins` section.
3. Add the display types: copy the files from `examples/displays`, or create them in the editor at `/ui/editor?tab=displays` (ADR 0071).
4. Add pages and a plan: copy files from `examples/pages` and `examples/plans` into the instance.
5. Add one device file under `devices/` for every display (ADR 0070). Copy `examples/devices/wohnzimmer.yaml`, or create the device in the editor. Set `plan: standard` in the device file, or assign the plan in the editor.
6. Put the plugins into `plugins/`: unpack `paperboy-plugins-<os>-<arch>.zip` in the instance directory. The zip is attached to each release; otherwise it is an artifact of the *Plugins* workflow. You can also run `./scripts/package-plugins.sh` to build the zip, or let Paperboy fetch the programs with a `download` section (see [Downloads](#downloads)). The zip creates versioned programs such as `plugins/weather-v1.4.0` (ADR 0075).
7. Start the server:
   ```sh
   go run ./cmd/paperboy -config . -listen :8090
   # show a page without a device:
   curl -o page.png http://127.0.0.1:8090/api/preview/e1001/uebersicht
   ```
   You can also run Paperboy in a container (`Dockerfile`, `compose.example.yaml`). The image contains Paperboy only. Use the `linux/amd64` plugin zip in the mounted instance directory.
8. Flash the Paperboy firmware (ADR 0041, see [firmware/esphome/README.md](firmware/esphome/README.md)). The release images are in `paperboy-firmware.zip`, or Paperboy fetches them for you (see [Downloads](#downloads)). Enter `http://<computer>:8090` as the Paperboy URL in the web UI of the device. The device registers with its MAC address. Paperboy rejects and logs an unknown MAC address.

The MAC address in a device file is the allow list. Paperboy shows an unknown device as unclaimed hardware in the devices tab. *Claim* asks first, then opens a new device file with the MAC prefilled (ADR 0080). Without a plan, a device shows a notice page.

## Releases

GitHub releases are the distribution point (ADR 0056). Each tag names a tested combination of the three deliverables:

| Tag | Contains |
|---|---|
| `vX.Y.Z` | Paperboy binaries, plugin zips and the firmware bundle |
| `plugins-vX.Y.Z` | plugin zips only |
| `firmware-vX.Y.Z` | firmware bundle only (`X.Y.Z` is the highest `fw_version` in the bundle) |

The tag names the release, not what is inside. Every plugin and every firmware image carries its own version, and that version changes only with a change (ADR 0043, 0075).

| Asset | Use |
|---|---|
| `paperboy-linux-amd64`, `paperboy-linux-arm64`, `paperboy-windows-amd64.exe` | the Paperboy binary for that platform |
| `paperboy-plugins-<os>-<arch>.zip` | unpack in the instance directory: creates `plugins/` with `<plugin>-v<version>` of every shipped plugin |
| `paperboy-firmware.zip` | unpack in the instance directory: creates `firmware/` with `<hardware>-v<version>.bin` of every hardware type (ADR 0043, 0059, 0060, 0063) |
| `SHA256SUMS` | checksums of all assets of the release |

The container image is published separately as `ghcr.io/maehld/paperboy` (`X.Y.Z`, `X.Y` and `latest`). To create a release, push a tag: `git tag v2.1.0 && git push origin v2.1.0`. The *Release* workflow builds all assets and attaches them to one release. A tag with a suffix (`v1.4.0-rc.1`, `plugins-v1.4.0-rc.1`) becomes a prerelease; only the `latest` channel installs it (ADR 0089, see [Downloads](#downloads)).

## Downloads

A `download` section makes Paperboy fetch plugin programs and firmware images from the releases (ADR 0060). Paperboy verifies every file against the `SHA256SUMS` of the release.

```yaml
download:
  source: https://github.com/maehld/paperboy  # optional, this is the default
  token: ${GITHUB_TOKEN}                     # optional: rate limits, private forks
  interval: 24h                              # optional: between checks, at least 1h
  plugin_channel: stable                     # optional: stable (default) or latest (ADR 0089)
  firmware_channel: stable                   # optional: stable or latest, per train
```

A bare `download:` key is enough. It uses the official source and checks every 24 hours. Without the section, the checks stay off.

- A check runs at startup and then after `interval` (default 24 h) in the background. The *Downloads* page (`/ui/downloads`) has a **Check now** button that starts a check at once. An instance without internet keeps working.
- **Plugins**: a configured plugin without a program gets the newest one as `plugins/<plugin>-v<version>` (`weather-v1.4.0`, `weather-v1.4.0.exe` on Windows). Paperboy updates a downloaded program when the plugin zip has a higher version of that plugin. An unchanged plugin stays as it is, however many releases pass (ADR 0075). The old file stays, and only that plugin restarts. A `version` pin names a plugin version (`v1.4.0`) and never moves.
- **Firmware**: every image of the bundle lands in `firmware/<hardware>-v<version>.bin` (ADR 0061). A `firmware.version` pin selects the release with exactly that image; this is also how you pin a rollback. Paperboy offers an update only with `firmware.update: auto`.
- **Channels** (ADR 0089): `stable` (default) installs only releases that GitHub does not mark as a prerelease. It also refuses a version with a suffix inside an unflagged release. `latest` installs prereleases too. A version may carry a suffix (`weather-v1.4.0-rc.1`, `e1001-v1.4.0-rc.1.bin`), so an rc and the final are two files, and a pin can name either. A channel never downgrades: after a switch from `latest` to `stable`, the newer program stays; use a pin for a rollback. A pin bypasses the channel and can select an rc from a prerelease.
- A program **without** a version in `plugins/` (`weather`) always wins. It is your build. Paperboy neither replaces nor pins it. Download such a plugin by hand, or delete the program to give the plugin back to the releases.
- Programs and images are never overwritten, so they pile up. The *Downloads* page shows the size of both inventories and a **Preview cleanup**. The preview lists what a cleanup would delete. It keeps the version in use and every version a pin names. It also keeps the firmware a device reports or is offered, files without a version, and the newest two versions of every plugin and hardware type (ADR 0090). If you delete a newer plugin file, Paperboy rolls back to the older one for the rest of the run. A `version` pin keeps the older one across restarts, and a firmware pin keeps the older image on offer.
- Paperboy cleans the download cache under `state/download/`. It keeps one archive per train and asset, and drops the previous one after the next is verified and installed. The learned entry lists stay, so a pin costs no download.
- The *Downloads* page shows the source, the interval, the last check and the running check. It shows the program and version behind every plugin, the images in `firmware/` and the firmware each device reports. It also shows the channel of each train and notes when a running version is not one the channel would deliver. The dashboard warns only when the last check failed.

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
    devices/         state of every MAC: <MAC>.json with API key, page cursor, battery and the address of the last contact (ADR 0080, 0083)
    plugins/         plugin state <id>/state.json and the declared blobs <id>/blobs/<name> (ADR 0081)
    download/        cache per train: plugins/ and firmware/ with cache.json and one archive each (ADR 0060, 0090)
    editor/          backups of pages, plans, devices and display types written or deleted by the editor (ADR 0066, 0070, 0071, 0073)
```

- `paperboy.yaml`, `displays/`, `pages/`, `plans/`, `devices/`, `plugins/` and `firmware/` are yours. Paperboy only reads them. Only the download feature (ADR 0060) adds plugin and firmware files there. The editor writes display types, pages, plans and devices through the API (ADR 0066, 0070, 0071).
- `state/` is created at startup and is the only directory Paperboy writes. Back up `state/devices/`: it holds the API keys, page cursors, battery levels, the address of the last contact and last served pages. Without it, the devices get new keys on their next sync (401, then registration by MAC). A state file whose MAC no device file uses is unclaimed hardware. The devices tab of the editor shows it with *Claim* (asks first) and *Forget*. The state follows the MAC, so renaming or deleting a device file does not touch it.

## Configuration

`paperboy.yaml` is the only configuration format (ADR 0026) and lives in the instance directory. Paperboy parses the file strictly: an unknown key, a duplicate MAC address and an unsupported protocol reject the configuration. A plan that is missing or broken does not: Paperboy logs it and the device gets a notice page, because plan files may be added while the server runs (ADR 0047).

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
  plugin_channel: stable    # optional: stable (default) or latest (ADR 0089)
  firmware_channel: stable  # optional: stable or latest
# Display types are files under displays/ (ADR 0071); devices are files under devices/ (ADR 0070).
```

- `plugins` declares the plugins (ADR 0049). `config` is the plugin's own section. String values expand `${VAR}` from the environment of Paperboy: an unset variable rejects the configuration, `$$` is a literal `$`, so secrets stay out of the file. The plugin checks its keys and rejects unknown ones. A rejected configuration appears as a notice in the cells of that plugin and in the log. The name `paperboy` is reserved for the built-in plugin.
- `language` (`de` or `en`) selects the language of the built-in image texts, the number format and the language plugins draw in. Without it, Paperboy uses the system language (Windows UI language, otherwise `LANG`/`LC_ALL`/`LC_MESSAGES`) and falls back to English. `timezone` (IANA name) applies to plans and built-in dates; without it, Paperboy uses the system timezone.
- `-config` names the instance directory (default `.`). Paperboy reads `<dir>/paperboy.yaml` and finds all other directories there. Paperboy rejects a file argument with an explanation and does not read a `.json` configuration.
- Paperboy reloads the configuration on change without a restart (ADR 0047). Handlers pick up a modified `paperboy.yaml` or a changed file under `displays/` or `devices/` on the next request. Plugins whose section changed are restarted, and their cached widgets are dropped. A broken file keeps the last valid configuration. If `paperboy.yaml` is not usable at startup in an existing instance directory, the server still starts in degraded mode. It serves no display types and no devices, and applies the file as soon as it loads, also when its mod time did not change. A wrong `-config` stays fatal.

## Display types

A display type is `displays/<name>.yaml` (ADR 0071). The file name is the name that pages and devices use: it is the suffix of `pages/<page>.<name>.yaml` and the `display_type:` of a device. Paperboy parses the file as strictly as pages and plans. A broken file rejects the configuration and names the file.

```yaml
kind: image
size: [800, 480]
gray_levels: 4     # 1 or 4
ppi: 125           # pixels per inch, required for image displays (800 px / 163 mm)
min_refresh: 60s   # never report a shorter interval
power: auto        # auto (default), battery or mains (ADR 0051)
```

- A display type with `kind: text` is a text display (ADR 0044). `size: [columns, rows]` is in characters and at most 80 × 24. `charset: utf8|ascii|hd44780` defaults to `utf8`. Paperboy rejects `gray_levels` and `ppi` for a text display.
- An image display type needs `ppi`, its pixel density (pixels per inch, 20–1000; ADR 0050). The density does not depend on the orientation: compute it from either side, `ppi = pixels × 25.4 / millimetres` of the active area. Page styles are in points and are converted with it, so a widget has the same physical size on a small dense panel and a large coarse one.
- Create, edit and rename display types at `/ui/editor?tab=displays` (ADR 0071, 0079). The files stay the source of truth, so Paperboy picks up a hand edit on the next request. A rename moves `displays/<name>.yaml` and nothing else. The confirmation names the page variants and devices that keep the old name. Those stay editable. A device whose display type is missing answers with 503 until its file is fixed. An orphaned page variant opens through a configured type of the same kind and can be saved as that type's variant. Delete works in the editor (ADR 0073). The confirmation names the page variants and devices that keep pointing at the missing type, and a copy stays under `state/editor/`.
- Paperboy needs at least one display type. Without one it rejects the configuration (see [Configuration](#configuration)). Several devices can share a display type. No device has to use a type. A page variant of a type that is not configured is listed with its error instead of disappearing.

## Devices

A device is `devices/<id>.yaml` (ADR 0070). The file name is the stable device ID (a-z, 0-9, `-`). It appears in logs and in `GET /api/devices`, while its state lives under its MAC in `state/devices/` (ADR 0080). Paperboy parses the file as strictly as pages and plans. A broken file rejects the configuration and names the file.

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
touch: false            # optional: draw the menu's tap controls (ADR 0082)
```

- `protocol` accepts only `paperboy` (the default): devices running the Paperboy firmware (ADR 0042). Paperboy rejects `trmnl` (removed), `opendisplay` (not supported yet) and `text-http`/`image-http` (the device API replaced them, ADR 0044).
- `plan` should name an existing `plans/<name>.yaml`. Paperboy logs a missing or broken plan and shows a notice page instead of rejecting the configuration, so you can add a plan file while the server runs (ADR 0047). Without `plan`, the device shows a notice page in the configured language (*No plan configured* / *Kein Plan konfiguriert*).
- Create and assign devices in the editor at `/ui/editor` (ADR 0070). The files stay the source of truth, so Paperboy picks up a hand edit on the next request.
- Paperboy keeps the source address of the last register, sync or text poll in the state file of the device (`client_ip`, ADR 0083). It shows the address on the dashboard, in the devices tab and in `GET /api/devices`. The address comes from the connection, never from a header, so a reverse proxy in front of Paperboy would show its own address.
- `touch: true` makes the device menu draw its tap controls. A device without touch keeps the button-only menu (ADR 0082). If the key is absent, Paperboy uses what the firmware reported.

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

- `widget` is `<plugin>/<widget>`. Paperboy passes `args` to the widget unchanged. `at` is the top-left cell (column, row, from 0), and `span` is the size in cells (default `[1, 1]`). Cells must stay inside the grid and must not overlap. Empty cells stay white.
- On an image display the grid divides the pixels. Paperboy asks every plugin for its widgets in exactly the pixel size of the cell and the gray levels of the panel. It quantizes the PNG to the palette (nearest gray, no dithering) and pastes it 1:1, so Paperboy never scales a widget. The PNG has 1 bit or 2 bits (4 gray levels), depending on the display type. A plugin that answers with another size gets a notice in its cell.
- `style` is in points (1 pt = 1/72 inch, ADR 0050). Paperboy passes it to every widget and converts it to pixels with the `ppi` of the display type. `padding` is the inner margin (default 4.5 pt ≈ 1.6 mm). `rule` is the separator width (default 0.6 pt ≈ 0.2 mm, never thinner than 1 px). `text_size` is the base text height (4–144 pt, default 16 pt, at most 0.12 of the display height), so widgets of different plugins match. `divider` (points, at least 1 px, default 0 = none) draws lines between the cells; Paperboy draws them, not the widgets. The `style` of a cell overrides the page style.
- The **built-in plugin** `paperboy` needs no configuration and provides `paperboy/header`, `paperboy/text` and `paperboy/clock` (see [docs/plugins/paperboy.md](docs/plugins/paperboy.md)).
- `kind: image` (default) or `kind: text` says which displays a page is for. A plan may serve image and text displays alike. When a device pages through a rule, Paperboy skips pages that do not fit its display type. A page does not fit when it has no variant and no shared page or when it has the other kind. It also does not fit when its text grid is not the character grid of the display. Paperboy logs each skip once per display type and page. When no page of the rule fits, the device gets a notice (*No matching page*). Paperboy does not skip a broken page; it shows its error as a notice.
- **Stale widgets**: when a plugin fails, Paperboy keeps showing its last widgets and marks the page. An image display gets a bar (*Outdated: calendar/agenda*, German *Veraltet: …*), a text display gets `!` in the last column of the first line. `notices: none` turns both off. A widget that never rendered becomes a hatched notice inside its cell with the reason.
- Preview without a device: `GET /api/preview/<display type>/<page>` (PNG, or `text/plain` for text displays; an optional `?at=YYYY-MM-DDTHH:MM` renders the page as at that time in the configured timezone). `GET /api/profiles` lists the display types.
- The page editor at `/ui/editor` builds a page from the same model (see [Web UI](#web-ui)).

### Text pages

A page for a text display (display type `kind: text`, ADR 0044) says `kind: text`. Its grid is the character grid and must match the display size, and cells are measured in characters:

```yaml
kind: text
grid: [20, 4]
cells:
  - { at: [0, 0], span: [20, 1], widget: paperboy/header }
  - { at: [0, 1], span: [20, 1], widget: weather/current }
  - { at: [0, 2], span: [20, 2], widget: calendar/agenda }
```

Plugins answer with lines for the cell. Paperboy converts them to the `charset` of the display type, cuts them to the cell width and pads them. `ascii` transliterates (ä → ae, ° → o, € → EUR). `hd44780` keeps `ä ö ü ß ° µ` for the ROM mapping of the firmware, writes `Ä Ö Ü` as `A O U` (the ROM has no capital umlauts, and the width must stay) and transliterates the rest. Set `charset: hd44780` for HD44780 displays; with the default `utf8` the firmware has to guess. A widget without a text variant shows its error in the cell.

The pages in `examples/pages` (`uebersicht`, `kalender`, `wetter`, `laden` and the 20 × 4 variants `uebersicht.lcd2004` and `laden.lcd2004`) use the shipped plugins. `examples/plans/standard.yaml` pages through them.

## Plans

Which pages a device shows is decided by a plan, `<name>.yaml` in `plans/`. A device points to it with `plan: wohnzimmer` in its `devices/<id>.yaml`, and several devices may share a plan. A plan only names pages, so it works for devices of different display types. Create and edit a plan in the browser at `/ui/editor` (ADR 0069, 0072): the *Plans* tab creates one, `/ui/editor/plan?name=<plan>` edits it, and the device page assigns it (ADR 0070). When you delete a plan, a copy stays under `state/editor/`; the confirmation names the devices that still use it.

```yaml
rules:
  - name: morgen
    when: { from: "07:00", to: "09:00", days: [mo, di, mi, do, fr] }
    pages: [kalender, uebersicht]
  - name: tag
    when: { from: "09:00", to: "19:00" }
    pages: [uebersicht, wetter, energie]
    rotate: true
    refresh: 5m
  - name: nacht
    when: { from: "19:00", to: "07:00" }
    pages: [uebersicht]
    refresh: 30m
fallback: nacht
```

- The first matching rule wins. A `when` without `from`/`to` always matches. `from` is inclusive, `to` exclusive. `to < from` wraps over midnight and then belongs to the day the window starts. Days accept German and English short names.
- Times use the global `timezone` from the configuration (ADR 0033). Plans have no timezone of their own, and Paperboy rejects a `timezone` key.
- `pages` is the paging order within the rule. `rotate: true` moves on at every timed refresh; otherwise only a button changes the page. Buttons page within the active rule only, a long press returns to its first page, and after `page_timeout` the device shows the first page again unless the rule rotates.
- `refresh` replaces the device interval. Paperboy caps the reported interval to the next rule boundary (but never below the floor of the power source of the device), so a sleeping device wakes up for a rule change.
- `fallback` names the rule used when nothing matches. Without a match and without a fallback, the device gets a notice page.
- A rule needs a name (a-z, 0-9, `-`) and at least one page. Rule names must be unique, and a rule must not name the same page twice.
- Paperboy reloads plans on change without a restart; a broken file keeps the last valid version. A missing or broken page also leads to a notice page, so devices never see a server error.
- Plans serve devices on the Paperboy firmware. Paperboy rejects `when.data`, because data conditions are not implemented yet.

### Actions

A plan may offer **actions** that plugins provide (ADR 0057): things a user starts from the device menu, such as switching the evcc charge mode. Actions belong to the plan, not to a page, so every rule and page of a plan offers the same list.

```yaml
actions:
  - action: evcc/mode
    args: { loadpoint: 0, mode: now }
    id: charge-now
    title: "Charge now"          # optional; otherwise the title of the plugin
  - action: evcc/mode
    args: { loadpoint: 1, mode: now }
    id: charge-now-2             # required: evcc/mode appears twice
```

- `action` is `plugin/action`. `args` are the arguments of the action from the `describe` answer of the plugin. An action without arguments can be a single string instead of a map.
- `id` is optional and plan-local. It becomes mandatory for **every** entry of an action as soon as the same `action` appears more than once, and IDs must be unique. The menu keeps its selection by `id`, or by `plugin/action` when there is no `id`, so reordering the list or changing `args`/`title` does not change the identity.
- Paperboy rejects exact duplicates of `action` and `args`. An unknown plugin, action or argument disables the entry with a reason instead of breaking the plan. Paperboy refuses a plugin that reports another protocol version as a whole (ADR 0058); it does not use the plugin without its actions.
- Paperboy rejects an `actions` key on a rule or a page with a hint.

#### The device menu

A short `home` always opens the menu. It also opens when the plan offers no actions; then it shows a *No actions* (*Keine Aktionen*) banner instead of the action grid. A device with `actions: deny` has no menu. A text display, a device with a critical battery and a device without a plan have no menu either. Paperboy draws the menu as an overlay over the top half of the current page. It has one row with `X`, up to three actions and `<`/`>`, stacked in the same order on portrait displays. The page below stays visible, and the menu pauses rotation and `page_timeout`.

- `next`/`previous` move the selection and wrap. `<`/`>` page through the list in steps of three.
- `home` runs the selected action. If the plugin asks for a confirmation (`confirm` in its `describe`), Paperboy shows a confirmation screen first. It runs the action only after the device reports that screen as painted (`shown` hash): the first `home` opens the menu or the confirmation, it never runs an action blind.
- A menu without a runnable action, and `home` on a disabled action, show the reason of the plugin as a banner.
- A long `home` closes the menu and keeps the page. On a device with `touch: true` the menu draws a control strip under the actions: `X` closes it, `<`/`>` page the list, and `next` past the last action selects the `X`. On a button-only device the strip is not drawn at all (ADR 0082). A tap on a confirming action opens its confirmation first; a tap never opens the menu.
- With the menu closed, a long `next` or `previous` goes to the first page of the active rule. Paperboy ignores long presses inside the menu, so the direction buttons only move the selection there.
- Unavailable actions stay visible, hatched, with the reason of the plugin. A plugin that is down keeps its actions visible as unavailable instead of hiding them.
- The menu closes itself after `menu_show_duration` (default 30 s, at most 60 s). It asks for a one-shot wake at that deadline (`wake` in the sync answer, ADR 0064), so it closes without waiting for the next regular refresh. The empty banner closes after ten seconds at the latest; any input dismisses it.
- An action gets about five seconds inside the sync. If it is not done by then, a thin banner at the top says *Running …* (*Wird ausgeführt …*), and Paperboy schedules one result wake (`reason: result`) for the outcome. The outcome is a banner too (*Done: …* or *Failed: …*, German *Erledigt: …* or *Fehlgeschlagen: …*) without cells or a close box. The next input or the next regular sync clears it.
- With `actions: deny`, `home` goes to page 1, and the dashboard marks the device as denied.
- Plugin actions set target states (e.g. `mode: now`), never toggles, so a repeated sync is safe.

## Plugins

A plugin is a long-running program that provides widgets (ADR 0049). Paperboy starts it on first use and sends its `config` section once (`configure`). Then it asks for widgets. Paperboy keeps the results until the `max_age` of the plugin (default 5 minutes) or until the plugin reports a change. It renders widgets that are still in use again in the background, so a device never waits for a plugin. When a plugin exits or does not answer within `timeout`, Paperboy restarts it. Until then, the last widgets appear as stale. A failing plugin is not asked again for a minute.

**Protocol** ([`docs/plugins/protocol.md`](docs/plugins/protocol.md), `pkg/plugin`): one JSON object per line on stdin, one answer per line on stdout with the same `id`. Actions are `configure`, `state`, `describe`, `render` (`levels` 2 or 4, `widgets: [{widget, width, height, args, style}]` → base64 PNGs), `text` (the same in characters → `lines`), `actions` (the state of concrete plan actions) and `action` (run one). Every request carries `language`, `timezone`, `now` and the `page` (`title`, `number`, `count`, display `width`/`height`, and for image displays `ppi`).

**Protocol version 5** (ADR 0050, 0057, 0081): each widget's `style` holds `padding`, `rule` and `text_px` in pixels, already converted from the points of the page. Go plugins use `Request.TextPx`, `PtPx` and `MMPx` for physical sizes. `describe` lists widgets and, optionally, actions, plus the plugin's own `version` (ADR 0075). An action request names a concrete `action` and its `args` and gets `actions` states (title, disabled reason, confirmation) or a `result` (`ok`, `message`, `changed`). Between answers a plugin may print `{"event": "changed", "changed": ["agenda"]}`.

Right after `configure`, a protocol 5 plugin gets one `state` request with the stored JSON and its declared `blobs` (base64). Every answer may send new values back: missing = unchanged, `null` = forget the state. A plugin declares large values such as an image as `blobs` in `describe` (ADR 0081). Paperboy writes `state/plugins/<id>/state.json` and `state/plugins/<id>/blobs/<name>` itself. A plugin keeps no files itself; Paperboy sets no `PAPERBOY_STATE_DIR`. It ends when stdin closes. Every plugin also sees `PAPERBOY_LANGUAGE` and `PAPERBOY_TIMEZONE` in its environment.

Every answer names the protocol version the plugin was built against (`"version": 5`). Paperboy requires at least 5 and accepts newer versions, because the protocol only grows additively. It refuses a plugin below that or without a version. A mismatch appears as the error of the cell and as one log line (ADR 0058).

A Go plugin implements `plugin.Handler` (`Describe`, `Configure`, `Render`). It can also implement `Texter`, `Preparer`, `Starter` (background work with a `notify` callback), `Ager` (`max_age`), `Actioner` (actions for the device menu) and `Stater`/`Blober` (state and files Paperboy keeps between restarts). It calls `plugin.Serve` and draws with `pkg/render`: the fonts, gray levels, fills and icon masks Paperboy itself uses, so its widgets look like the rest of the page. Plugins in other languages speak the same JSON lines.

Paperboy ships the plugins below. One zip per platform (`paperboy-plugins-linux-amd64.zip`, `paperboy-plugins-windows-amd64.zip`) contains all of them. Unpacked in the instance directory, the zip creates `plugins/` with `<plugin>-v<version>`. Each plugin raises its version only when it changes (ADR 0075); the version is in `cmd/<plugin>/VERSION`. Every plugin has its own documentation under `docs/plugins/`:

- [`evcc`](docs/plugins/evcc.md) – PV, home, grid, battery, energy flow, loadpoints and forecast from [evcc](https://evcc.io); the first shipped plugin with actions, `evcc/mode` switches a charge mode from the device menu.
- [`departures`](docs/plugins/departures.md) – real-time public transport departures from an EFA installation.
- [`myskoda`](docs/plugins/myskoda.md) – battery, charging plans, parking position and the vehicle image from the [MyŠkoda Public API](https://public.api.connect.skoda-auto.cz/docs).
- [`weather`](docs/plugins/weather.md) – current weather and forecast from [Open-Meteo](https://open-meteo.com/).
- [`calendar`](docs/plugins/calendar.md) – agenda, week and month from CalDAV and iCalendar.
- [`pokedex`](docs/plugins/pokedex.md) – a Pokémon card from [PokéAPI](https://pokeapi.co).
- [`xkcd`](docs/plugins/xkcd.md) – [xkcd](https://xkcd.com) comics.
- [`countdown`](docs/plugins/countdown.md) – days to a date, optionally with a progress bar.
- [`rss`](docs/plugins/rss.md) – RSS and Atom feeds as a list or as a single headline.
- [`paperboy`](docs/plugins/paperboy.md) – built in, no zip needed: header, text and clock.

## Web UI

Paperboy serves its operator pages on the same address as the device API. They need no token (see [Security](#security)).

### Dashboard

`GET /` redirects to `/ui`, a read-only overview of all devices (ADR 0040). Each card shows the status, the plan and its active rule. It also shows a preview of the last served page (the lines for text displays), refresh, last contact, battery and the last render or plan error. The page is a server-rendered Go template without JavaScript, refreshes itself every 30 seconds and needs no token. Rendered PNGs live in memory only, so after a restart a card omits `last_page_url` and uses the page preview or a placeholder. The header shows the running build: version (or `dev`), short commit with `+dirty` for local changes, and commit date from the VCS stamp; the Go toolchain is in the tooltip. Every page links the editor.

### Plan preview

The *preview* link on a device card opens `/ui/plan?device=<id>` (ADR 0054), a read-only view of the plan of the device. It shows every rule with its time window, `rotate` and `refresh`, the fallback, the rule that applies now (or at the time picked with `&at=YYYY-MM-DDTHH:MM`) and the next rule change. Each page is previewed for the display type of the device; pages the device would skip show the reason, broken pages their error. The view does not change the page cursor or the state of the device.

### Virtual display

The *virtual* link on a device card and in the plan view opens `/ui/virtual?plan=<plan>&display=<type>` (ADR 0091). The page acts as a display of that type: it syncs like the firmware, shows the page the plan picks, pages with the buttons of the plan and opens the device menu. Optional URL parameters replace what a device file would decide: `refresh`, `menu` (5–60 s), `page_timeout` (all seconds) and `power=mains|battery`. The URL is the only configuration, so a link is a display. The cursor and the menu live in memory and not in `state/devices/`, so no device is touched. *Reset* starts again at the first page.

Paperboy resolves, selects and confirms actions like on a device, but never runs them: the result banner says *Simulated* and names the action. `←`/`→`/Enter and press-and-hold work, *Full screen* hides the chrome, and a debug line below the display names the active rule, the page, the refresh, the wake and the last error.

### Widgets page

`/ui/plugins` (linked from the dashboard) opens the page editor without a page. Pick a widget in the palette, or open the page without a widget and it starts with the built-in header. Paperboy draws the widget in its cell size on the chosen display type. The inspector shows its size, arguments and style. The snippet panel shows the finished cell, ready to copy into a page. Sizes are spans on a reference grid of 6 × 8 cells (`?grid=` changes it), and plugins declare their sizes as spans of that grid.

`GET /api/plugins` lists every plugin with its widgets, the sizes they are designed for, their arguments and the config keys of the plugin. A single widget is available as `GET /api/widget/<plugin>/<widget>?display=<type>` with `span=3x2` (and `grid=6x8`) or `width`/`height` in pixels, and arguments as `arg.<name>=<value>` (JSON values keep their type, e.g. `arg.days=3`). It answers a PNG or `text/plain`, and for a failing widget the notice a page would show, with the reason in `X-Widget-Error`.

### Editors

`/ui/editor` is one overview with a sub-tab per object type (`?tab=pages|plans|devices|displays`, default pages). Each tab has one card with its count, a filter field and its new-object button in the header. The button asks for the name in a dialog (a page also chooses the display type) and opens the editor of the new object. A row opens its object: the name is a link, and a click on the row follows it. Rename and Delete live in the editor and name the references (ADR 0073, 0079, 0084). Only the Delete of a page variant that neither the row nor the display type switch reaches stays in the list. Every editor shows the same action bar `← <list>` · object · status · Rename · Delete · Reload · Save (ADR 0076). A save sends the model with the hash it read, so a file changed on disk answers `409` and asks for a reload. Paperboy keeps the last ten old versions under `state/editor/`.

#### Page editor

The pages tab lists every page once. For the selected display type it shows the file that resolves (its variant, else the shared page) with kind, grid, title and parse error, and the variants of the page as links. `/ui/editor/page?name=<page>&display=<type>` builds one page for a display type (ADR 0068). It edits the title, the grid and the style. On text displays the grid is fixed to the character grid. The palette from `/api/plugins` is filtered by the kind of the display. For the selected cell the editor sets the span, the position, the arguments and the style.

Click a widget and then a free cell, or drag it from the palette. A placed box is moved with the pointer (it stays where it was grabbed) or by its position. Resize it by the handles on its right edge, bottom edge and corner; a size or position that would overlap another box is not applied (ADR 0085). A live draft preview shows the result.

The board is the rendered draft with the cell boxes on top (ADR 0074): an image as its background, the text lines one character per slot. A *Cells* switch hides the overlay (remembered per browser, forced on while placing), and a "rendering…" hint covers the debounce.

Nothing is written before *Save*. A shared page stays the shared file until *Save as … variant*. A file that does not parse is listed with its error and opens read-only, because the API takes a model, not text. *Delete* removes the file the editor shows after naming the plans that use the page (ADR 0073). *Rename* moves the shared page and every variant as one set and names the plans that keep the old name (ADR 0079). A variant of a display type that is no longer configured opens through a configured type of the same kind and is saved as that type's variant.

#### Plan editor

`/ui/editor/plan?name=<plan>` edits a plan (ADR 0069). A missing file opens as a new plan that the first save writes. The editor edits rules in their order: name, time window (`from`/`to` as times of day, days as checkboxes), pages in their order with `rotate` and `refresh`, and the fallback. It also edits the actions the device menu offers, built from the actions of the plugins with their arguments and confirmation.

The editor marks rules that an earlier rule always overrides. It names pages that the own devices of the plan would skip, and links each device to its `/ui/plan` preview and to its device page. *Delete* removes the plan after naming the devices that use it. *Rename* moves the file and names the devices that keep the old name (ADR 0079).

#### Device editor

The devices tab lists the files under `devices/` with display type, MAC, address and plan, and creates new ones in a dialog. Hardware that registered with an unknown MAC appears below them as unclaimed, with its hardware, firmware, address and first and last contact. *Claim* asks first, then opens a new device file with the MAC prefilled; *Forget* deletes its state file (ADR 0080).

`/ui/editor/device?id=<id>` edits one (ADR 0070): display type, MAC address, plan chosen from the plan files, `refresh`, the device menu (`actions`, `menu_show_duration`, `touch`), the power source, the firmware hold and the battery thresholds. It links the plan preview of the device and the current file text. The editor writes only the device file, so a plan assignment never touches `paperboy.yaml`. A new device file needs a display type and a MAC address.

*Delete* removes the file; the device stops syncing until a file with its MAC exists again (ADR 0073), and its state waits under the MAC. *Rename* moves only the file; the state stays with the MAC, so the page cursor, battery reading and menu survive (ADR 0079, 0080).

#### Display type editor

The displays tab lists the files under `displays/` with kind, size, `ppi` or `charset`, gray levels and power, and creates new types in a dialog. `/ui/editor/display?name=<name>` edits one (ADR 0071) with the fields of its kind, the physical size computed from `ppi`, the page variants and devices that reference it, and the current file text.

The `ppi` field has a helper. It converts the diagonal of the panel in inches or one side of its active area in millimetres into `ppi` from the pixel size in the form. It says so when the two disagree, because an inch diagonal is usually rounded. *Rename* moves only the file and names the page variants and devices that keep the old name (ADR 0079). *Delete* removes the file after naming the page variants and devices that reference it (ADR 0073).

## Device API

`POST /api/device/register` with `{"mac", "hardware", "firmware"}` (optionally `power` and `touch`) returns `{"key", "device"}` for a configured MAC (an existing key is kept) and `403` otherwise (ADR 0042, ADR 0064). Every wake sends one `POST /api/device/sync` with `Authorization: Bearer <key>`:

- `reason`: `timer`, `button`, `touch`, `powercycle`, `menu` or `result`.
- `events`: buttons by role `next`, `previous`, `home`, optionally with `hold_ms`; touches with `x`/`y`. Each event has a monotonically increasing `id`, and Paperboy ignores a repeated ID.
- `shown`: the hash on the panel.
- `battery`: `volts` is required, `percent`, `mah` and `ma` are optional.
- `rssi`, `power` and `log` entries.

The answer names the image (`image.path` relative to the Paperboy URL, `image.hash`) and the deep-sleep time `refresh_s`. It also names `listen_s` (0 after a timer wake, 20 s after input) and `sleep` (`false` for mains devices, ADR 0051). It names the server `time` in Unix seconds and `firmware` (an update offer, otherwise `null`). `wake` (`{reason, after_s}`) is a one-shot wake for the menu deadline or an action result; otherwise it is absent. An unknown key gets `401`, and the device registers again. An invalid body gets `400`.

A device whose file has a `Problem` answers with `503` and the reason, never with `401` (ADR 0079).

**Text displays** (ADR 0044): register and sync like ePaper displays, but the sync answers `"image": null` and `"text": {"lines", "hash"}`, and `listen_s` is always 0. They run on mains power and never sleep. Between syncs they poll `GET /api/device/text?shown=<hash>` with their key every `refresh_s`, which the plan decides (ADR 0045; the firmware syncs at most once a minute and uses one minute until its first sync). The answer is `{"lines", "hash"}`; unchanged text gets `204`. The poll renders the page the cursor points to without advancing it and writes nothing to the state file of the device. It updates the shown content in memory for `/api/devices` and the dashboard. An image display gets `409`. Paperboy word-wraps notices (no plan, broken page, critical battery) into the lines.

**Buttons**: events apply in order before rendering. `next` = next page, `previous` = one page back (wrapping), a long `next`/`previous` = first page of the active rule, `home` = the device menu (ADR 0057, ADR 0067, see [Actions](#actions)). A short `home` always opens the menu, also when the plan offers no actions; then it is an empty banner. With the menu open, `next`/`previous` move the selection and `home` confirms; a long `home` closes the menu and keeps the page. After `page_timeout` (5 min) the device shows the first page again. Touches operate the device menu when it is open (the Sticky sends taps, ADR 0064, issue #54); outside a menu, Paperboy only logs them. The server decides at 700 ms whether a press was long; the firmware only measures and reports `hold_ms`.

## Operation

### Intervals

`defaults.refresh` (at least 1 minute) or the `refresh` of the device applies; the `refresh` of a device may go down to 1 s. A plan rule may override it (down to `1s`). The reported interval never goes below the floor of the power source (ADR 0051). Battery devices keep one minute or the `min_refresh` of the display type if that is longer. Mains devices go down to `min_refresh`: by default `1s` for text and `10s` for image displays. When the composed image does not change, the display does not redraw.

### Power source

`power: auto|battery|mains` on the display type, overridable per device; default `auto` (ADR 0051). The firmware reports `power` (`battery` or `mains`) in register and sync: the Sticky measures USB power on GPIO9, the E1001 cannot detect it and reports `battery`, and text displays report `mains`. `auto` uses the last report (`reported_power` in `/api/devices`, next to the effective `power`) and behaves as `battery` until the device reports one. Text displays with older firmware therefore fall back to a one-minute floor until you update them or set `power: mains`. The sync answers `sleep: false` for mains devices: the ePaper firmware then stays awake with Wi-Fi up and syncs again after `refresh_s` instead of deep sleeping.

### Battery warning

The warning is fed by `battery.volts` of the sync (ADR 0020). With `battery` in `defaults` or on a device, every page below `warn_volts` gets the bar *Battery low – please charge* (*Akku schwach – bitte laden*). Below `critical_volts` a full-screen image *Battery almost empty* (*Akku fast leer*) appears instead, and buttons then do not page. A level ends only `hysteresis_volts` above its threshold. Without `battery` there is no warning. All texts use the configured `language`.

### Firmware updates

Put OTA images into `firmware/<hardware>-v<version>.bin` of the instance (`firmware/esphome/build.ps1 -Hardware sticky -Instance <dir>` does it). Paperboy offers the highest version in the sync as `{"version", "path", "md5"}`. It compares the version part by part as numbers; a `firmware.version` pin narrows it to exactly that image (ADR 0060). It offers an update when all of these conditions hold (ADR 0043):

- `firmware.update` is `auto`.
- The device has no `firmware: hold`.
- The reported version differs (a lower file is a rollback).
- The battery reading is at least `min_battery_volts` and not at warning level (text displays run on mains power and skip the battery condition).
- The version has not failed three times on that device.

A sync that still reports the old version after an offer counts as a failure. A changed image (new MD5) or clearing `firmware_failures` in the state file `state/devices/<MAC>.json` allows new attempts. Paperboy serves images at `GET /firmware/<file>` without a token; it ignores other file names and lists them in the log and on the dashboard. `GET /api/devices` adds `firmware_available` and `firmware_status` (`current`, `offered`, `available`, `failed`, `held`, `none`). Test the rollback with a broken image before you use `update: auto`.

### Device logs

The sync carries firmware log entries (`level`, `message`, optional `source`, `time` and `id`). Paperboy writes one line per entry with device, level, source and age. An entry with a `time` or `id` that a repeated sync delivers again is logged only once.

### Device list

`GET /api/devices` shows every configured device without keys: registration, hardware, firmware, battery voltage (and `battery_percent`/`battery_mah` from a fuel gauge), signal strength, wake reason and last sync. It also shows what the device is showing (ADR 0039). The fields are `last_page`, `last_page_kind` (`page`, `notice` or `battery`), `last_page_hash`, `last_page_url` and `last_refresh` (text displays: `last_text` instead of `last_page_url`). The last render or plan error is in `last_error`, and a derived `status` is `ok`, `late`, `never` or `unregistered`. `late` means the last sync is more than twice the reported refresh old; devices deep-sleep, so an idle device is not an offline one.

## Security

Devices are admitted by allow list and use their own key. The device API answers with plain HTTP status codes (`400`, `401`, `403`). The dashboard, the editor, the preview, the virtual display, `GET /api/devices`, `GET /api/plugins` and the page and plan API need no token, so keep Paperboy in a trusted network. Whoever reaches `/ui` can change the pages and plans. Only plugins named in `paperboy.yaml` run, so the configuration file is executable content.

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

The dashboard header shows version, commit and commit date. The commit and its date come from Go's VCS stamping, which works as long as the Git repository is present. The container image therefore keeps `.git` in the build context and installs the git binary in its build stage (the alpine Go image ships none).

The version name cannot come from VCS stamping. Go stamps any working-tree build with a pseudo-version derived from the commit (`v0.0.0-…`), even when the exact commit is tagged. Release builds therefore pass the tag in: `-ldflags "-X github.com/maehld/paperboy/internal/paperboy.version=v2.0.0"`. `release.yml`, the container image (`--build-arg VERSION=…`) and `scripts/build.ps1 -Version` do this. A build without it reports `dev` plus the short commit. A build without a repository or with `-buildvcs=false` reports `dev`. `go version -m <binary>` shows what was stamped.

The workflows build the three deliverables (ADR 0056). *CI* runs formatting, vet, tests and a cross-build on every change. *Plugins* and *Firmware* build their artifacts; the firmware only when `firmware/esphome/` changes. *Docker* publishes the image, and *Release* collects everything into one GitHub release on a tag.

```text
cmd/paperboy/            Startup, options and example configuration
cmd/weather/             Weather plugin: Open-Meteo (ADR 0049)
cmd/calendar/            Calendar plugin: CalDAV and iCalendar (ADR 0048, 0049)
cmd/evcc/                evcc plugin: energy flow, charging point, forecast
cmd/xkcd/                xkcd plugin: comic with optional title and alt text
cmd/countdown/           Countdown plugin: days to a date with a progress bar
cmd/departures/          Departures plugin: EFA real-time departures (ADR 0053)
cmd/myskoda/             MyŠkoda plugin: vehicle state and commands
cmd/pokedex/             Pokédex plugin: a Pokémon card from PokéAPI
cmd/rss/                 RSS and Atom plugin: feed list and headline
cmd/odtest/              Test program for OpenDisplay devices (TCP and Bluetooth)
pkg/plugin/              Plugin protocol and Go helper (public API)
pkg/render/              Fonts, palettes and drawing shared by Paperboy and plugins (public API)
internal/paperboy/       YAML configuration, device API, plans, battery warning, dashboard
internal/page/           Pages: grid parsing and composition from widgets
internal/widget/         Plugin processes, widget cache and background refresh
internal/builtin/        Built-in plugin "paperboy": header, text, clock
internal/plan/           Display plans: time rules that select pages per device (ADR 0025)
internal/download/       Release downloads and the verification against SHA256SUMS
internal/semver/         Version grammar for plugin and firmware names (ADR 0089)
internal/opendisplay/    OpenDisplay protocol; odble = Bluetooth transport
firmware/esphome/        Paperboy firmware on ESPHome for Sticky and E1001 (ADR 0041)
docs/adr/                Architecture decisions
```

## Licence

Paperboy is published under the [MIT licence](LICENSE). Third-party material keeps its own licence:

- The OpenMoji pictograms in `cmd/weather/icons/` and `cmd/evcc/icons/` are [CC BY-SA 4.0](cmd/weather/icons/README.md); the licence travels with the plugin zip.
- The firmware in `firmware/esphome/` is a Paperboy configuration for [ESPHome](https://esphome.io). The images built from it contain the ESPHome runtime, whose C++ parts are GPLv3; the sources are in this repository. The bundled Go fonts (`pkg/render`) come with their own BSD-style licence.
- The examples and the plugin test fixtures are fictional; xkcd comics are CC BY-NC 2.5 and are fetched at runtime, never shipped.
