# 0120: Arguments for values

- Status: Accepted
- Date: 2026-11-15
- Related: ADR 0055 (allowed arg values), ADR 0111 (text pages from plugin values), ADR 0118 (text boxes and overflow), issue #264

## Context

ADR 0111 gave text pages declared values. A values request named the keys of one page, and the plugin answered data for them. It carried no arguments, because the first migration had no use for them: a page named the value, and the plugin took its defaults.

The shipped plugins made the gap visible:

- `departures` describes the first configured stop only. A page cannot show a second stop.
- `evcc` describes the first loadpoint only.
- `calendar` cannot narrow the agenda to calendars or a shorter window.
- `pokedex` always shows the default seed.
- `countdown` declares no values at all. A countdown is entirely arguments, so a text page cannot show one.

A widget reads `args` since ADR 0049. A value is the same kind of request to the same plugin, so it should read arguments the same way.

## Decision

1. **A value declares its arguments.** `describe.values` gains `args` per value, with the shape of `WidgetInfo.args` (ADR 0055): type, default, required, description, enum and labels. An empty map means the value reads none.
2. **A values request carries one argument set.** The request keeps `values` as the list of names and adds `value_args` with the arguments of that set. The answer stays a map from name to value. Both belong to [protocol version 7](../design/05-plugins.md), which no release carries yet (ADR 0111): the values protocol gains the field and `values[].args` before it ships, so `values` keeps its shape and a plugin that ignores `value_args` still answers without arguments.
3. **Paperboy groups by plugin and argument set.** Two boxes that read the same value with different arguments are two requests. The cache key holds the arguments like it holds a widget's. A values request still never runs under the state lock (ADR 0111).
4. **A text box carries `args`.** It is a value cell's key next to `label`, `unit`, `align` and `overflow`. A literal text cell rejects `args` with a hint, as it rejects `label` and `unit` (ADR 0118).
5. **The plugin checks its arguments.** The same `plugin.Values.Check` that guards a widget's arguments rejects a typo or a value outside an enum.
6. **The editor edits them.** The text inspector shows the argument controls of the declared value, with the same forms as the widget inspector.
7. **The plugins use them where they help.** `departures` takes `stop` and the format arguments, `evcc` the loadpoint `index`, `calendar` the window and calendar filters, `pokedex` the `seed`, `rss` the `feed`, `weather` the forecast `days`, and `countdown` gains values for `date` and `since`; its label stays the cell's.

## Alternatives

- **One argument set per value, in the list.** `values` becomes a list of `{name, args}`. The answer is keyed by name, so two entries with the same name and different arguments collide; a request would still need one set. The extra nesting buys nothing.
- **Arguments on the value declaration only.** A page could not pass its own dates, stops or seeds; the plugin would need one value per possible argument.
- **A widget-like `values` action with `widgets`.** The names and the arguments would travel in the widget list, but the answer, the cache and the editor would then treat a value like a drawn widget, which it is not.
- **Keep text widgets for countdown and the argument-driven plugins.** ADR 0111 removes them; a second path would stay forever.
- **Paperboy builds the arguments.** It does not know what a plugin reads; a generic key/value box in the page would duplicate the argument schema of every plugin.

## Consequences

- `pkg/plugin` grows, so every plugin binary changes and raises its version (ADR 0075, 0107).
- A page may now show two stops, two loadpoints or several countdowns on one text display.
- The editor and the page schema need the extra key; a value without arguments behaves as before.
- The cache holds one entry per argument set, so a page with many argument sets costs more requests. The page owns that number.
- List values (`for-each`) stay deferred (ADR 0111).