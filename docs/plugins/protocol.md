# Plugin protocol

Paperboy and its plugins talk over standard input and standard output, one
JSON object per line (ADR 0049). This document is the reference for the wire
format; the Go types live in [`pkg/plugin`](../../pkg/plugin/plugin.go).

- Current version: **4** (ADR 0050 added version 3, ADR 0057 added actions).
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
| `version` | Protocol version, currently `4` |
| `action` | `configure`, `describe`, `render`, `text`, `actions` or `action` |
| `language` | `"de"` or `"en"` |
| `timezone` | IANA name such as `Europe/Berlin` |
| `now` | Time of the request, RFC 3339 |
| `page` | `title`, `number` (1-based), `count`, display `width`/`height`, and `ppi` for image displays |
| `widgets` | For `render` and `text`: the widgets to draw |
| `actions` | For `actions` and `action`: the concrete actions |
| `device` | For `actions` and `action`: `id`, `name`, `hardware` |
| `levels` | For `render`: 2 or 4 gray levels |
| `config` | For `configure`: the plugin's section of `paperboy.yaml` |

### `configure`

The first request after the start, once. `config` is the plugin's section of
`paperboy.yaml` with `${VAR}` already expanded from the environment. A plugin
rejects unknown keys and missing required keys, usually with
`Values.Check`. An error answer stops the plugin; Paperboy reports the error in
the cells that use it.

```json
{"id":"1","version":4,"action":"configure","language":"de","timezone":"Europe/Berlin","now":"2026-11-07T12:00:00+01:00","config":{"lat":52.5,"lon":13.4}}
```

### `describe`

Answers with `description`. It feeds `GET /api/plugins`, the widget gallery and
the plan's action validation.

```json
{
  "id": "2",
  "description": {
    "title": "evcc",
    "widgets": [
      {
        "widget": "site",
        "title": "Site",
        "kinds": ["image", "text"],
        "sizes": [{"columns": 3, "rows": 3}],
        "args": {"short": {"type": "bool", "default": false}}
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
    "config": {"url": {"type": "string", "required": true}}
  }
}
```

