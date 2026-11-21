[← Design documents](README.md) · [Paperboy](../../README.md)

# System

## What Paperboy is

Paperboy is a display service for e-paper and text displays. It composes pages from widgets, selects a page by a plan and delivers it to a device.

The service holds no data of its own. Every value on a page comes from a plugin: a long-running program that draws a widget into exactly the size of its cell. Paperboy composes the pages, caches them and delivers them.

The devices run Paperboy's own firmware on ESPHome ([ADR 0041](../adr/0041-esphome-firmware.md)). A device sleeps, wakes up for one cycle, asks for the next page, paints it and sleeps again.

Version 2 ([ADR 0049](../adr/0049-plugin-widgets.md)) replaced the data document of version 1: that document held values, and a layout put them on a page. Now a plugin draws its widget itself. The old ADRs stay as history; this documentation describes only version 2.

## Parts

| Part | Runs as | Job |
|---|---|---|
| Paperboy | one Go binary | configuration, plans, page composition, device API, operator UI, downloads |
| Plugins | one child process per plugin | fetch data and draw widgets |
| Firmware | on the device | register, sync, download, paint, sleep |
| Instance directory | files on disk | configuration, content, plugin programs and state |

Paperboy imports no package of another service. A plugin that needs another service talks to it itself. Paperboy talks only to its plugins, over stdin and stdout.

## Process model

Paperboy is a single process with a single HTTP listener. The device API and the operator UI share that listener. The routes decide which of the two a request belongs to. So the device API stays open while the UI has a password ([ADR 0096](../adr/0096-a-password-for-the-operator-ui.md)).

A plugin is a child process. Paperboy starts it on first use, sends its configuration once and keeps it running. A plugin that exits or does not answer within its `timeout` is killed and started again ([ADR 0049](../adr/0049-plugin-widgets.md)).

Background loops keep the service warm:

- The configuration watcher reloads `paperboy.yaml` when it changes ([ADR 0047](../adr/0047-config-reload.md)).
- A stamp of name, size and modification time reloads when a file under `displays/` or `devices/` changes.
- The widget host renders cached widgets again before they expire and after a plugin reports a change ([ADR 0049](../adr/0049-plugin-widgets.md)).
- The download check runs at startup and then once per `download.interval` ([ADR 0060](../adr/0060-download-from-releases.md)).

Concurrency rule: a device request takes the configuration before it locks the state. It resolves the plan, its rule and the pages that fit before the lock ([ADR 0138](../adr/0138-resolve-plans-outside-the-state-lock.md)) and applies the page cursor under it. It renders, draws and writes without the lock. The values of a plan condition are read before the lock too ([ADR 0127](../adr/0127-plan-conditions-are-values.md)). A plugin request therefore never runs while the state lock is held, and a slow plugin cannot block the other devices.

Paperboy replaces every file below `state/` through a unique temporary file and a rename. Two editors, a repeated sync or a plugin that saves at the same time therefore leave a complete file, never a mix.

Two memory caches hold composed pages ([ADR 0139](../adr/0139-bound-the-page-caches.md)). The rendered pages hold at most 256 entries, and an entry older than 24 hours goes. The notice pages hold at most 500 entries and drop the least recently used. A dropped page is composed again on the next request. A device that asks for a dropped notice gets `404` and keeps its screen until the next sync. The notes that suppress repeated refusal logs keep at most 256 paths.

## Data flow

