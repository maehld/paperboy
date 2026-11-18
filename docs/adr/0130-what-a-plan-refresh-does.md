# 0130: What a plan refresh does

- Status: Accepted, implemented
- Date: 2026-11-18
- Builds on: ADR 0025 (plans), 0042 (device API), 0057 (the menu), 0064 (device protocol v4), 0091 (virtual display)
- Answers: issue #316

## Context

A plan rule has one switch for paging, `rotate: true` (ADR 0025). It advances the page at every timed refresh. Without it, only a button changes the page, and `page_timeout` returns to the first page afterwards. Three problems follow.

- A plan cannot say "always the first page" or "keep what the user picked". A device without buttons shows the first page either way.
- `page_timeout` is a global default. It cannot be off, it has no per-device or per-plan value, and it forces an extra wake just to repaint the first page. That wake costs energy on a battery device.
- `rotate` reads like `rotation`, the quarter turn of the panel (ADR 0102). Two meanings of one word are a bug (glossary).

## Decision

1. A rule names `on_refresh` with one of three values:

   | Value | A timed refresh shows |
   |---|---|
   | `cycle` | the next page of the rule, wrapping at its end |
   | `first` | the first page of the rule |
   | `current` | the page it showed before |

2. The default is `current`. A plan without the key keeps its page at a timed refresh.
3. A rule change always starts at the first page. This includes a change from a time window and from a value condition. The rule for `current` stays one sentence, and condition rules need no extra order or name rules.
4. A button press acts before the refresh. A sync with button input never also cycles or resets. The menu keeps its own pause: while it is open, paging and the automatic change wait (ADR 0057).
5. `defaults.page_timeout` is removed. The page cursor stays until a button, a rule change or a refresh mode moves it. `last_input` leaves the device state, and no extra wake caps a sleep.
6. `rotate: true` stays as an alias for `on_refresh: cycle`. The parser and the editor API accept it; the editor and `Encode` write `on_refresh`. A plan that also names another mode is rejected with a hint.
7. The plan editor offers the three values, and the plan preview names the effective mode.

## Consequences

- A plan states its paging in one place. The global timeout and its extra wake disappear.
- Old configs with `defaults.page_timeout` fail with a hint that names `on_refresh`. Old state files keep an ignored `last_input`.
- A `current` rule holds a button-paged page over any number of refreshes. Only a rule change or a button moves it.
- A `first` rule may reset a button-paged page at the next refresh, which can be one second on a text display.
- The virtual display loses its `page_timeout` parameter. An old URL is rejected with a hint.
- No protocol change and no firmware change: the device receives `refresh_s` and a page, as before.

## Alternatives

- **Keep `page_timeout` for `first`.** It only resets earlier than the next refresh, and the extra wake stays. A second switch for one edge case does not pay.
- **Keep the current page across a rule change by name.** It needs rules for a page that the new rule lacks, and for a page whose display type is skipped. A condition rule would then have to know the page lists of its neighbours.
- **A boolean pair (`rotate`, `keep`).** It allows contradictory settings and still has no name for the default.
