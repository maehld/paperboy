[← Paperboy](../../README.md)

# Plugins

A plugin is a long-running program that provides widgets (ADR 0049). Paperboy starts it on first use and sends its `config` section once (`configure`). Then it asks for widgets. Paperboy keeps the results until the `max_age` of the plugin (default 5 minutes) or until the plugin reports a change. It renders widgets that are still in use again in the background, so a device never waits for a plugin. When a plugin exits or does not answer within `timeout`, Paperboy restarts it. Until then, the last widgets appear as stale. A failing plugin is not asked again for a minute.

**Protocol** ([`docs/plugins/protocol.md`](protocol.md), `pkg/plugin`): one JSON object per line on stdin, one answer per line on stdout with the same `id`. Actions are `configure`, `state`, `describe`, `render` (`levels` 2 or 4, `widgets: [{widget, width, height, args, style}]` → base64 PNGs), `text` (the same in characters → `lines`; a cell with `scroll: true` asks for wider lines, ADR 0092), `actions` (the state of concrete plan actions) and `action` (run one). Every request carries `language`, `timezone`, `now` and the `page` (`title`, `number`, `count`, display `width`/`height`, and for image displays `ppi`).

**Protocol version 5** (ADR 0050, 0057, 0081): each widget's `style` holds `padding`, `rule` and `text_px` in pixels, already converted from the points of the page. Go plugins use `Request.TextPx`, `PtPx` and `MMPx` for physical sizes. `describe` lists widgets and, optionally, actions, plus the plugin's own `version` (ADR 0075). An action request names a concrete `action` and its `args` and gets `actions` states (title, disabled reason, confirmation) or a `result` (`ok`, `message`, `changed`). Between answers a plugin may print `{"event": "changed", "changed": ["agenda"]}`.

Right after `configure`, a protocol 5 plugin gets one `state` request with the stored JSON and its declared `blobs` (base64). Every answer may send new values back: missing = unchanged, `null` = forget the state. A plugin declares large values such as an image as `blobs` in `describe` (ADR 0081). Paperboy writes `state/plugins/<id>/state.json` and `state/plugins/<id>/blobs/<name>` itself. A plugin keeps no files itself; Paperboy sets no `PAPERBOY_STATE_DIR`. It ends when stdin closes. Every plugin also sees `PAPERBOY_LANGUAGE` and `PAPERBOY_TIMEZONE` in its environment.

Every answer names the protocol version the plugin was built against (`"version": 5`). Paperboy requires at least 5 and accepts newer versions, because the protocol only grows additively. It refuses a plugin below that or without a version. A mismatch appears as the error of the cell and as one log line (ADR 0058).

A Go plugin implements `plugin.Handler` (`Describe`, `Configure`, `Render`). It can also implement `Texter`, `Preparer`, `Starter` (background work with a `notify` callback), `Ager` (`max_age`), `Actioner` (actions for the device menu) and `Stater`/`Blober` (state and files Paperboy keeps between restarts). It calls `plugin.Serve` and draws with `pkg/render`: the fonts, gray levels, fills and icon masks Paperboy itself uses, so its widgets look like the rest of the page. Plugins in other languages speak the same JSON lines.

Paperboy ships the plugins below. One zip per platform (`paperboy-plugins-linux-amd64.zip`, `paperboy-plugins-windows-amd64.zip`) contains all of them. Unpacked in the instance directory, the zip creates `plugins/` with `<plugin>-v<version>`. Each plugin raises its version only when it changes (ADR 0075); the version is in `cmd/<plugin>/VERSION`. Every plugin has its own documentation under `docs/plugins/`:

- [`evcc`](evcc.md) – PV, home, grid, battery, energy flow, loadpoints, forecast, the energy history of the day, week, month and year as a table or a bar chart, and evcc's values and charging statistics from [evcc](https://evcc.io); the first shipped plugin with actions, `evcc/mode` switches a charge mode from the device menu.
- [`departures`](departures.md) – real-time public transport departures from an EFA installation.
- [`myskoda`](myskoda.md) – battery, charging plans, parking position and the vehicle image from the [MyŠkoda Public API](https://public.api.connect.skoda-auto.cz/docs).
- [`weather`](weather.md) – current weather and forecast from [Open-Meteo](https://open-meteo.com/).
- [`calendar`](calendar.md) – agenda, week, month and birthdays from CalDAV and iCalendar.
- [`pokedex`](pokedex.md) – a Pokémon card from [PokéAPI](https://pokeapi.co).
- [`xkcd`](xkcd.md) – [xkcd](https://xkcd.com) comics.
- [`countdown`](countdown.md) – days to a date, optionally with a progress bar.
- [`rss`](rss.md) – RSS and Atom feeds as a list or as a single headline.
- [`paperboy`](paperboy.md) – built in, no zip needed: header, text and clock.
