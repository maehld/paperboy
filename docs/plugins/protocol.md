# Plugin protocol

Paperboy and its plugins talk over standard input and standard output, one
JSON object per line ([ADR 0049](../adr/0049-plugin-widgets.md)). This document is the reference for the wire
format; the Go types live in [`pkg/plugin`](../../pkg/plugin/plugin.go).

- Current version: **7** ([ADR 0050](../adr/0050-physical-scaling-by-display-ppi.md) added version 3, [ADR 0057](../adr/0057-plugin-actions-from-the-plan.md) added
  actions, [ADR 0081](../adr/0081-plugin-state-over-the-protocol.md) added plugin state and blobs and made 5 the minimum,
  [ADR 0103](../adr/0103-plugin-conditions-in-plans.md) added conditions, [ADR 0111](../adr/0111-text-pages-from-plugin-values.md) added values).
  From version 5 on the protocol only grows additively. `server_status` is additive and keeps version 6
  ([ADR 0115](../adr/0115-device-status-in-a-widget.md)).
- Paperboy starts a plugin on first use and keeps it running.
- Paperboy writes one request line; the plugin writes exactly one answer line
  with the same `id`. Between answers the plugin may write events without an
  `id`.
- The plugin exits when standard input closes.
- Unknown request or answer fields must be ignored, so later additions stay
  compatible.

## Requests

Every request carries:

| Field | Meaning |
|---|---|
| `id` | Request identifier; the answer repeats it |
| `version` | Protocol version, currently `7` |
| `action` | `configure`, `state`, `describe`, `render`, `text`, `values`, `actions`, `action` or `conditions` |
| `language` | `"de"` or `"en"` |
| `timezone` | IANA name such as `Europe/Berlin` |
| `now` | Time of the request, RFC 3339 |
| `page` | `title`, `number` (1-based), `count`, display `width`/`height`, and `ppi` for image displays |
| `widgets` | For `render` and `text`: the widgets to draw |
| `values` | For `values`: the declared value keys the page needs |
| `actions` | For `actions` and `action`: the concrete actions |
| `device` | For `actions`, `action` and `values`: `id`, `name`, `hardware`; absent for a preview |
| `server_status` | For `render` and `text`, built-in plugin only: `current_device_status` and `selected_devices_status` (ADR 0115) |
| `levels` | For `render`: gray levels of the display, 1–256; the canvas falls back to 2, 4 or 16 tones |
| `config` | For `configure`: the plugin's section of `paperboy.yaml` |
| `state` | For `state`: the JSON object the plugin stored last time; missing when there is none ([ADR 0081](../adr/0081-plugin-state-over-the-protocol.md)) |
| `blobs` | For `state`: the stored blobs by name, base64 in JSON ([ADR 0081](../adr/0081-plugin-state-over-the-protocol.md)) |

### `configure`

The first request after the start, once. `config` is the plugin's section of
`paperboy.yaml` with `${VAR}` already expanded from the environment. A plugin
rejects unknown keys and missing required keys, usually with
`Values.Check`. An error answer stops the plugin; Paperboy reports the error in
the cells that use it.

```json
{"id":"1","version":5,"action":"configure","language":"de","timezone":"Europe/Berlin","now":"2026-11-07T12:00:00+01:00","config":{"lat":52.5,"lon":13.4}}
```

### `state`

Sent once, right after a successful `configure`. It carries `state` (the
stored object) and `blobs`
(the stored files), so the plugin has its data before the first widget
request. The answer may return `state` and `blobs` to store; an empty answer is
enough.

```json
{"id":"2","version":5,"action":"state","language":"de","timezone":"Europe/Berlin","now":"2026-11-07T12:00:00+01:00","state":{"fetched_at":"2026-11-07T11:00:00Z"},"blobs":{"render.png":"iVBORw0…"}}
```

### `describe`

Answers with `description`. It feeds `GET /api/plugins`, the Playground and
the plan's action validation.

