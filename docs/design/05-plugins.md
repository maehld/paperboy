[← Design documents](README.md) · [Paperboy](../../README.md)

# Plugins

A plugin is a long-running program that draws widgets. Paperboy starts it, configures it once and then asks it for widgets and actions. Every value on a page comes from a plugin; Paperboy itself holds no data.

The reference of the protocol is [`../plugins/protocol.md`](../plugins/protocol.md), and every shipped plugin has its own page under [`../plugins/`](../plugins/README.md). This document describes how the parts fit together.

## A plugin in the instance

A program is found in `plugins/` first and on `PATH` second ([ADR 0060](../adr/0060-download-from-releases.md), [0075](../adr/0075-plugin-versions.md)). Within one directory:

1. an unversioned `<plugin>` wins,
2. then the pinned version,
3. then the highest `<plugin>-v<version>`.

The resolved path is part of the plugin's specification, so installing a new version restarts the plugin and drops its cached widgets. The name `paperboy` is reserved for the built-in plugin.

## The protocol

Paperboy and the plugin exchange one JSON object per line: a request on stdin, an answer on stdout with the same `id`. The protocol version is 9 ([ADR 0111](../adr/0111-text-pages-from-plugin-values.md), [0125](../adr/0125-remove-the-text-widget-path.md), [0126](../adr/0126-a-schedule-event-for-plugin-demand.md), [0127](../adr/0127-plan-conditions-are-values.md)). Paperboy requires at least version 5 and accepts newer ones. The protocol grew additively until version 8. Version 8 removed the text action; version 9 adds an inbound schedule event and the `enum` value type, and removes conditions. A plugin below 8 that still has text widgets is accepted, because nothing asks it for text. A plugin below 9 never receives a schedule and cannot declare an enum value. `Message.CheckVersion` runs on every answer; a wrong version fails that plugin with one clear log line instead of a broken page ([ADR 0058](../adr/0058-plugin-protocol-version-check.md), [0081](../adr/0081-plugin-state-over-the-protocol.md)).

Besides answers, Paperboy may send a line without `id`: the schedule event names when it next needs the plugin's data. An event without `next_refresh` means no demand now. It goes only to a plugin that reports version 9 or newer. A plugin that relies on the schedule requires version 9 and refuses an older host. A plugin ignores events it does not know. `pkg/plugin.Demand` receives the event. `Wait(ctx, floor)` sleeps until the named moment minus 30 seconds, wakes on a new schedule, and never returns earlier than `floor` after the call. A plugin calls it after each fetch, so the floor counts from the end of the fetch. The event format is `{"event":"schedule","next_refresh":"…"}`.

Paperboy computes the demand per plugin from the configured devices. It takes the expected contact (`last_seen + refresh` of the last sync). It also takes the pages of the rule the device is on that fit its display type, and the values of the plan's conditions. Actions do not count; they are resolved when the menu opens. A device that is late or never seen adds no demand until it contacts again, so a powered-off device cannot keep a plugin polling. Previews and the virtual display add no demand either.

| Action | Request carries | Answer carries |
|---|---|---|
| `configure` | The plugin's `config`, `env`, `language`, `timezone` | Nothing, or an error |
| `state` | The stored state and the declared blobs | New state and changed blobs |
| `describe` | – | Widgets, values, actions, config keys, blob names, version |
| `render` | Widgets with pixel `width`/`height`, `args`, `style` | One base64 PNG per widget |
| `values` | The value keys a text page needs and `value_args`, the arguments of that set | `values` per key |
| `actions` | Concrete plan actions, the device | State per action: title, `disabled` reason, `confirm` |
| `action` | One action with its args, the device | `result` with `ok`, `message`, `changed` |

Every request carries `language`, `timezone`, `now` and `levels`, the tone count of the display. `levels` is 1 to 256; a canvas falls back to 2, 4 or 16 tones when it cannot draw the value ([ADR 0107](../adr/0107-plugin-builds-reach-installations.md)). The request also carries the `page` with title, number, count and display size, and `ppi` for image displays. A widget also gets `style` with `padding`, `rule` and `text_px` in pixels.

`server_status` is Paperboy's own view of the devices (ADR 0115). It holds `current_device_status`, the device the page is rendered for, and `selected_devices_status`, a map of only the devices the page names in its widget arguments. A status names the id, display type, plan, hardware and firmware. It also names the contact state, last contact, power source, signal and battery reading. The estimated runtime and the last page or error follow. It carries no key, MAC or address. A preview without a device gets the sample device `preview` with fixed values ([ADR 0123](../adr/0123-a-sample-device-in-previews.md)). Only the built-in plugin receives the object; a plugin process would cache stale rows.

A plugin answers with `version`, optionally `state`, `blobs`, `widgets`, `actions`, `result`, `max_age`, `description`, or a `changed` event.

## What a plugin declares

`describe` is how the editors learn what a plugin offers:

