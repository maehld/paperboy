# 0161: Home syncs the page and the device menu goes

- Status: Accepted
- Date: 2026-11-24
- Supersedes: [ADR 0057](0057-plugin-actions-from-the-plan.md) (actions from the plan), [ADR 0067](0067-buttons-and-the-menu.md) (buttons and the menu), [ADR 0078](0078-the-menu-stays-open-while-it-is-used.md) (the menu stays open), [ADR 0082](0082-menu-tap-controls-in-a-strip.md) (the menu's tap controls)
- Answers: issue #431
- Prepares: issue #432 (widget actions)

## Context

A short `home` opened the device menu with the plan's actions. The menu needed a
plan-level action list, an overlay, a confirmation screen, a result banner, a
tap strip and a deadline wake. After #431 no widget can run an action.

Issue #432 moves actions onto the page cells. The menu then competes with the
new mode: a tap on a widget and a long `home` to confirm cannot coexist with a
menu on a short `home`. The plan-level action list would also have no caller,
because a page owns its action.

On a touch display the panel keeps its picture in deep sleep and the digitiser
is off. A tap cannot wake the device. The only way to activate the digitiser is
a button wake that syncs the served page and keeps the device listening. That
is the job of `home`.

## Decision

1. **A short `home` is a plain sync.** It keeps the page cursor and the trigger
   decision. It does not page, does not open an overlay and does not signal. It
   reports `listen_s = 20` like every input.

2. **A long `home` does nothing.** The button has one meaning.

3. **`next` and `previous` page.** A long `next` or `previous` jumps to the
   first page of the plan, also for a device with `actions: deny`.

4. **The device menu is removed.** That is the plan's `actions` list,
   `menuState`, the overlay drawing, the tap strip, the confirmation screen,
   the result banner, the deadline wake, `menu_show_duration`, the virtual
   display's `menu` parameter and the `menu` field of the devices API.

5. **Removed keys get a hint.** `actions` in a plan and `menu_show_duration` in
   `paperboy.yaml` or a device file are rejected with a hint. Paperboy has no
   migration code.

6. **Two device settings stay.** `actions: allow|deny` and `touch` remain on the
   device file as the switches for the widget actions of #432.

7. **The sync reasons `menu` and `result` stay accepted.** A one-shot wake of
   the old server may still arrive during the upgrade. Nothing sends them.

8. **Button input keeps the page and the trigger.** A trigger waits for the
   next sync without button input. A short `home` counts as button input, so
   `choosePlanPage` takes an explicit `buttonInput` flag instead of inferring
   input from an empty page-role list.

9. **A button press is not a timed refresh.** `on_refresh` applies only to a
   timed sync. A short `home` does not cycle a page and does not alert.

## Alternatives

- **Keep the menu on a long `home`.** Rejected. A long `home` is the natural
  confirm key for the widget actions of #432. Two overlays with two key maps
  would be hard to learn.
- **Keep the menu for key-only devices.** Rejected. The menu would need a
  second action source and a second state machine. The widget mode will serve
  the E1001 and the TRMNL with the later key step.
- **Keep `menu_show_duration` for a later overlay.** Rejected. No overlay
  remains. The widget mode gets its own deadline in #432.
- **Make `actions: deny` page to page 1 again.** Rejected. The key should not
  change its meaning per device.

## Consequences

- No action is reachable until #432. `main` may be without actions.
- A plan has pages, refresh windows and triggers; the plan editor loses its
  actions panel.
- The state file and the devices API lose the menu. An old state file keeps the
  unknown `menu` key, which the JSON decoder ignores.
- A Home press never changes the page. The device stays awake for 20 s, so the
  next press is quick.
- The firmware and the plugin protocol do not change. The plugin action
  interface stays in `sdk/plugin` for #432.
