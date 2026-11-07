# 0053: Real-time departures from an EFA installation

- Status: Accepted
- Date: 2026-11-07
- Builds on: ADR 0049 (plugins), ADR 0050 (physical scaling)

## Context

Issue #11 asks for a plugin that shows real-time public transport departures
for Lippe Mobil (OWL) with a configurable stop. The
data source must be public, give real-time data and ideally work for other
regions too.

## Research (2026-11-07)

- The regional journey planner of Westfalen/NWL (Lippe Mobil, go.on, ...) is an
  EFA installation (MENTZ), reachable at
  `https://www.westfalenfahrplan.de/nwl-efa/`. It speaks the EFA JSON
  interface (`outputFormat=rapidJSON`) and needs no API key.
- `XML_STOPFINDER_REQUEST?type_sf=any&name_sf=<stop name>` resolves a stop
  name uniquely to a global stop ID such as `de:00000:1001`; a real stop in
  the region was checked and returns real-time data.
- `XML_DM_REQUEST?type_dm=stop&name_dm=<stop ID>&mode=direct&useRealtime=1`
  returns `stopEvents` with `departureTimePlanned`,
  `departureTimeEstimated` (real time, present when the trip is monitored),
  `realtimeStatus` (`MONITORED`, `DEVIATION`, `TRIP_CANCELLED`, ...) and
  `transportation` (line number, destination). Times are UTC with a `Z`.
  The fixtures in `cmd/departures/testdata` are recorded answers with names,
  IDs and coordinates replaced by placeholders; the cancellation fixture is
  derived by editing, because no cancelled trip was running at the time of
  recording.
- Terms of use: the NWL endpoint publishes neither terms nor rate limits. The
  related VRR OpenService allows the interface for research, test, development
  and hobby use and asks other users to contact the operator; community
  guidance for EFA test servers is not to poll faster than every 30 s.
- Alternatives rejected: the DELFI/HAFAS endpoints need an access key or a
  contract, TRIAS needs a customer-specific access, and scraping the
  Westfalenfahrplan website is the same service without the stable interface.

## Decision

- A generic plugin `cmd/departures` with a `backend` option; the only backend
  is `efa`, with a configurable `base` URL (default Westfalenfahrplan), so any
  EFA installation with rapidJSON works (VRR, NVBW, VVS, ...). The stop has no default and is required. A stop is a
  name (resolved once through the stop finder, then kept for the life of the
  plugin process) or an ID (`de:00000:1001`, or the numeric stop ID).
- The plugin polls in the background, default `refresh: 60s`, never faster than
  30 s, one departure monitor request per poll (plus one stop-finder request at
  start). It sends a `User-Agent` naming the project. Operators are expected to
  check the terms of the installation they point it to.
- Config: `stop`, `lines`, `directions` (substring of the destination), `max`,
  `refresh`, `walk_minutes`. Filters apply to the polled departures; more are
  requested (`limit` = max(3·max, 20)) than shown. Departures that left more than 30
  seconds ago and, with `walk_minutes`, those leaving sooner are hidden.
- Widget `departures` (image and text): line, destination, minutes until
  departure (clock time from one hour on, `time: auto|minutes|clock`), delay
  `+N` from `departureTimeEstimated`, cancellations (`realtimeStatus`
  containing `CANCEL`) with the destination struck through and "Ausfall"
  instead of the time; optional stop header. Sizes in points via `TextPx` and
  `PtPx`.
- A `changed` event fires when the filtered list, the delays or cancellations
  change; the minute countdown alone does not, the widget's `max_age` of 30 s
  keeps it current.

## Consequences

- The service depends on an endpoint without a stated service level. A failure
  shows as the stale bar or the cell notice with the last result kept.
- No new dependency. Other transit systems that are not EFA (HAFAS, GTFS-RT)
  need a further backend later.