- **Widgets** with their sizes as spans of the 6 × 8 reference grid, and the arguments of each widget.
- **Actions** with name, title, arguments, whether they need a confirmation, and the conditions under which they are offered.
- **Values** with name, title, type, unit, precision, progression, kind, enum keys and args. A text cell and a plan condition read them ([ADR 0111](../adr/0111-text-pages-from-plugin-values.md), [0120](../adr/0120-arguments-for-values.md), [0122](../adr/0122-hide-and-progress-a-unit.md), [0127](../adr/0127-plan-conditions-are-values.md)). The unit is the base unit the plugin answers in; the progression names the larger steps Paperboy may use. An `enum` value declares its allowed keys. Paperboy rejects an answer outside them. The declaration carries no labels; a text cell labels the keys it shows.
- **Config keys**, the **blob names** that Paperboy stores for it, and its own **version**.

An argument declares its type, default, whether it is required and its description. It can also declare an `enum` of allowed values with `enum_labels` for the display text ([ADR 0055](../adr/0055-allowed-arg-values-and-gallery-form.md)). The stored value is the key; an editor without a label shows the key. `plugin.Values.Check` rejects a value outside the declared set, so a typo in a page fails loudly instead of drawing something unexpected. Paperboy fills the `enum` of `paperboy/battery.device` and `paperboy/device-status.devices` with the configured device ids on every `/api/plugins` answer. An empty `enum_labels` key names the empty value for the editor. The built-in `Describe` stays enum-free, so a page that names a removed device still renders ([ADR 0123](../adr/0123-a-sample-device-in-previews.md)).

A widget that cannot draw anything in its cell reports `ErrTooSmall` with the cell size instead of leaving the cell empty. Paperboy draws its hatched notice with that reason, and every shipped plugin does this.

## The plugin host

`internal/widget` owns the processes ([ADR 0049](../adr/0049-plugin-widgets.md)).

- A plugin starts on first use. After an exit, after its `timeout` (default 30 seconds, killed) and after a change of its specification it is started again. A new process may be another program, so it drops the describe answer and the cached results of the plugin. Removed plugins drop their cached results.
- The cache is keyed by plugin, widget, arguments, size, gray levels, style, page, language, timezone and device. An entry lives until the plugin's `max_age` (default 5 minutes).
- A background loop every 5 seconds renders an entry again only when the plugin's demand is within 30 seconds. Otherwise the entry stays expired until a device asks; the plugin answers from memory, so no device waits. The loop also sends the schedule event when the demand changes by more than a minute. Entries idle for an hour are evicted, and the cache holds at most 1000 entries.
- A failing plugin serves its stale results and is not asked again for a minute.
- The built-in plugin runs in process and is never cached.
- Values get the same cache and the same `changed` event as widgets. A describe answer lives for five minutes, because it carries the declarations a text page uses to format a value. A cached value belongs to the declaration it was made with. A describe answer with different values drops the cached values of that plugin, so a unit never comes from an older program.

The values of a plan's conditions use the normal value cache. Paperboy reads them before it selects a page, outside the state lock, and omits a missing or failing answer, so the condition does not match. A text display that polls every second and a condition that names the same value share that entry.

## State and blobs

A plugin that needs to survive a restart sends its state with every answer ([ADR 0081](../adr/0081-plugin-state-over-the-protocol.md)). Paperboy writes it to `state/plugins/<id>/state.json` and the blobs to `state/plugins/<id>/blobs/<name>`, atomically and readable by the owner only. Paperboy reads them before `configure` and sends them once in the `state` request that follows. A plugin's background work starts after that request, so it sees the restored state.

The limits are 256 KiB per state, 1 MiB per blob and four blobs. A missing `state` means "unchanged" and `null` means "forget". An invalid name or an oversized value is logged and ignored; nothing is deleted automatically.

### What a plugin keeps

A plugin keeps state only for what a restart must not lose **and** cannot rebuild cheaply. All four conditions must hold:

- A restart would lose a quota, a session, a token or a long history.
- The restored value stays a cache that the plugin refreshes, not a live reading.
- A restored state can only make a page older, never wrong.
- The state is a cache bound to the config: it carries a version or a fingerprint, and a changed config discards it.

State must not hold live data. A restored departure view would claim trips that already left. A stateless plugin keeps nothing, because every input is an argument. The state must not hold a secret that the config does not already hold. A value that one cheap request rebuilds belongs in memory, not on disk. A blob is the exception: it holds what a fetch cannot rebuild.

Only [`dexcom`](../plugins/dexcom.md) and [`myskoda`](../plugins/myskoda.md) keep state today. `dexcom` keeps the session and the readings of the last day. The login and the readings are quota-limited, and the trend and the graph need the history. `myskoda` keeps the last vehicle fetch, its fetch times and the render blob. The API has a quota, and the render image is not cheap. No other shipped plugin keeps state, because one fetch or a short refresh rebuilds its data. A restart within the refresh interval therefore costs no request.

