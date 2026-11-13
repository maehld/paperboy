[← Paperboy](../README.md)

# Setup

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
ui:                    # optional: the password of the operator UI (ADR 0096)
  password: ${PAPERBOY_UI_PASSWORD}
# Display types are files under displays/ (ADR 0071); devices are files under devices/ (ADR 0070).
```

- `plugins` declares the plugins (ADR 0049). `config` is the plugin's own section. String values expand `${VAR}` from the environment of Paperboy: an unset variable rejects the configuration, `$$` is a literal `$`, so secrets stay out of the file. The plugin checks its keys and rejects unknown ones. A rejected configuration appears as a notice in the cells of that plugin and in the log. The name `paperboy` is reserved for the built-in plugin.
- `language` (`de` or `en`) selects the language of the built-in image texts, the number format and the language plugins draw in. Without it, Paperboy uses the system language (Windows UI language, otherwise `LANG`/`LC_ALL`/`LC_MESSAGES`) and falls back to English. `timezone` (IANA name) applies to plans and built-in dates; without it, Paperboy uses the system timezone.
- `-config` names the instance directory (default `.`). Paperboy reads `<dir>/paperboy.yaml` and finds all other directories there. Paperboy rejects a file argument with an explanation and does not read a `.json` configuration.
- Paperboy reloads the configuration on change without a restart (ADR 0047). Handlers pick up a modified `paperboy.yaml` or a changed file under `displays/` or `devices/` on the next request. Plugins whose section changed are restarted, and their cached widgets are dropped. A broken file keeps the last valid configuration. If `paperboy.yaml` is not usable at startup in an existing instance directory, the server still starts in degraded mode. It serves no display types and no devices, and applies the file as soon as it loads, also when its mod time did not change. A wrong `-config` stays fatal.

## Operator UI

The dashboard, the editors and their API are operator business: they show the instance's content (calendars, departures, energy, the displays' MAC addresses) and they write pages, plans, devices and display types (ADR 0096). `ui.password` protects all of it:

```yaml
ui:
  password: ${PAPERBOY_UI_PASSWORD}
```

- The password goes into `paperboy.yaml` through the environment, like every other secret; without `${…}` it would sit in the file in plain text.
- The browser shows its own login prompt, the user name is ignored. `curl -u :password http://…/api/devices` works for scripts. Requests without credentials answer `401` with a hint that names `ui.password`.
- Without the `ui` section the UI stays open, as it was before this option existed. Paperboy warns at startup when the listener is not loopback and no password is set. A password added or changed later applies to the next request, without a restart.
- A changed password ends the sessions of open tabs: their next request answers `401` and the browser asks again. There is no logout, because the browser keeps Basic credentials until it is closed; use a private window on a shared machine.
- The UI password is not the device key. A display keeps registering, syncing and downloading its pages with its own key (ADR 0042, ADR 0097); the firmware images on `/firmware/` stay open, because they are public release artifacts. Requests from another site cannot write: they are refused with `403` even when the browser would send the password along.
- **Expose beyond the LAN:** Paperboy does not terminate TLS, and both the password and the page images of the displays travel in clear text through the LAN. Put a reverse proxy in front that terminates HTTPS and forwards to Paperboy's `-listen` address, and keep the device URL (`http://<host>:<port>`) as it is, because the firmware keeps that address. The proxy's address is what the dashboard shows as the device's last contact (ADR 0083).

## Display types

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
- Pages and notice pages go only to a device that identifies itself with its key (ADR 0097); the firmware sends it with the image download. A key only reaches pages of its own display type. The dashboard shows the last served image through the UI route `/api/devices/<id>/image`, which needs `ui.password` like the rest of the operator UI.
- `touch: true` makes the device menu draw its tap controls. A device without touch keeps the button-only menu (ADR 0082). If the key is absent, Paperboy uses what the firmware reported.
