# 0078: The menu stays open while it is used

- Status: Accepted, implemented
- Date: 2026-11-09
- Builds on: ADR 0042 (device API), ADR 0057 (action menu), ADR 0064 (wakes),
  ADR 0067 (buttons)
- Answers: issue #129

## Context

`menu_show_duration` (default 30 s, at most 60 s) used to count from the moment
the menu opened. That is much shorter than it sounds on a battery device: every
`next`, `previous` or `home` costs a whole wake cycle (connect, sync, download,
panel refresh), about 3–5 s on the Sticky, plus the operator's own reaction
time. Three or four presses to reach an action therefore already exceed the
deadline.

The press that lands after it finds an expired menu: `handleSync` closes the
menu, the sync counts as "the menu took part" and pauses paging (ADR 0067), so
that press neither moves the selection nor turns the page - the panel simply
repaints the page. The next press then pages normally. To the operator this
looks as if `next`/`previous` close the menu and only the following press works,
which is exactly what issue #129 reports.

A second mismatch made it worse: the firmware listens for `listen_s` (20 s) and
only then deep-sleeps for the one-shot wake (`wake: {reason: menu, after_s}`).
The wake is measured from the sync, so it arrived up to `listen_s` after the
deadline. In that window the panel still showed a menu that Paperboy had already
closed, and a press in it just dismissed the old picture.

A live capture on the E1001 with v3.6.0 shows exactly this: the menu opened at
22:01:40, four `next` presses at 22:01:48, 22:01:54, 22:02:00 and 22:02:06 moved
the selection, and the press at 22:02:16 (36 s after opening, 10 s after the last
one) found the expired menu. It closed without moving the selection, and the
press after it turned the page - "the menu was there and then it was gone".

## Decision

### 1. The deadline counts from the last input

`menuState` stores `LastInput` next to `OpenedAt`. `next`, `previous`, `home`
and every tap that keeps the menu open (moving the selection, scrolling,
selecting, the confirmation and its "no") move the deadline to
`LastInput + menu_show_duration`; only `home`/`X` closing it, the result, the
empty banner and the deadline itself end it.

The menu therefore expires after `menu_show_duration` **without input**, which
is what an operator expects from a setting with that name, and navigation never
runs out of time while it is being used.

### 2. Listen window and wake share the remaining time

The sync answer splits the time the menu still has: the firmware keeps
listening for at most `listen_s`, and the one-shot wake gets
`remaining - listen_s` (at least 1 s). The device then wakes when the menu
expires and repaints the page with it, instead of `listen_s` later. This also
applies to the `result` wake of a running action, which now fires after its
budget rather than after the listen window.

### 3. A press on a visible menu belongs to it

The deadline is a server-side timer, but the panel keeps the menu screen until
the deadline wake repaints it. A press that arrives in that window must not be
swallowed and must not page: the panel visibly shows the menu, so the press
belongs to it.

`menuState` therefore stores `Screen`, the hash of the last menu screen served.
`handleSync` only expires a menu when the sync carries no button or touch input,
or when the device reports a different `shown` hash. A press with the menu
screen still on the panel keeps the menu, moves the selection and moves the
deadline (which also asks for a new deadline wake). The deadline wake itself
carries no input and still closes the menu.

No firmware release is needed: the firmware already sleeps after listening, so
only the numbers in the sync answer change.

## Consequences

- Navigating the menu works for as long as the operator keeps pressing, and a
  forgotten menu still closes after the configured duration and keeps the page.
- A press that arrives after the deadline but while the panel still shows the
  menu continues the menu instead of being swallowed; the page only turns after
  the menu screen is really gone from the panel.
- The panel and `state/devices.json` agree about whether the menu is open: a
  press either moves the selection in an open menu or pages after the menu was
  closed and repainted.
- A device that stays awake on mains keeps waking every `menu_show_duration`
  after the last input while a menu is open; a battery device does one extra
  wake per menu, as before.
- `listen_s` is no longer always 20 s after input: while a menu is open it
  shrinks to the remaining deadline (at least 0 s). Input that arrives during it
  still syncs without a full wake.
- ADR 0057 and ADR 0067 stay as history; the phases, the drawing and the button
  meanings are unchanged.
