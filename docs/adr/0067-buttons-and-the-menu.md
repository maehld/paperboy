# 0067: Buttons – home opens the menu, long presses page

- Status: Accepted, implemented
- Date: 2026-11-08
- Builds on: ADR 0042 (device API), ADR 0057 (action menu), ADR 0064 (device protocol v4)
- Answers: issue #88

## Context

ADR 0057 gave `home` two meanings: a short `home` opens the action menu when the
plan offers actions, otherwise it goes to page 1; a long `home` closes the menu
**and** goes to page 1. Two things turned out to be undesirable:

- The behaviour of `home` depended on the plan. On a plan without actions the
  same key did something else, and on a plan with actions a long press left the
  menu and also jumped to the first page.
- The firmware never sent `hold_ms` at all: `pb_button` queued only the role and
  the event ID, so long presses did not exist on real devices. The server-side
  long-press handling was dead code outside the tests.

## Decision

### 1. Home is the menu key

A short `home` always opens the menu when the device may use it (not `actions:
deny`), has a plan and is not in a critical battery state. A plan without
actions opens an **empty menu**: a banner "Keine Aktionen" / "No actions" in
place of the action grid, no cells and no selection. It closes on any input, on
a tap, or after at most 10 s (or `menu_show_duration` when that is shorter), so
a battery device does not stay awake for a menu that has nothing to offer.

### 2. Go to start moves to the direction keys

A long `next` or `previous` with the menu closed goes to the first page of the
active rule. This is the old "long `home` = go to start" on a key that is
otherwise unused for navigation. Inside the open menu long presses are
ignored: the direction keys only move the selection there.

### 3. Long home only closes

A long `home` closes the open menu and keeps the page. `X` and a tap do the
same. A long `home` with the menu closed does nothing; it does not open the
menu, only a short press does.

### 4. The server decides what is long

The firmware measures the press and sends `hold_ms` with the event. The server
treats a press of at least 700 ms as held. Keeping the threshold on the server
allows tuning it without a firmware update and keeps button semantics out of
the firmware.

The firmware queues the event when the button is released or after the hold
timeout while it is still held, then starts its sync. A normal tap therefore
syncs when the finger lifts, which is roughly the earlier timing; a held
button reports at the threshold even if the user keeps holding it.

The empty menu is a new phase (`empty`) in `menuState`; its hash is part of the
served image like every other menu screen.

## Consequences

- Devices need firmware 2.1.0 or newer for long presses. An older device keeps
  short `home` = menu and cannot jump to page 1 with a long press; Paperboy
  offers the new image through ADR 0043.
- The button that dismisses the menu (long `home`, any input on the empty
  banner) never also pages, because the input belongs to the menu that was
  open when the sync arrived.
- A denied device keeps its old behaviour: short and long `home` page to the
  first page.
- The empty menu costs two panel refreshes (open and close) and keeps the
  device awake for up to 10 s. That is the price of `home` meaning the same
  thing on every plan.
- ADR 0057 stays as history; the menu's phases, drawing, confirmation and
  actions are unchanged apart from the empty banner.