1. A device wakes and calls `POST /api/device/sync`. The operator UI can ask the same questions for a plan preview or for the virtual display instead.
2. Paperboy reads the values that the plan's conditions name, one request per plugin, and keys them by `plugin/value`. The normal value cache answers a poll that repeats them.
3. The plan selects a page: the first rule whose time window, weekdays and value conditions match, else its fallback. A page that does not fit the device's display type is skipped ([ADR 0049](../adr/0049-plugin-widgets.md)).
4. The page composes its cells. All widgets of an image page or all values of a text page go to the plugin host in one request per plugin. The render therefore costs one round trip per plugin, not one per cell.
5. The plugin host answers from its cache. A missing or expired entry is drawn or answered by the plugin process.
6. Paperboy quantizes every widget image to the gray levels of the display type, pastes it one to one into the page and hashes the pixels. A text page formats its values and lays them into its boxes (ADR 0118).
7. The finished page is kept in memory as PNG and as its hash. A device downloads it from `/pages/{display type}/{page}/{hash}.png`, or as QOI with `?format=qoi` for the firmware ([ADR 0072](../adr/0072-pages-as-qoi.md)).
8. The device paints the page, reports its battery and goes back to sleep, or stays awake for a menu.
9. A page that cannot be composed becomes a notice page (`/hints/{hash}.png`), never an error.

## Instance directory

All content lives in one directory, selected with `-config <dir>`; the default is `.` ([ADR 0037](../adr/0037-instance-directory-layout.md), [0049](../adr/0049-plugin-widgets.md), [0070](../adr/0070-devices-in-their-own-files.md), [0071](../adr/0071-display-types-in-files-of-their-own.md)).

```text
paperboy.yaml                       configuration
displays/<name>.yaml                display types
devices/<id>.yaml                   devices
pages/<page>.yaml                   a page for its kind, with its variants map
pages/<page>.<label>.yaml           a layout that the page names for a display type
plans/<name>.yaml                   plans
plugins/<plugin>-v<version>         plugin programs, optional
firmware/<hardware>-v<version>.bin  firmware images
state/devices/<MAC>.json            device state
state/plugins/<plugin>/state.json   plugin state Paperboy writes (ADR 0081)
state/plugins/<plugin>/blobs/<name>  files a plugin hands to Paperboy
state/editor/                       backups of edited and deleted files (ADR 0066)
state/download/<train>/cache.json   release lists per release train (ADR 0090)
```

The file name is the stable name. A variant is named by the label that the page file gives it in `variants`; a device file is named after the device ID. A display type file carries the name that pages and devices refer to. Renaming a file therefore means renaming a reference; the editor names the references and moves the file only ([ADR 0079](../adr/0079-rename-config-items.md), [0110](../adr/0110-pages-serve-their-kind-and-name-variants.md)).

`plugins/` is optional. Paperboy finds a program in `plugins/` first and on `PATH` second ([ADR 0060](../adr/0060-download-from-releases.md), [0075](../adr/0075-plugin-versions.md)).

Old instances from version 1 still carry `providers/` and `icons/`. Nothing reads them, and they can be deleted by hand.

## Configuration

`paperboy.yaml` is parsed strictly: an unknown key is an error with its line. The top level holds:

| Key | Content |
|---|---|
| `language`, `timezone` | Language and zone for every page, plan and plugin ([ADR 0033](../adr/0033-global-language-and-timezone.md)) |
| `plugins` | One entry per plugin: `version`, `timeout`, `env`, `config` ([ADR 0049](../adr/0049-plugin-widgets.md), [0060](../adr/0060-download-from-releases.md)) |
| `download` | `source`, `token`, `interval`, `plugin_channel`, `firmware_channel` ([ADR 0060](../adr/0060-download-from-releases.md), [0062](../adr/0062-plugin-updates-and-download-control.md), [0089](../adr/0089-release-channels.md)) |
| `defaults` | `refresh`, `menu_show_duration` ([ADR 0057](../adr/0057-plugin-actions-from-the-plan.md)) |
| `firmware` | `version` pin for the images an instance installs |
| `ui` | `password` for the operator UI ([ADR 0096](../adr/0096-a-password-for-the-operator-ui.md)) |

A plugin entry expands `${VAR}` from the environment, `$$` becomes `$`, and an unset variable is an error. Secrets therefore stay out of the file. The plugin name `paperboy` is reserved for the built-in plugin, and a `command` key is rejected with a hint.

The sections `display_types` and `devices` of version 1 are rejected with a hint. Display types are files under `displays/` ([ADR 0071](../adr/0071-display-types-in-files-of-their-own.md)), devices are files under `devices/` ([ADR 0070](../adr/0070-devices-in-their-own-files.md)). The version 1 sections `server` and `providers` are rejected the same way.

