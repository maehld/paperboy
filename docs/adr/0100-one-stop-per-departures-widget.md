# 0100: One stop per departures widget

- Status: Accepted, implemented
- Date: 2026-11-13
- Builds on: ADR 0053 (departures plugin)
- Answers: issue #226

## Context

ADR 0053 gave the departures plugin one `stop`. A page that should show two
stops therefore needed two plugin entries - two processes, two copies of the
poll loop, and the same filters configured twice. The stop also had to be
chosen when the plugin was configured, although it is a property of the
widget: the same page may show the stop next door on one device and the main
station on another.

`directions` matches the destination on the client side because the EFA
departure monitor has no direction to filter by. Its `stopEvents` carry
`transportation.destination.name` (the headsign) and nothing else that names a
direction; `XML_DM_REQUEST`'s `line` parameter can narrow a direction only
through DIVA line IDs (`subnet:line:supplement:direction:project`), which an
operator cannot know. So the destination stays the only handle on a
direction.

## Decision

### 1. The config lists the stops, the widget picks one

`stops` replaces `stop`. Every entry is `{name, stop}`: `stop` is a stop name
or ID as before, `name` is what a widget selects. A plain string entry uses
itself as the name. Names must be unique. A leftover `stop` is rejected with a
hint, because a config with both would be ambiguous.

The widget gets the arg `stop` with the configured names as its enum; the
first configured stop is the default, so pages without the arg keep working.
There is no "all": unlike the RSS feeds, mixing departures of two stops gives
a list whose rows cannot be told apart reliably.

### 2. Every stop has its own poll, name and error

The plugin sends one departure monitor request per stop and refresh, in
parallel, and remembers the stop ID from the first stop finder answer per stop.
A failing stop keeps its last departures and its own error, which only its
widgets show as the cell notice; the other stops keep updating and the change
event still fires for them.

`lines`, `directions`, `max`, `walk_minutes` and `refresh` apply to every stop.
Per-stop filters can follow when an installation needs them.

The stop's own name (EFA's `name`, `disassembledName` and `parent` locality)
drives the header and the `locality`/`locality_order` rules per widget, so two
widgets of the same page show the name and locality of their own stop.

## Consequences

- One plugin process serves all stops of an instance; a page may show several
  stops side by side.
- A refresh costs one request per stop. The minimum interval stays 30 s, and
  the requests run in parallel, so the load is unchanged per stop.
- Existing configs need `stop` rewritten as `stops: [{name: ..., stop: ...}]`
  (or `stops: ["<stop>"]`). There is no migration code.
- The direction filter stays a client-side match on the destination.
