# 0162: A widget action and the plugin's choices

- Status: Accepted, implemented
- Date: 2026-11-24
- Builds on: [ADR 0161](0161-home-syncs-and-the-menu-goes.md) (home syncs, the menu is gone), [ADR 0057](0057-plugin-actions-from-the-plan.md) (plugin actions), [ADR 0049](0049-plugin-widgets.md) (widgets), [ADR 0082](0082-menu-tap-controls-in-a-strip.md) (the digitiser switch)
- Answers: issue #432
- Leaves open: a key selection and a protocol request with a widget context

## Context

ADR 0161 removed the device menu. Actions had no caller any more. A page is a
grid of widgets, so an action belongs to the cell that shows its subject: the
evcc loadpoint widget offers the charge mode, a car widget switches the car.

The plugin already describes its actions, their arguments and their state
(`describe` and `actions`, ADR 0057). The protocol is version 9. The page must
not repeat the arguments or the choices; the plugin owns them.

A touch display keeps its panel picture in deep sleep and its digitiser is off.
A short `home` wakes the device and serves the page. That wake is the moment to
show what can run.

## Decision

1. **A cell names at most one action.** `action: mode` on an image cell names an
   action of the cell's plugin. The page carries no action arguments, titles or
   confirmations. A text page has no actions.

2. **The plugin owns the choices.** The server resolves a cell in three steps:
   - `describe` names the action and its arguments;
   - every argument the widget also declares is taken from the widget's args
     (`index` from the loadpoint widget);
   - the one remaining required enum argument is the choice, and the server
     makes one `actions` call per enum value.

   The `actions` answer supplies the tile text and the reason a choice cannot
   run. A plugin that is down or does not know the action keeps the cell with
   the reason; it never costs the page.

3. **A short home opens the action mode.** It needs `touch: true`, an
   `actions` setting other than `deny`, and a page with at least one actionable
   cell. Every actionable cell is replaced by its option tiles. Paperboy draws
   this view, not the plugin. A frame marks the cell, a disabled option is
   hatched, and a cell that cannot hold its options says so.

4. **A tap runs one option in the sync.** The answer is the normal page with
   the new state. That is one paint and no confirmation. A failed call shows a
   black banner with the reason; the next sync serves the plain page again. A
   tap outside the tiles leaves the mode. A tap whose `shown` hash is not the
   action view runs nothing.

5. **The mode lasts 20 seconds.** A one-shot wake of reason `action` serves the
   plain page. Every input keeps the device's 20-second input window, because
   that window also serves the next key press. A timer wake reports 0.

6. **The keys only page.** `next` and `previous` page and leave the mode. A
   long `home` does nothing. A selection with the keys comes later.

7. **Protocol 9 is enough.** The server uses `describe` and `actions` as they
   are. A plugin's `confirm` is ignored in this version.

8. **The active choice is the disabled one.** Version 9 has no active flag. A
   plugin that marks its current value as unavailable (evcc does) gets a
   hatched tile. A real `active` flag waits for a later protocol.

## Alternatives

- **A new `ActionOptions` request.** It would carry the widget context and an
  `active` flag, and allow more than one dimension of choice. It costs protocol
  version 10 and raises every plugin version, because `sdk/plugin` changes. The
  charging mode does not need it; the request can come when an action does.
- **The plugin renders the action view.** Every action plugin would need a
  second renderer, the hit areas would come from the plugin, and the look would
  diverge. Paperboy draws the view from the plugin's data instead.
- **Actions on the page with their arguments.** That is the plan model of ADR
  0057 in a new place: the page would repeat the plugin's schema and a page
  used by two plans could disagree with itself.
- **Confirm every action.** `confirm` exists in version 9. Destructive actions
  need a dialog; the charging mode does not. The confirmation can come with the
  key step.

## Consequences

- An operator writes `action: mode` on a widget; the plugin decides what the
  home screen offers. evcc renames its action argument `loadpoint` to `index`,
  so it matches the loadpoint widget, and returns the short mode label.
- Only the evcc plugin version rises. The protocol and the other plugins do
  not change.
- A cell shows one dimension of choice. A plugin with two required enum
  arguments keeps its cell disabled with a reason until a later protocol.
- The action view needs a cell with room for its tiles. A small cell says
  `Cell too small`; the operator enlarges it.
- The keys cannot run an action yet. The E1001 and the TRMNL wait for the key
  step; a touch display runs an action with a tap.