## Reload

The whole configuration reloads on change ([ADR 0047](../adr/0047-config-reload.md)). Each handler reads a snapshot through `Server.config()`, so a request that runs during a reload works on one consistent view.

- A broken file keeps the last valid configuration and logs the error. The service never stops because of a typo.
- A start with a broken file serves an empty configuration until the file loads.
- The background loop hands the plugin section to the widget host every two seconds. A changed specification restarts the plugin, and a removed one drops its cached results.
- A named plan that is missing no longer aborts the start.
- A device whose display type is not configured still loads, but carries a `Problem`. Its sync and text poll answer 503 with the reason ([ADR 0079](../adr/0079-rename-config-items.md)).

## Code map

The repository has three Go modules ([ADR 0146](../adr/0146-server-plugins-and-firmware-in-their-own-directories.md)). `server/` is the service. `plugins/` holds the shipped plugin programs. `sdk/` holds the public plugin protocol and the shared drawing. `go.work` binds them for local builds.

| Path | Responsibility |
|---|---|
| `server/cmd/paperboy/` | Startup, options and the example configuration |
| `server/internal/paperboy/` | Configuration, device API, plans, state, operator UI, downloads |
| `server/internal/atomicfile/` | Replace a file in one step |
| `server/internal/page/` | Page parsing and composition from widgets and values |
| `server/internal/plan/` | Time rules that select pages |
| `server/internal/widget/` | Plugin processes, widget and value cache, background refresh |
| `server/internal/builtin/` | Built-in plugin `paperboy`: header, text, clock, logo, battery, device status, values |
| `server/internal/download/` | Release lists, archives and verification |
| `server/internal/duration/` | Durations in the configuration and in plans |
| `server/internal/format/` | Text helpers shared with the built-in plugin |
| `server/internal/ident/` | The name rule of pages, plans, displays, plugins and devices |
| `server/internal/reading/` | Catalog of the readings a device may report |
| `server/internal/semver/` | Version grammar of programs and images |
| `server/internal/stats/` | Timers, gauges and process numbers for the stats page |
| `server/internal/strictyaml/` | One YAML document with known fields only |
| `plugins/cmd/<plugin>/` | The shipped plugin programs, one program per plugin |
| `plugins/internal/imagelimit/` | Pixel budget for an image from an untrusted source |
| `sdk/plugin/` | Public plugin protocol and its Go helper |
| `sdk/render/` | Fonts, palettes and drawing shared by Paperboy and plugins |
| `firmware/esphome/` | The firmware for Sticky, E1001, E1003 and the LCD2004 |
| `docs/` | These documents, the ADRs, the topic pages and the plugin reference |

## Decisions

[ADR 0028](../adr/0028-paperboy-eigenes-repository.md), [0029](../adr/0029-repository-trennung-umgesetzt.md) (own repository) · [0031](../adr/0031-everything-in-english.md) (English) · [0033](../adr/0033-global-language-and-timezone.md) (language and timezone) · [0037](../adr/0037-instance-directory-layout.md) (instance directory) · [0047](../adr/0047-config-reload.md) (reload) · [0049](../adr/0049-plugin-widgets.md) (v2 and plugin widgets) · [0056](../adr/0056-repository-layout-and-release-trains.md) (repository and release trains) · [0070](../adr/0070-devices-in-their-own-files.md), [0071](../adr/0071-display-types-in-files-of-their-own.md) (device and display type files) · [0079](../adr/0079-rename-config-items.md) (renaming) · [0096](../adr/0096-a-password-for-the-operator-ui.md) (UI password) · [0127](../adr/0127-plan-conditions-are-values.md) (conditions are values) · [0138](../adr/0138-resolve-plans-outside-the-state-lock.md) (plans outside the state lock) · [0139](../adr/0139-bound-the-page-caches.md) (bounded page caches) · [0146](../adr/0146-server-plugins-and-firmware-in-their-own-directories.md) (one directory per deliverable)