The readings of Dexcom in `state/` are acceptable: the operator owns the directory and can delete it. Paperboy exposes no age of a restored state, because `max_age` and the `changed` event set the refresh. No shared helper checks the config fingerprint; each plugin compares its own. An e-paper device polls every 5 to 10 minutes, so a restart rarely shows a gap on the page. That argues against broad state use.

## Versions

Every plugin has its own version in `cmd/<plugin>/VERSION`, embedded in the program ([ADR 0075](../adr/0075-plugin-versions.md)). The plugin zip names the programs `<plugin>-v<version>`. A change to a plugin raises the version in the same commit. The *Plugins* workflow fails a pull request that changes `cmd/<plugin>` without raising it.

`pkg/plugin` and `pkg/render` are part of every plugin binary, so a change there raises the version of every plugin ([ADR 0107](../adr/0107-plugin-builds-reach-installations.md)). Other shared `pkg/` code, `go.mod` and `go.sum` only warn.

`describe` reports the plugin's own version. When it differs from the version in the program's file name, Paperboy logs one warning per plugin.

## The built-in plugin

`internal/builtin` provides the plugin `paperboy`: `header`, `text`, `clock`, `logo`, `battery` and `device-status` for image displays ([ADR 0049](../adr/0049-plugin-widgets.md), [0095](../adr/0095-the-logo-on-pages-and-devices.md), [0115](../adr/0115-device-status-in-a-widget.md)). It draws from `pkg/render` and needs no data source, so it is always available and never cached. Its values `title`, `time`, `now`, `date`, `long` and `block` give a text page data without another plugin ([ADR 0111](../adr/0111-text-pages-from-plugin-values.md)). It is the only plugin that receives `server_status`; a page that shows its own device is cached per device.

## The shipped plugins

| Plugin | Data source | Widgets |
|---|---|---|
| [`calendar`](../plugins/calendar.md) | CalDAV, CardDAV, iCalendar links and files | `agenda`, `birthdays`, `month`, `week` |
| [`weather`](../plugins/weather.md) | Open-Meteo | `current`, `days`, `hours` |
| [`evcc`](../plugins/evcc.md) | evcc `/api/state` and its energy history | `site`, `flow`, `loadpoint`, `forecast`, `history`, `chart`, `values`, action `mode` |
| [`departures`](../plugins/departures.md) | EFA rapidJSON | `departures` |
| [`myskoda`](../plugins/myskoda.md) | MyŠkoda Public API | `battery`, `charging`, `profiles`, `location`, `climate`, `card`, `car`, actions `charging` and `climate` |
| [`dexcom`](../plugins/dexcom.md) | Dexcom Share | `glucose`, `graph`, `values`, value `state` with the keys `normal`, `low`, `high`, `urgent` |
| [`rss`](../plugins/rss.md) | RSS 2.0, RSS 1.0 and Atom | `feed`, `headline` |
| [`xkcd`](../plugins/xkcd.md) | xkcd | `comic` |
| [`countdown`](../plugins/countdown.md) | none, every date is an argument | `countdown` |
| [`pokedex`](../plugins/pokedex.md) | PokéAPI | `card` |

Every widget is for an image display. A plugin with data for a text page declares values instead ([ADR 0111](../adr/0111-text-pages-from-plugin-values.md)). The [plugin pages](../plugins/README.md) describe arguments, config keys, values, refresh rules and the data source of each one.

## Decisions

[ADR 0048](../adr/0048-calendar-provider-and-agenda.md) (calendar) · [0049](../adr/0049-plugin-widgets.md) (plugin widgets) · [0111](../adr/0111-text-pages-from-plugin-values.md) (values for text pages, amended by [ADR-0118](0118-text-boxes-and-overflow.md), arguments by [ADR-0120](0120-arguments-for-values.md), units by [ADR-0122](0122-hide-and-progress-a-unit.md), the text action removed by [ADR-0125](0125-remove-the-text-widget-path.md)) · [0053](../adr/0053-departures-from-efa.md), [0100](../adr/0100-one-stop-per-departures-widget.md) (departures) · [0055](../adr/0055-allowed-arg-values-and-gallery-form.md) (argument values) · [0057](../adr/0057-plugin-actions-from-the-plan.md) (actions) · [0058](../adr/0058-plugin-protocol-version-check.md) (protocol version check) · [0065](../adr/0065-plugin-state-directory.md) → [0081](../adr/0081-plugin-state-over-the-protocol.md) (state) · [0075](../adr/0075-plugin-versions.md), [0107](../adr/0107-plugin-builds-reach-installations.md) (a version per plugin) · [0099](../adr/0099-glucose-readings-from-the-dexcom-share-service.md) (Dexcom) · [0127](../adr/0127-plan-conditions-are-values.md) (conditions are values) · [0123](../adr/0123-a-sample-device-in-previews.md) (device ids as choices) · [0126](../adr/0126-a-schedule-event-for-plugin-demand.md) (schedule event)
