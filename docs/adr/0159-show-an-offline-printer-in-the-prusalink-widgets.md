# 0159: Show an offline printer in the prusalink widgets

- Status: Accepted, implemented
- Date: 2026-11-22
- Builds on: [ADR 0149](0149-a-prusalink-plugin-for-printer-status.md) (PrusaLink plugin), [ADR 0127](0127-plan-conditions-are-values.md) (conditions are values), [ADR 0151](0151-a-shared-design-language-for-plugin-widgets.md) (design language)
- Amends: point 4 and the consequences of [ADR 0149](0149-a-prusalink-plugin-for-printer-status.md)
- Answers: issue #420

## Context

The PrusaLink plugin reads a printer on the local network. The operator switches
the printer off in the evening, so a request then fails.

The first version answered the failure as an error of the cell. After a good
answer it kept the last data. A switched-off printer therefore showed an error,
or `Printing 42 %` for good. Both mislead the operator. The first says the page
is broken. The second says a print runs on a printer that is off.

One failed request is no proof for an offline printer. PrusaLink can be slow
while a print runs, and a network can drop a single packet. A false offline
notice costs a full refresh on e-paper, and the next answer costs another one.

## Decision

1. The snapshot counts the failed status requests in a row. The widgets show
   *Printer offline* when the printer misses two requests in a row, about 30
   seconds after the first, and when it never answered. A printer that never
   answered is offline at once. One failure after a good answer keeps the last
   data.
2. Every widget draws the same centered notice in its cell. The plugin returns
   no error for a silent printer, and `Prepare` succeeds. A broken widget name
   stays an error.
3. The `state` value declares the key `offline`. A text page labels it, and a
   plan trigger can show a page while the printer is off. The other values
   answer a dash, so no trigger matches a state that the printer no longer
   reports.
4. A failed job request alone does not count as offline, because the status
   answered. The status failure decides.

## Consequences

- A switched-off printer redraws the page once, into the offline notice, and
  once more when it answers again. The old behavior redrew nothing after the
  first error.
- A printer that never answers delays the first page by the request timeout of
  15 seconds. The first version did the same before it answered an error.
- The plugin keeps the last data in the snapshot. A single hiccup shows it for
  at most one retry interval of 30 seconds.
