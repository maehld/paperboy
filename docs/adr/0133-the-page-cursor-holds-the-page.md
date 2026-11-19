# 0133: The page cursor holds the page

- Status: Accepted, implemented
- Date: 2026-11-19
- Builds on: ADR 0025 (plans), 0042 (device API), 0080 (device state per MAC), 0130 (what a plan refresh does)

## Context

The device state stores the page cursor as a number: `page` is the index into the
pages a rule offers for the display type. A rule change resets it to the first
page (ADR 0130).

An operator edits a plan file while a device shows one of its pages. The state
keeps `page` and the rule's name, so the index survives the edit. When the edit
adds, removes or reorders pages, the index names another page: the device shows
a page the operator did not leave it on and needs several presses to return.

A rule change and a plan edit look alike from the outside, but they are not the
same. A rule change is a different rule. A plan edit keeps the rule and changes
its pages.

## Decision

1. The device state also stores `page_name`: the page the cursor points at.
2. A sync that stays in the same rule resolves the index from the name before
   buttons and refresh modes apply. A page the rule lost starts the rule at its
   first page.
3. Every sync that chooses a page writes its name to the state.
4. A rule change still starts at the first page. The name does not carry over to
   another rule, because that rule may not list the page (ADR 0130).
5. A state file without a name keeps its index and receives a name on the next
   sync. There is no migration.
6. A duplicate page name resolves to its first entry. A rule that lists a page
   twice gains nothing, and the plan editor warns about it.
7. The critical battery warning clears the name, so the page cursor starts over
   when the pages return.
8. No protocol and no firmware change: the device receives a page as before.

## Consequences

- An edit of the plan keeps a device on its page. A page in front of the current
  one no longer moves the display.
- A page the rule lost or renamed starts the rule at its first page. This is the
  only case where an edit moves the cursor, and it has no better answer.
- The shown page follows the page, not a number. The text position of a text
  page and the page count stay correct because the index is recomputed.
- The state file grows by one short string.
- The plan editor needs no change: it writes pages, and the device decides what
  its cursor means.

## Alternatives

- **Reset to the first page on every plan edit.** It needs the plan's version or
  hash in the state and moves a device away from its page after an edit that
  does not touch the page list, for example a corrected refresh value.
- **Keep the index only.** This is the behaviour that reports the problem.
- **Hold the page across a rule change by name.** ADR 0130 rejected it: a rule
  may not list the page, and a condition rule would have to know its neighbours.
- **Store the last served page instead of the cursor.** A notice page or a
  battery warning leaves no page name, so the cursor would restart after each
  interruption.
