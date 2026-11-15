[← Design documents](README.md) · [Paperboy](../../README.md)

# Plugins

A plugin is a long-running program that draws widgets. Paperboy starts it, configures it once and then asks it for widgets and actions. Every value on a page comes from a plugin; Paperboy itself holds no data.

The reference of the protocol is [`../plugins/protocol.md`](../plugins/protocol.md), and every shipped plugin has its own page under [`../plugins/`](../plugins/README.md). This document describes how the parts fit together.

## A plugin in the instance

A program is found in `plugins/` first and on `PATH` second (ADR 0060, 0075). Within one directory:

1. an unversioned `<plugin>` wins,
2. then the pinned version,
3. then the highest `<plugin>-v<version>`.

The resolved path is part of the plugin's specification, so installing a new version restarts the plugin and drops its cached widgets. The name `paperboy` is reserved for the built-in plugin.

## The protocol

Paperboy and the plugin exchange one JSON object per line: a request on stdin, an answer on stdout with the same `id`. The protocol version is 6 (ADR 0103). Paperboy requires at least version 5 and accepts newer ones, because the protocol only grows additively. `Message.CheckVersion` runs on every answer; a wrong version fails that plugin with one clear log line instead of a broken page (ADR 0058, 0081).

| Action | Request carries | Answer carries |
|---|---|---|
| `configure` | The plugin's `config`, `env`, `language`, `timezone` | Nothing, or an error |
| `state` | The stored state and the declared blobs | New state and changed blobs |
| `describe` | – | Widgets, actions, conditions, config keys, blob names, version |
| `render` | Widgets with pixel `width`/`height`, `args`, `style` | One base64 PNG per widget |
| `text` | The same in characters | `lines` per widget |
| `actions` | Concrete plan actions, the device | State per action: title, `disabled` reason, `confirm` |
| `action` | One action with its args, the device | `result` with `ok`, `message`, `changed` |
| `conditions` | – | A map of the declared conditions, true while active |

Every request carries `language`, `timezone`, `now` and `levels` (2, 4 or 16). It also carries the `page` with title, number, count and display size, and `ppi` for image displays. A widget also gets `style` with `padding`, `rule` and `text_px` in pixels, and `scroll` on a text display.

A plugin answers with `version`, optionally `state`, `blobs`, `widgets`, `actions`, `result`, `conditions`, `max_age`, `description`, or a `changed` event.

## What a plugin declares

`describe` is how the editors learn what a plugin offers:

- **Widgets** with their sizes as spans of the 6 × 8 reference grid, and the arguments of each widget.
- **Actions** with name, title, arguments, whether they need a confirmation, and the conditions under which they are offered.
- **Conditions** with name, title and description, for the rules of a plan (ADR 0103).
- **Config keys**, the **blob names** that Paperboy stores for it, and its own **version**.

An argument declares its type, default, whether it is required and its description. It can also declare an `enum` of allowed values with `enum_labels` for the display text (ADR 0055). The stored value is the key; an editor without a label shows the key. `plugin.Values.Check` rejects a value outside the declared set, so a typo in a page fails loudly instead of drawing something unexpected.

A widget that cannot draw anything in its cell reports `ErrTooSmall` with the cell size instead of leaving the cell empty. Paperboy draws its hatched notice with that reason, and every shipped plugin does this.

## The plugin host

`internal/widget` owns the processes (ADR 0049).

- A plugin starts on first use. After an exit, after its `timeout` (default 30 seconds, killed) and after a change of its specification it is started again. Removed plugins drop their cached results.
- The cache is keyed by plugin, widget, arguments, size, gray levels or text, style, page, language and timezone. An entry lives until the plugin's `max_age` (default 5 minutes).
- A background loop every 5 seconds renders entries that were used within the last hour again before they expire, or after a `changed` event. Entries idle for an hour are evicted, and the cache holds at most 1000 entries.
- A failing plugin serves its stale results and is not asked again for a minute.
- The built-in plugin runs in process and is never cached.

Conditions get their own cache of 10 seconds per plugin. A sync therefore never waits, and a text display that polls every second does not ask every time. A `changed` event or a changed specification drops the answer. A plugin that fails answers with an error, and then every condition counts as inactive.

## State and blobs

A plugin that needs to survive a restart sends its state with every answer (ADR 0081). Paperboy writes it to `state/plugins/<id>/state.json` and the blobs to `state/plugins/<id>/blobs/<name>`, atomically and readable by the owner only. After `configure`, every plugin gets them once in a `state` request, so it starts where it stopped.

The limits are 256 KiB per state, 1 MiB per blob and four blobs. A missing `state` means "unchanged" and `null` means "forget". An invalid name or an oversized value is logged and ignored; nothing is deleted automatically.

The Dexcom plugin keeps its session and the readings of the last day this way. The MyŠkoda plugin keeps its last fetch. A restart within the interval therefore costs no request.

## Versions

Every plugin has its own version in `cmd/<plugin>/VERSION`, embedded in the program (ADR 0075). The plugin zip names the programs `<plugin>-v<version>`. A change to a plugin raises the version in the same commit. The *Plugins* workflow fails a pull request that changes `cmd/<plugin>` without raising it. A change to shared `pkg/` code only warns.

`describe` reports the plugin's own version. When it differs from the version in the program's file name, Paperboy logs one warning per plugin.

## The built-in plugin

`internal/builtin` provides the plugin `paperboy`: `header`, `text`, `clock` and `logo`, for image and text displays (ADR 0049, 0095). It draws from `pkg/render` and needs no data, so it is always available and never cached.

## The shipped plugins

| Plugin | Data source | Widgets |
|---|---|---|
| [`calendar`](../plugins/calendar.md) | CalDAV, CardDAV, iCalendar links and files | `agenda`, `birthdays`, `month`, `week` |
| [`weather`](../plugins/weather.md) | Open-Meteo | `current`, `days`, `hours` |
| [`evcc`](../plugins/evcc.md) | evcc `/api/state` and its energy history | `site`, `flow`, `loadpoint`, `forecast`, `history`, `chart`, `values`, action `mode` |
| [`departures`](../plugins/departures.md) | EFA rapidJSON | `departures` |
| [`myskoda`](../plugins/myskoda.md) | MyŠkoda Public API | `battery`, `charging`, `profiles`, `location`, `climate`, `card`, `car`, actions `charging` and `climate` |
| [`dexcom`](../plugins/dexcom.md) | Dexcom Share | `glucose`, `graph`, `values`, conditions `urgent`, `low`, `high` |
| [`rss`](../plugins/rss.md) | RSS 2.0, RSS 1.0 and Atom | `feed`, `headline` |
| [`xkcd`](../plugins/xkcd.md) | xkcd | `comic` |
| [`countdown`](../plugins/countdown.md) | none, every date is an argument | `countdown` |
| [`pokedex`](../plugins/pokedex.md) | PokéAPI | `card` |

Most of them have text variants. The [plugin pages](../plugins/README.md) describe arguments, config keys, refresh rules and the data source of each one.

## Decisions

ADR 0048 (calendar) · 0049 (plugin widgets) · 0053, 0100 (departures) · 0055 (argument values) · 0057 (actions) · 0058 (protocol version check) · 0065 → 0081 (state) · 0075 (a version per plugin) · 0099 (Dexcom) · 0103 (conditions)
