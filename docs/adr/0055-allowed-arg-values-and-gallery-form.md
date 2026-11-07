# 0055: Allowed argument values and the gallery form

- Status: Accepted
- Date: 2026-11-07
- Builds on: ADR 0040 (operator dashboard), ADR 0049 (plugins), ADR 0050 (protocol version 3)

## Context

Issue #32 asks to try a widget's arguments in the widget gallery
(`/ui/plugins`) and copy the result into a page. `describe` already declares
every argument with `type`, `default` and `description`, and
`GET /api/widget/{plugin}/{widget}` already takes `arg.<name>`. Many string
and number arguments only accept a fixed set of values (`align`, clock
`format`, departures `time`, calendar week `start`, evcc site `columns`), but
the schema could not say so: an editor had to offer a free text field, and a
typo silently fell back to the default.

## Decision

- `plugin.Arg` gets an optional `enum`: the allowed values, for a `list` the
  allowed elements. It is additive and optional, so the protocol version
  stays 3: older plugins without `enum` keep working, and Paperboy treats a
  missing `enum` as "any value".
- The Go helper enforces it: `Values.Check` (configuration) rejects values
  outside the `enum`, and `Handle` checks each widget's args against the
  widget's `describe` before rendering, answering `"<arg>: <value> is not one
  of …"` as that widget's error. Values are compared by their text, so `2`
  from YAML and `2.0` from JSON match. Plugins in other languages may do the
  same; Paperboy itself does not validate args.
- All shipped plugins and the built-in plugin fill `enum` wherever an
  argument or configuration key has a fixed set of values.
- The gallery shows a GET form per widget, generated from the schema, without
  JavaScript (ADR 0040): `enum` → select, `bool` → select default/true/false
  (so "unset" stays possible), `int`/`float`/`duration`/`string` → text input
  with the default as placeholder, `list` → comma-separated text; optionally a
  span from the declared sizes. Submitting loads
  `/ui/plugins?display=…&widget=<plugin>/<widget>&arg.<name>=…#<plugin>-<widget>`;
  only that widget's previews get the args, typed by the schema (strings stay
  strings). Invalid values show as the widget's notice. Below the previews a
  copyable cell `{ at: [0, 0], span: [c, r], widget: …, args: { … } }` holds
  only the args that differ from their default.

## Consequences

- A typo in a fixed-value argument is now an error in the cell instead of a
  silent default; existing pages with an invalid value show a notice after
  the update.
- Editors (the planned page editor on `GET /api/plugins`) can offer choices
  from the same schema.
- Plugins built against an older `pkg/plugin` keep working but do not
  validate enums until rebuilt.
