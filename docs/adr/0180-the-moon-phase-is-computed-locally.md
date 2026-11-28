# 0180: The moon phase is computed locally

- Status: Accepted, implemented
- Date: 2026-11-28
- Builds on: ADR 0049 (plugin widgets), 0151 (design language)
- Answers: issue #503

## Context

The weather plugin shows the current moon phase beside the phase name and the
illuminated share. Open-Meteo has no moon data. A second service would add a
source, a key and a failure mode to a value that changes once a day.

## Decision

1. The plugin computes the phase itself, without a new dependency. It counts
   the synodic month of 29.530588853 days from the new moon of
   2000-01-06 18:14 UTC. The illuminated share is `(1 - cos(2π·phase)) / 2`.
2. The picture is one of the 28 phase glyphs of Weather Icons
   ([ADR 0179](0179-pictograms-from-weather-icons.md)). The name is one of
   eight localised names.
3. The plugin shows no moonrise and no moonset. Both need a position of the
   moon, which this computation does not give.

## Consequences

- The widget needs no request and no state, and it works for a date without a
  forecast.
- The phase can be off by about one day. That is enough for a name and a
  share, but not for an event time.
- The eight names are wider than the 28 glyphs. The picture follows the
  nearest 28th of the cycle, the name the nearest eighth.
