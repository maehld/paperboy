# 0046: Width-aware value formatting for text displays

- Status: Accepted
- Date: 2026-10-31
- Amends: ADR 0044 (text displays on the device API)

## Context

Text pages place values with `right N (power …)`. `right` only knows text: a value as wide as its column touches its neighbour ("435 W15,5 kWh"), a wider one is cut from the left. A 20×4 display has no room to spare, so values should take as few columns as they need without losing their meaning.

## Decision

Text page templates get a helper `fit <width> "<format>" <value>`. It formats the value with one of the existing formats (`power`, `energy`, `percent`, `ratio`, `temp`, `time`, `abs`, `duration`, or `""`) and returns exactly `width` columns, right-aligned, using the first spelling that fits:

1. the normal text (`15,5 kWh`, `100 %`),
2. the unit without its space (`15,5kWh`, `100%`),
3. fewer decimals, with and then without the space (`16 kWh`, `16kWh`; `kW` goes from two decimals down to none),
4. the bare number, most decimals first (`15,5`, `16`),
5. otherwise `#` across the whole width.

`duration` only drops its spaces (`3h20min`); `time`, `abs` and plain values have no shorter spelling. A missing value stays an em dash. Decimal separator and units still come from the global `language` (ADR 0033). An unknown format is a render error. Gaps between columns are written into the template as literal spaces.

`fit` is available on text pages only; image pages keep their typographic `shrink`.

### Aligned units (amendment 2026-10-31)

An optional fourth argument gives the unit a column of its own: `fit <width> "<format>" <value> <unit_width>`. The value is written as number, one space and the unit left-aligned and padded to `unit_width`, so numbers of several lines end in the same column even when their units differ (`5,7 kWh`, `  0 %  `). In this form the unit never moves or disappears: only decimals are dropped, and a number that still does not fit becomes `#` in front of the unit. `power` keeps switching between `W` and `kW` inside the unit column. Formats without a unit (`time`, `abs`, `duration`, plain) leave the unit column blank. Without the fourth argument `fit` behaves as described above.

## Consequences

- Values never touch or get cut silently; at worst they lose the unit, and `#` makes a column that is too narrow visible.
- Templates only state column widths; the shortening order is the same on every page.
- `right` stays for text and for values that must keep their full form.
