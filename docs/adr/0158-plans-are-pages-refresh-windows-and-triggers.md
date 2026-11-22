# 0158: A plan is pages, refresh windows and triggers

- Status: Accepted, implemented
- Date: 2026-11-22
- Supersedes: [ADR 0025](0025-plaene-je-display.md) (plans)
- Amends: [ADR 0127](0127-plan-conditions-are-values.md) (conditions), [ADR 0130](0130-what-a-plan-refresh-does.md) (what a refresh does), [ADR 0131](0131-a-plan-rule-alerts-the-device.md) (the alert), [ADR 0133](0133-the-page-cursor-holds-the-page.md) (the cursor), [ADR 0140](0140-an-explicit-devices-api-answer.md) (the devices API)
- Answers: issue #409

## Context

ADR 0025 gave a plan a list of rules. A rule bundles a time window, value
conditions, a page list, `refresh`, `on_refresh` and `alert`; the first
matching rule wins and `fallback` names a rule for the rest. Four things did
not pay for themselves:

- The time window mostly chose a different refresh interval. "10m during the
  day, 1h at night" was a day rule and a night rule with two page lists.
- A warning duplicated the window of the rule it interrupted, or it stood above
  every window and then suppressed the normal pages for hours.
- `fallback` and the first-match order existed only because pages were bound to
  a time. A plan with one static page list needs neither.
- The page cursor was reset by every rule change, so a warning also reset the
  page the operator had chosen.

ADR 0127 made a condition a named enum value. The plan still had no way to say
"this page at this time" or "this page on this day" without a whole rule.

## Decision

1. **`pages` is the rotation.** It is an ordered list. An entry is a page name or
   `{page: <name>, hidden: true}`. A hidden page is not in the rotation; only a
   trigger shows it. At least one page is visible.

2. **`on_refresh` belongs to the plan.** It is `cycle`, `first` or `current`,
   default `current`. A timed sync applies it; a button press and every other
   sync do not page on their own.

3. **A lost cursor is resolved by `on_refresh`.** A page that leaves the list
   takes the next page at its declared position with `cycle`, the first page
   with `first`, and `page_before_trigger` with `current`. A plan edit that
   keeps the page keeps the cursor by name (ADR 0133).

4. **`refresh` is a default and its windows.** `default` is the interval outside
   the windows; without it the device's own interval applies. A window has
   `from`, `to` and `interval`. `from` is inclusive and `to` exclusive; a later
   `to` than `from` crosses midnight. Windows must not overlap. The next window
   edge caps the sleep, so the device changes its interval on time.

5. **`triggers` is ordered.** A trigger has `when` conditions, a `page` that must
   be in `pages`, and an optional `alert` of `once` or `refresh`. The first
   matching trigger whose page fits the display type delivers its page on every
   sync without button input. A button sync pages the extended list and never
   triggers; the next sync without input pins the page again.

6. **A condition tests a value.** `value` names what it tests:
   - an enum value of a plugin answers one of the keys in `is`;
   - `weekday` is an enum of the plan with the keys `mon … sun`;
   - `time` takes a window of the day in `from` and `to`;
   - `date` takes a range of local days in `from` and `to`. Without `to` the
     window is the single day of `from`.

   `weekday`, `time` and `date` are built into the plan and evaluated from the
   local moment, so they need no plugin. Every condition of a trigger must
   match; the keys within `is` are alternatives. A trigger needs at least one
   condition. The values of the built-in `paperboy` page plugin (`paperboy/time`,
   `paperboy/date`) stay page values; the plan's names are bare.

7. **A trigger lifts the hidden page.** While it holds, its page is part of the
   extended list: the visible pages plus that page at its declared place. When
   it ends, the page is hidden again and the cursor is resolved by
   `on_refresh`. A visible trigger page stays where it is.

8. **`page_before_trigger` is one field of the device state.** It holds the page
   a trigger displaced, for `current`, and is cleared when the trigger ends.

9. **The rule model is gone.** `rules` and `fallback` are rejected with a hint
   that names the new keys. Paperboy has no migration code (ADR 0049).

10. **The state and the API use the new words.** The device state stores
    `trigger` and `page_before_trigger` instead of `rule`. The devices API
    exposes `trigger` instead of `rule`, and the virtual display's debug and the
    sync log follow.

11. **Every time and date condition creates a boundary.** The plan caps the
    sleep at the next refresh window edge, the next trigger window edge and the
    next date midnight. A boundary further than eight days away is ignored.

12. **The pages of a plan are static.** Demand covers every page of the plan,
    hidden or not, and the values of every trigger. A trigger page is therefore
    never cold.

13. **The plan preview shows the plan, not rules.** It lists the pages with
    their previews, the effective interval, the next change and the trigger
    that matches at the selected time. Values are live; the time only selects
    the refresh window.

## Alternatives

- **Keep the rules.** The time window would still carry page sets, a warning
  would still duplicate it, and `fallback` would stay. Rejected by this ADR.
- **One page per display type, or no base page.** That is the page and variant
  question, and it is a separate step. The plan keeps naming page names only.
- **`time` and `dates` as keys of the trigger.** That is the second matcher this
  ADR avoids: a trigger would have a page, a condition list and a time block.
- **Declare a value range in the plugin protocol.** A plugin could declare that
  a value is testable as a time or a date. That is the cleanest "the plan knows
  no names" model, but it costs a protocol field, moves part of the validation
  from the plan parser to the server, and needs its own ADR. The three built-ins
  stay in the plan for now.
- **Prefixed plan values (`paperboy/time`).** The built-in page plugin already
  uses those names for the time and the date of a text page. A bare `time`
  avoids two meanings of one name.
- **Remember the cursor by index.** A plan edit would move it. Names and the
  single `page_before_trigger` field are enough.

## Consequences

- A plan has four lists: pages, refresh windows, triggers and actions. `rule`
  and `fallback` leave the vocabulary.
- Demand, page caching and the preview see a static page set, so a page is warm
  before a trigger shows it.
- A trigger pins its page at the plan's cadence. A button press can page away;
  the next sync without input brings the page back. `alert: refresh` reminds at
  every such sync.
- `current` needs one state field. `cycle` and `first` need no memory.
- A window that crosses midnight with a weekday condition checks the weekday
  now. "Friday night" is `[fri, sat]` or two conditions; most windows are
  daytime, where the weekday is exact.
- Existing plans are refused with a hint. The operator rewrites them by hand.
- The devices API changes the field `rule` to `trigger`. The operator UI, the
  virtual display and the log use the new word.
- A text display keeps its poll from the effective interval; the floors of
  ADR 0045 and 0051 still apply.
- The documentation owns the plan in `docs/design/02-content.md`, the refresh in
  `03-devices.md`, the editor and the preview in `04-operator-ui.md`, and the
  terms in the glossary.
