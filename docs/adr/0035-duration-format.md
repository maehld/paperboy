# 0035: Duration format and explicit charge direction for derived values

- Status: Accepted
- Date: 2026-10-22
- Extends: ADR 0024 (data document, templates), ADR 0034 (layout primitives and typography)
- Related: ADR 0032 (providers and regions), SolarAI's ADR 0032 (provider pushes data)

## Context

SolarAI delivers the remaining battery time as a derived value (`solar/derived/battery_remaining`). Until now it sent a ready-made text ("3 h 20 min"), because the template vocabulary had no way to render a duration: `power`, `energy`, `percent`, `ratio`, `temp` and `time` all format magnitudes or clock times, `abs` prints a bare number.

Consequences of the text: the provider decides unit, rounding and separators and thereby the presentation of a template; nothing about the value can be recomputed, aligned or reformatted by the service, and every other producer of durations would invent its own wording. The direction (charging or discharging) was part of the text as well ("bis 0 % in …"), so a template could not place it independently of the number.

## Decision

1. **New format `duration`.** It reads a number of **seconds** and renders whole minutes:
   - `2520` → `42 min`
   - `7200` → `2 h`
   - `12000` → `3 h 20 min`
   - more than a day stays in hours (`111900` → `31 h 5 min`), because a day unit would need language-dependent wording
   - `null` → `—` as with every other format

   The value is rounded to whole minutes; the decimal separator of the global `language` applies to numbers, the unit abbreviations are the same in `de` and `en`.
2. **Providers send numbers and flags, never formatted texts.** `solar/derived/battery_remaining` becomes an object `{seconds, charging}` (SolarAI's ADR 0032). A template words the direction itself:

   ```yaml
   - label: Restzeit
     value: solar/derived/battery_remaining/seconds
     format: duration
     detail: '{{ if .solar.derived.battery_remaining.charging }}bis voll{{ else if .solar.derived.battery_remaining }}bis 0 %{{ end }}'
   ```

   The second `if` keeps the detail empty while the value is missing.

## Consequences

- The provider stays free of units and wording; the template owns both and can show the direction where it fits.
- The remaining-time row on the battery reference page now shows "3 h 20 min" with the detail "bis voll" (charging) or "bis 0 %" (discharging); the golden images were regenerated.
- `duration` is a plain magnitude format, so it also works in tiles, headers and template helpers (`{{ duration .path }}`), not only in rows.
- A duration below 60 s renders as `0 min`; producers that need seconds of precision cannot express them. Add a `seconds` unit only when a page needs it (new ADR).

## Open questions

1. Should `duration` support days (`2 d 3 h`) and thus language-specific unit names? Only if a page shows multi-day durations.
2. Do we need a duration for the chart axis or the header status? Not yet.
