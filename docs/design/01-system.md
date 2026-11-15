[← Design documents](README.md) · [Paperboy](../../README.md)

# System

## What Paperboy is

Paperboy is a display service for e-paper and text displays. It composes pages from widgets, selects a page by a plan and delivers it to a device.

The service holds no data of its own. Every value on a page comes from a plugin: a long-running program that draws a widget into exactly the size of its cell. Paperboy composes the pages, caches them and delivers them.

The devices run Paperboy's own firmware on ESPHome (ADR 0041). A device sleeps, wakes up for one cycle, asks for the next page, paints it and sleeps again.

Paperboy grew out of the display service of SolarAI (ADR 0028, 0029). Version 2 (ADR 0049) replaced the data document of version 1: that document held values, and a layout put them on a page. Now a plugin draws its widget itself. The old ADRs stay as history; this documentation describes only version 2.

## Parts

| Part | Runs as | Job |
|---|---|---|
| Paperboy | one Go binary | configuration, plans, page composition, device API, operator UI, downloads |
| Plugins | one child process per plugin | fetch data and draw widgets |
| Firmware | on the device | register, sync, download, paint, sleep |
| Instance directory | files on disk | configuration, content, plugin programs and state |

Paperboy imports no SolarAI package. A plugin that needs SolarAI (or any other service) talks to it itself. Paperboy talks only to its plugins, over stdin and stdout.

## Process model

Paperboy is a single process with a single HTTP listener. The device API and the operator UI share that listener. The routes decide which of the two a request belongs to. So the device API stays open while the UI has a password (ADR 0096).

A plugin is a child process. Paperboy starts it on first use, sends its configuration once and keeps it running. A plugin that exits or does not answer within its `timeout` is killed and started again (ADR 0049).

Background loops keep the service warm:

- The configuration watcher reloads `paperboy.yaml` when it changes (ADR 0047).
- A stamp of name, size and modification time reloads when a file under `displays/` or `devices/` changes.
- The widget host renders cached widgets again before they expire and after a plugin reports a change (ADR 0049).
- The download check runs at startup and then once per `download.interval` (ADR 0060).

Concurrency rule: a device request takes the configuration before it locks the state. It selects the page under the lock, and it renders, draws and writes without it. A plan condition is asked before the lock too (ADR 0103). A plugin request therefore never runs while the state lock is held, and a slow plugin cannot block the other devices.

## Data flow

1. A device wakes and calls `POST /api/device/sync`. The operator UI can ask the same questions for a plan preview or for the virtual display instead.
2. Paperboy asks the plan's conditions: one `conditions` call per plugin that a rule names. The widget host answers from a cache of 10 seconds.
3. The plan selects a page: the first rule whose time window, weekdays and conditions match, else its fallback. A page that does not fit the device's display type is skipped (ADR 0049).
4. The page composes its cells. All widgets of the page go to the widget host in one request. The render therefore costs one round trip per plugin, not one per cell.
5. The widget host answers from its cache. A missing or expired entry is drawn by the plugin process.
6. Paperboy quantizes every widget image to the gray levels of the display type, pastes it one to one into the page and hashes the pixels.
7. The finished page is kept in memory as PNG and as its hash. A device downloads it from `/pages/{display type}/{page}/{hash}.png`, or as QOI with `?format=qoi` for the firmware (ADR 0072).
8. The device paints the page, reports its battery and goes back to sleep, or stays awake for a menu.
9. A page that cannot be composed becomes a notice page (`/hints/{hash}.png`), never an error.

## Instance directory

All content lives in one directory, selected with `-config <dir>`; the default is `.` (ADR 0037, 0049, 0070, 0071).

```text
paperboy.yaml                       configuration
displays/<name>.yaml                display types
devices/<id>.yaml                   devices
pages/<page>.<display type>.yaml    a page for one display type
pages/<page>.yaml                   a page for every display type without its own file
plans/<name>.yaml                   plans
plugins/<plugin>-v<version>         plugin programs, optional
firmware/<hardware>-v<version>.bin  firmware images
state/devices/<MAC>.json            device state
state/plugins/<plugin>/state.json   plugin state Paperboy writes (ADR 0081)
state/plugins/<plugin>/blobs/<name>  files a plugin hands to Paperboy
state/editor/                       backups of edited and deleted files (ADR 0066)
state/download/<train>/cache.json   release lists per release train (ADR 0090)
```