```json
{
  "id": "2",
  "description": {
    "title": "evcc",
    "version": "1.2.0",
    "widgets": [
      {
        "widget": "site",
        "title": "Site",
        "kinds": ["image", "text"],
        "sizes": [{"columns": 3, "rows": 3}],
        "args": {"short": {"type": "bool", "default": false}}
      }
    ],
    "values": [
      {
        "name": "pv",
        "title": "PV power",
        "type": "number",
        "unit": "kW",
        "precision": 1
      }
    ],
    "actions": [
      {
        "name": "mode",
        "title": "Charge mode",
        "args": {"mode": {"type": "string", "required": true, "enum": ["now", "off"]}},
        "confirm": {"required": true, "text": "Switch the charge mode?"}
      }
    ],
    "config": {"url": {"type": "string", "required": true}},
    "conditions": [
      {"name": "low", "title": "Low", "description": "the reading is below the low threshold"}
    ],
    "blobs": ["render.png"]
  }
}
```

- `widgets[].kinds` is `image`, `text` or both.
- `values[]` declares the data a text page can lay out ([ADR 0111](../adr/0111-text-pages-from-plugin-values.md)): `name`, `title`, `type`, `unit`, `precision` and `kind`.
  `type` is `number`, `text`, `duration`, `timestamp` or `direction`. `precision` is the number of decimals Paperboy writes for a number.
  `kind` is `value` (default) or `list`; protocol 7 answers only single values. A value is data, not text: the plugin never pads or formats it.