- `widgets[].kinds` is `image`, `text` or both.
- `widgets[].sizes` are spans of the 6 × 8 reference grid, hints for an editor.
- `args` use the same [argument schema](#arguments) as configuration keys.
- `actions[].name` is the action name without the plugin prefix. A plan
  references it as `plugin/name`.
- `confirm` is optional. `required: true` makes the device ask before running
  the action; `text` overrides Paperboy's generic confirmation text.
- A plugin without actions omits `actions` or sends an empty list.

### `render` and `text`

`render` draws each widget on a canvas of exactly `width` × `height` pixels
with `levels` gray levels and returns a PNG. `text` fills each widget with
`width` columns and `height` lines and returns `lines`. Widgets get their
arguments in `args` and the page's look in `style`:

| `style` field | Meaning |
|---|---|
| `padding` | Inner margin in pixels |
| `rule` | Width of separator lines in pixels, at least 1 |
| `text_px` | Base text height in pixels |

Paperboy converts the page's points to pixels with the display's `ppi`
(ADR 0050). Text displays get no `style` and no `ppi`; a widget sizes its text
from `lines`/`columns` instead.

```json
{"id":"3","version":4,"action":"render","levels":4,"page":{"title":"Overview","number":1,"count":3,"width":800,"height":480,"ppi":125},"widgets":[{"widget":"site","width":400,"height":240,"args":{"short":true},"style":{"padding":8,"rule":1,"text_px":28}}]}
```

```json
{"id":"3","widgets":[{"png":"…base64…"}],"max_age":300}
```

An answer carries one result per widget, in the request's order. A widget that
fails sets `error`; the other widgets still draw. `max_age` (seconds) tells
Paperboy how long it may reuse the results; `0` means its default of five
minutes.

### `actions`

Reports the current state of the concrete actions, one answer per call, in the
request's order.

```json
{"id":"4","version":4,"action":"actions","language":"de","timezone":"Europe/Berlin","now":"2026-11-07T12:00:00+01:00","device":{"id":"kueche","name":"Kitchen","hardware":"sticky"},"page":{"title":"Overview","number":1,"count":3,"width":800,"height":480,"ppi":125},"actions":[{"action":"mode","args":{"mode":"now"}},{"action":"refresh"}]}
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
{"id":"5","version":4,"action":"action","language":"de","timezone":"Europe/Berlin","now":"2026-11-07T12:00:00+01:00","device":{"id":"kueche","hardware":"sticky"},"page":{"title":"Overview","number":1,"count":3,"width":800,"height":480,"ppi":125},"actions":[{"action":"mode","args":{"mode":"now"}}]}
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
for its request: `widgets`, `actions`, `result`, `description` or `max_age`.
`Handle` fills `version` with the protocol version the plugin was built
against, in every answer and in the `changed` event. Paperboy calls
`Message.CheckVersion` on every answer and refuses the plugin when the version
differs or is missing (ADR 0058): the cells show the error and the plugin is
dropped with one log line. Plugins written by hand in another language must
print the field themselves.

An event has no `id`:

```json
{"event":"changed","changed":["agenda"]}
```

`changed` invalidates the named widgets, or all widgets of the plugin when the
list is empty or missing. Plugins must send it when action-relevant state
changes; for widgets with actions, a threshold on a number is not enough.

## Arguments

`Arg` describes one widget argument, action argument or configuration key:

| Field | Meaning |
|---|---|
| `type` | `string`, `int`, `float`, `bool`, `duration` or `list` |
| `default` | Default value |
| `required` | The key must be set |
| `description` | Help text for an editor |
| `enum` | Allowed values, or allowed elements of a list (ADR 0055) |

`Values.Check(known)` rejects unknown keys, missing required keys and values
outside `enum`. `Values.CheckEnums(known)` only checks the enums and leaves
unknown keys alone; `Handle` applies it per widget before calling `Render` or
`Text`.

## State between restarts

A plugin process ends when it exits, when a request times out, and when its
configuration changes; Paperboy itself restarts on every deployment. Everything
a plugin keeps in memory is lost then, and Paperboy's widget cache is no help
because it holds drawn results, not data.

Paperboy therefore starts every plugin with `PAPERBOY_STATE_DIR`, the absolute
path of `<instance>/state/plugins/<plugin>/` (ADR 0065). The directory exists
before `configure`, and an `env:` key of the same name in the plugin's section
cannot override it.

The files inside belong to the plugin; Paperboy never reads, writes or deletes
them, and a plugin that is no longer configured leaves its directory behind.
A plugin keeps what lets it skip work after a restart — when it loaded last, a
wait a server asked for, a downloaded picture — under these rules: no secrets
(a fingerprint of a key is enough), a version number in the file, atomic writes
(temporary file, then rename), and a file that does not fit is ignored with one
log line instead of failing the plugin.

`plugin.StateDir()` returns the path in Go; it is `""` for the built-in plugin
and in tests.

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
| `Preparer` | `Prepare(req) error` once per request, before its widgets |
| `Starter` | `Start(ctx, notify)` for background work; `notify` sends `changed` |
| `Ager` | `MaxAge(req) time.Duration` for widget and action caching |
| `Actioner` | `Actions(req) ([]ActionState, error)` and `Invoke(req, call) (ActionOutcome, error)` |

A plugin calls `plugin.Serve(handler)` and draws with `pkg/render`, so its
widgets use the same fonts, gray levels and scalings as Paperboy's own.

## Version history

| Version | Change |
|---|---|
| 3 | `page.ppi` and `style.text_px` in pixels; `style.text_size` removed (ADR 0050) |
| 4 | Actions: `describe.actions`, `actions`, `action`, `actions`/`result` answers, `device` (ADR 0057) |

There is no compatibility layer. The version is a hard check (ADR 0058), so a
plugin that reports another version is refused as a whole, not used without
its actions: after a protocol bump every plugin has to be rebuilt, and they
ship with Paperboy in the same release.