The file name is the stable name. A page variant is named after its display type, a device file after the device ID. A display type file carries the name that pages and devices refer to. Renaming a file therefore means renaming a reference; the editor names the references and moves the file only (ADR 0079).

`plugins/` is optional. Paperboy finds a program in `plugins/` first and on `PATH` second (ADR 0060, 0075).

Old instances from version 1 still carry `providers/` and `icons/`. Nothing reads them, and they can be deleted by hand.

## Configuration

`paperboy.yaml` is parsed strictly: an unknown key is an error with its line. The top level holds:

| Key | Content |
|---|---|
| `language`, `timezone` | Language and zone for every page, plan and plugin (ADR 0033) |
| `plugins` | One entry per plugin: `version`, `timeout`, `env`, `config` (ADR 0049, 0060) |
| `download` | `source`, `token`, `interval`, `plugin_channel`, `firmware_channel` (ADR 0060, 0062, 0089) |
| `defaults` | `refresh`, `page_timeout`, `menu_show_duration` (ADR 0057) |
| `firmware` | `version` pin for the images an instance installs |
| `ui` | `password` for the operator UI (ADR 0096) |

A plugin entry expands `${VAR}` from the environment, `$$` becomes `$`, and an unset variable is an error. Secrets therefore stay out of the file. The plugin name `paperboy` is reserved for the built-in plugin, and a `command` key is rejected with a hint.

The sections `display_types` and `devices` of version 1 are rejected with a hint. Display types are files under `displays/` (ADR 0071), devices are files under `devices/` (ADR 0070). The version 1 sections `server` and `providers` are rejected the same way.

## Reload

The whole configuration reloads on change (ADR 0047). Each handler reads a snapshot through `Server.config()`, so a request that runs during a reload works on one consistent view.

- A broken file keeps the last valid configuration and logs the error. The service never stops because of a typo.
- A start with a broken file serves an empty configuration until the file loads.
- The background loop hands the plugin section to the widget host every two seconds. A changed specification restarts the plugin, and a removed one drops its cached results.
- A named plan that is missing no longer aborts the start.
- A device whose display type is not configured still loads, but carries a `Problem`. Its sync and text poll answer 503 with the reason (ADR 0079).

## Code map

| Path | Responsibility |
|---|---|
| `cmd/paperboy/` | Startup, options and the example configuration |
| `internal/paperboy/` | Configuration, device API, plans, state, operator UI, downloads |
| `internal/page/` | Page parsing and composition from widgets |
| `internal/plan/` | Time rules that select pages |
| `internal/widget/` | Plugin processes, widget cache, background refresh, conditions |
| `internal/builtin/` | Built-in plugin `paperboy`: header, text, clock, logo |
| `internal/download/` | Release lists, archives and verification |
| `internal/semver/` | Version grammar of programs and images |
| `internal/stats/` | Timers, gauges and process numbers for the stats page |
| `internal/opendisplay/` | OpenDisplay protocol and its Bluetooth transport |
| `pkg/plugin/` | Public plugin protocol and its Go helper |
| `pkg/render/` | Fonts, palettes and drawing shared by Paperboy and plugins |
| `firmware/esphome/` | The firmware for Sticky, E1001, E1003 and the LCD2004 |
| `docs/` | These documents, the ADRs, the topic pages and the plugin reference |

## Decisions

ADR 0028, 0029 (own repository) · 0031 (English) · 0033 (language and timezone) · 0037 (instance directory) · 0047 (reload) · 0049 (v2 and plugin widgets) · 0056 (repository and release trains) · 0070, 0071 (device and display type files) · 0079 (renaming) · 0096 (UI password) · 0103 (conditions)