- `version` is the plugin's own version, matching the `-v<version>` of its program name ([ADR 0075](../adr/0075-plugin-versions.md)). It is optional; Paperboy compares it with the file name and logs one warning when they differ.
- `widgets[].sizes` are spans of the 6 × 8 reference grid, hints for an editor.
- `args` use the same [argument schema](#arguments) as configuration keys.
- `actions[].name` is the action name without the plugin prefix. A plan
  references it as `plugin/name`.
- `confirm` is optional. `required: true` makes the device ask before running
  the action; `text` overrides Paperboy's generic confirmation text.
- A plugin without actions omits `actions` or sends an empty list.
- `conditions[].name` is the condition name without the plugin prefix. A plan
  depends on it as `plugin/name` ([ADR 0103](../adr/0103-plugin-conditions-in-plans.md)). The plugin decides what makes the
  condition active; thresholds and units stay in its configuration.
- `blobs` names the files the plugin keeps besides its state, such as a
  downloaded image ([ADR 0081](../adr/0081-plugin-state-over-the-protocol.md)). Paperboy logs files below `blobs/` that the
  plugin does not declare.

### `conditions`

Reports every declared condition and whether it is active now.

```json
{"id":"6","version":6,"action":"conditions","language":"de","timezone":"Europe/Berlin","now":"2026-11-07T12:00:00+01:00"}
```

```json
{"id":"6","conditions":{"urgent":false,"low":true,"high":false}}
```

- A missing name, an empty answer and an error answer all count as inactive.
  Paperboy never turns missing data into an alarm page.
- The plugin decides what "active" means; a stale value should be reported as
  inactive, because the page would otherwise show a warning about old data.
- Paperboy caches the answer for a few seconds and drops it on a `changed`
  event, so a plugin should send `changed` when a condition may have flipped.
- A plugin without conditions answers with an error; plans that name one of
  its conditions never match.

### `render` and `text`

`render` draws each widget on a canvas of exactly `width` × `height` pixels
with `levels` gray levels and returns a PNG. `text` fills each widget with
`width` columns and `height` lines and returns `lines`. A text page of values
no longer uses `text` ([ADR 0111](../adr/0111-text-pages-from-plugin-values.md)); the action stays in the protocol for
older plugins, and the built-in plugin still answers text widgets for the
widgets page. A later protocol version may remove it. Widgets get their
arguments in `args` and the page's look in `style`:

| `style` field | Meaning |
|---|---|
| `padding` | Inner margin in pixels |
| `rule` | Width of separator lines in pixels, at least 1 |
| `text_px` | Base text height in pixels |

Paperboy converts the page's points to pixels with the display's `ppi`
([ADR 0050](../adr/0050-physical-scaling-by-display-ppi.md)). Text displays get no `style` and no `ppi`; a widget sizes its text
from `lines`/`columns` instead.

A text widget in a cell with `scroll: true` gets `scroll: true` and may then
answer lines longer than `width`: Paperboy shows the first `width` columns and
the display scrolls the rest ([ADR 0092](../adr/0092-scrolling-text-lines.md)). Without the field a line is cut at
`width`, which is what every widget did before; a plugin that ignores the flag
simply keeps its lines that fit.

```json
{"id":"3","version":5,"action":"render","levels":4,"page":{"title":"Overview","number":1,"count":3,"width":800,"height":480,"ppi":125},"widgets":[{"widget":"site","width":400,"height":240,"args":{"short":true},"style":{"padding":8,"rule":1,"text_px":28}}]}
```

```json
{"id":"3","widgets":[{"png":"…base64…"}],"max_age":300}
```

An answer carries one result per widget, in the request's order. A widget that
fails sets `error`; the other widgets still draw. Paperboy draws the error as a
hatched notice in the widget's cell, so a widget must report a cell it cannot
draw in instead of leaving it blank: `plugin.TooSmall(w)` wraps
`plugin.ErrTooSmall` with the cell's pixel size and reads
`cell too small: 800x180`. `max_age` (seconds) tells Paperboy how long it may
reuse the results; `0` means its default of five minutes.

### `values`

Answers the values of a text page ([ADR 0111](../adr/0111-text-pages-from-plugin-values.md)). One request covers every key of the page that belongs to this plugin. It carries the usual `page`, optional `device`, `language`, `timezone` and `now`.

```json
{"id":"7","version":7,"action":"values","language":"de","timezone":"Europe/Berlin","now":"2026-11-15T12:00:00+01:00","device":{"id":"kueche"},"page":{"title":"Werte","number":1,"count":1,"width":20,"height":4},"values":["pv","home"]}
```

```json
{"id":"7","version":7,"values":{"pv":{"number":4.2},"home":{"number":1.5}},"max_age":60}
```

- The answer is a map from the requested name to one value. Exactly one field carries the value:
  `{"number": 12.5}`, `{"text": "…"}`, `{"duration": 3600}` in seconds, `{"timestamp": "2026-11-15T12:00:00+01:00"}` in RFC 3339, or `{"direction": 1}` with 1 up, 0 flat and -1 down.
- A missing name is an error for that cell; Paperboy shows a `? …` cell. The other values still reach the page.
- `max_age` works like for widgets: it tells Paperboy how long it may reuse the answer, and a `changed` event drops it.
- A plugin that does not declare a requested name is not asked; the cell shows the error instead.
- Paperboy formats the value: a number with the declared precision and the global language, a duration, a timestamp or a direction. The plugin decides neither width nor alignment.
- A text value may carry several lines, one per record, such as an agenda or a list of departures. Paperboy shows them line by line; the label opens the first line and the unit closes the last. List values with named fields arrive in a later version.

### `server_status`

`server_status` is Paperboy's privileged view of its devices (ADR 0115). Only
the built-in plugin receives it; every other plugin gets no field, because its
cached widgets would show stale rows. The object carries no key, no MAC and no
address.

```json
{
  "server_status": {
    "current_device_status": {
      "id": "kueche",
      "display": "sticky",
      "plan": "standard",
      "status": "ok",
      "last_seen": "2026-11-15T09:04:30+01:00",
      "seen_s": 30,
      "power": "battery",
      "battery": {"volts": 3.75, "percent": 84, "charging": false},
      "runtime_s": 1036800,
      "runtime_span_s": 172800
    },
    "selected_devices_status": {
      "wohnzimmer": {"id": "wohnzimmer", "status": "late", "seen_s": 7440}
    }
  }
}
```

- `current_device_status` is the device the page is rendered for; it is
  missing in a preview without a device.
- `selected_devices_status` is a map from id to status. It holds only the
  devices a page names in its widget arguments, not every configured device.
  An id that no device file has appears with `status: "unknown"`.
- `status` is `ok`, `late`, `never`, `unregistered` or `unknown`.
- `battery` carries `volts`, `percent`, `mah`, `ma`, `charging` and `level`
  (`warn` or `critical`). `runtime_s` is the estimated battery runtime; `0`
  means no estimate or a mains device.

### `actions`

Reports the current state of the concrete actions, one answer per call, in the
request's order.

```json
{"id":"4","version":5,"action":"actions","language":"de","timezone":"Europe/Berlin","now":"2026-11-07T12:00:00+01:00","device":{"id":"kueche","name":"Kitchen","hardware":"sticky"},"page":{"title":"Overview","number":1,"count":3,"width":800,"height":480,"ppi":125},"actions":[{"action":"mode","args":{"mode":"now"}},{"action":"refresh"}]}
```

```json
{"id":"4","actions":[{"title":"Charge now"},{"title":"Refresh","disabled":"evcc is offline"}],"max_age":60}
```

- `title` overrides the spec's title for this instance.
- `disabled` is the reason why the action cannot run now; empty means
  available. A disabled action stays visible in the menu, hatched, instead of
  disappearing.
- `confirm` overrides the spec's confirmation for this instance.
- The plugin validates `args` itself against the spec; a bad value becomes a
  `disabled` reason instead of a protocol error. `Values.Check` does both the
  unknown-key and the enum check.
- The number of states must match the number of calls. A mismatch or an error
  answer disables all actions of that plugin until the next successful answer.

### `action`

Runs exactly one concrete action. Paperboy sends one `actions` entry.

```json
{"id":"5","version":5,"action":"action","language":"de","timezone":"Europe/Berlin","now":"2026-11-07T12:00:00+01:00","device":{"id":"kueche","hardware":"sticky"},"page":{"title":"Overview","number":1,"count":3,"width":800,"height":480,"ppi":125},"actions":[{"action":"mode","args":{"mode":"now"}}]}
```

```json
{"id":"5","result":{"ok":true,"message":"Charging now","changed":true}}
```

- `ok: false` with a `message` means the action ran but failed; Paperboy shows
  the message and keeps the menu open.
- A protocol or plugin failure is an `error` answer instead.
- `changed: true` tells Paperboy to drop the plugin's cached widgets before the
  next render.
- Actions should set a target state (`mode: now`), never toggle, so a retried
  request is safe.

## Answers and events

An answer carries `id`, its `version` and optionally `error`, plus the payload
for its request: `widgets`, `values`, `actions`, `result`, `conditions`, `description`
or `max_age`.
`Handle` fills `version` with the protocol version the plugin was built
against, in every answer and in the `changed` event. Paperboy calls
`Message.CheckVersion` on every answer and refuses the plugin when the version
is missing or older than 5 ([ADR 0058](../adr/0058-plugin-protocol-version-check.md), [ADR 0081](../adr/0081-plugin-state-over-the-protocol.md)); newer versions are accepted,
because the protocol only grows additively. The cells show the error
and the plugin is dropped with one log line. Plugins written by hand in another
language must print the field themselves.

An answer may also carry `state` and `blobs` to store (see [state between
restarts](#state-between-restarts)).

An event has no `id`:

```json
{"event":"changed","changed":["agenda"]}
```

`changed` invalidates the named widgets, or all widgets of the plugin when the
list is empty or missing. Plugins must send it when action-relevant state
changes; for widgets with actions, a threshold on a number is not enough.

An event may carry `state` and `blobs` as well, and a line with only `state`
stores it without touching the widget cache.

## Arguments

`Arg` describes one widget argument, action argument or configuration key:

| Field | Meaning |
|---|---|
| `type` | `string`, `int`, `float`, `bool`, `duration` or `list` |
| `default` | Default value |
| `required` | The key must be set |
| `description` | Help text for an editor |
| `enum` | Allowed values, or allowed elements of a list ([ADR 0055](../adr/0055-allowed-arg-values-and-gallery-form.md)) |
| `enum_labels` | Display text per enum value, keyed by `fmt.Sprint` of the value; the stored value stays the key |

`Values.Check(known)` rejects unknown keys, missing required keys and values
outside `enum`. `Values.CheckEnums(known)` only checks the enums and leaves
unknown keys alone; `Handle` applies it per widget before calling `Render` or
`Text`. `describe` is answered on demand, so a plugin may compute `enum` from
the data it has loaded; the calendar plugin offers the discovered calendar
names as check boxes. Paperboy validates against the answer current at that
moment.

## State between restarts

A plugin process ends when it exits, when a request times out, and when its
configuration changes; Paperboy itself restarts on every deployment. Everything
a plugin keeps in memory is lost then, and Paperboy's widget cache is no help
because it holds drawn results, not data.

State is part of the protocol ([ADR 0081](../adr/0081-plugin-state-over-the-protocol.md)). Paperboy keeps it, a protocol 5
plugin never writes a file. Paperboy reads the stored state before `configure`,
so an answer to `configure` cannot replace it. A plugin starts its background
work after the `state` request and sees the restored state there.

- **`state`** is a JSON object of at most 256 KiB. The `state` request carries
  the stored one; every answer and event may send a new one. A missing `state`
  field means unchanged, `state: null` forgets it. A plugin returns the whole
  object, not a diff.
- **`blobs`** are larger values, such as a downloaded image. `describe`
  declares their names, the `state` request carries the stored ones
  base64-coded, and any answer or event may write changed ones. A blob is at
  most 1 MiB; a plugin keeps at most four.

Paperboy stores them in the plugin's directory, `<instance>/state/plugins/<id>/`:
`state.json` and `blobs/<name>`, both readable by the owner only. A file a
plugin no longer declares stays until the operator removes it; deleting the
directory resets the plugin.

A plugin never writes these files; `PAPERBOY_STATE_DIR` and
`pkg/plugin.StateDir()` are gone ([ADR 0081](../adr/0081-plugin-state-over-the-protocol.md)).

Keep no secrets in the state (a fingerprint of a key is enough), version the
object so a later plugin can discard a format it does not know, and treat the
blobs as a cache that can be rebuilt.

## Go helper

A Go plugin implements `plugin.Handler`:

```go
type Handler interface {
    Describe() Description
    Configure(cfg Values, req *Request) error
    Render(req *Request, w WidgetRequest, canvas *image.Paletted) error
}
```

Optional interfaces:

| Interface | Purpose |
|---|---|
| `Texter` | `Text(req, widget) ([]string, error)` for text displays |
| `Valuer` | `Values(req) (map[string]Value, error)` for the declared values of a text page |
| `Preparer` | `Prepare(req) error` once per request, before its widgets |
| `Starter` | `Start(ctx, notify)` for background work; `notify` sends `changed` |
| `Ager` | `MaxAge(req) time.Duration` for widget and action caching |
| `Actioner` | `Actions(req) ([]ActionState, error)` and `Invoke(req, call) (ActionOutcome, error)` |
| `Stater` | `LoadState(state, blobs)` when the `state` request arrives and `State() json.RawMessage`: the object to store, nil when unchanged |
| `Blober` | `Blobs() map[string][]byte`: changed blobs, nil when none; clear the marks |

A plugin calls `plugin.Serve(handler)` and draws with `pkg/render`, so its
widgets use the same fonts, gray levels and scalings as Paperboy's own.

## Version history

| Version | Change |
|---|---|
| 3 | `page.ppi` and `style.text_px` in pixels; `style.text_size` removed ([ADR 0050](../adr/0050-physical-scaling-by-display-ppi.md)) |
| 4 | Actions: `describe.actions`, `actions`, `action`, `actions`/`result` answers, `device` ([ADR 0057](../adr/0057-plugin-actions-from-the-plan.md)) |
| 5 | Plugin state and blobs: the `state` request, `answer.state`/`answer.blobs`, `describe.blobs`; `PAPERBOY_STATE_DIR` removed ([ADR 0081](../adr/0081-plugin-state-over-the-protocol.md)) |
| 6 | Conditions: `describe.conditions`, the `conditions` request and `answer.conditions` ([ADR 0103](../adr/0103-plugin-conditions-in-plans.md)) |
| 7 | Values: `describe.values`, the `values` request and `answer.values` ([ADR 0111](../adr/0111-text-pages-from-plugin-values.md)) |

A plugin is used as a whole or not at all ([ADR 0058](../adr/0058-plugin-protocol-version-check.md)). The check is a floor,
not equality: Paperboy requires at least version 5 and accepts newer ones,
because the protocol only grows additively – unknown fields are ignored,
existing fields keep their meaning, and Paperboy gates an optional feature on
the version a plugin reports. That version is the only thing a plugin has to
announce; there is no separate capability list.
