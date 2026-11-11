# 0080: The menu's tap controls are their own strip

- Status: Proposed
- Date: 2026-11-11
- Builds on: ADR 0057 (action menu), ADR 0067 (buttons and the menu), ADR 0078
  (the menu stays open while it is used)
- Answers: issue #136

## Context

ADR 0057 put the close box (`X`) and the page arrows (`<`, `>`) in the same row
as the actions. Only the touch path ever handled them (`menuCell.tap`); with
buttons they were unreachable, and a long `home` was the only way to close the
menu. On the E1001, which has no digitiser at all, the `X` was therefore a
control that could never be used, and the arrows only appeared when the list
had more than one page.

The hardware test on 2026-11-11 (issue #136) made this visible: on the E1001 the
panel showed an `X` that no button could reach, and the confirmation screen said
"Home = ja, andere Taste = nein" while showing `Ja`/`Nein` cells, so it looked
like a choice that did not exist.

## Decision

### 1. The tap controls get a strip of their own

The overlay keeps the header for confirmation and result text, then the row of
action cells, then a **control strip** along its bottom edge, a quarter of the
overlay's height. The strip holds

- `‹` and `›` on its left, only while the list has more than one page, and
- the close box `✕` on its right.

The strip is drawn only for devices whose operator turned the digitiser on, and
those cells are styled as controls: a thin frame around a small grey glyph,
never an inverted fill like a selected action. A device without the strip keeps
the whole overlay for its actions.

### 2. `touch` is a device setting

`devices/<id>.yaml` gains an optional `touch: true|false`, default `false`
(ADR 0070's file, no display-type field). The device file wins; otherwise the
value the firmware reports in `register`/`sync` counts, defaulting to `false`.
The firmware does not send it yet — the API accepts the field so a later build
can, and until then the operator sets it by hand.

It is a per-device decision on purpose: a digitiser may be present and still
unwanted (accidental taps, a cheaper interaction model, battery).

### 3. The close box is reachable with buttons

Where the strip is drawn, `next` past the last action moves the selection onto
the close box, `home` there closes the menu and keeps the page, and `previous`
returns to the last action. The selection on the close box is a heavier frame,
not an inverted fill, so it still reads as a control and not as an action.
Without the strip nothing changes: the list wraps and a long `home` closes.

### 4. The confirmation names the keys

The confirmation screen says **"Home = ausführen · Next/Previous = abbrechen"**
("Home = run · Previous/Next = cancel") instead of "Home = ja, andere Taste =
nein". There is no selectable yes/no row, so the text names the buttons that
exist: `home` runs the action, `previous`/`next` return to the list.

## Consequences

- On a button-only device the menu no longer draws controls that cannot be
  pressed; the panel and the buttons agree.
- On a touch device the controls are visually separate from the actions, and
  the close box can be reached with either input.
- Every menu layout gains a second area: `menuLayout` computes the action cells
  and the strip together, and taps use the same geometry, so what is drawn and
  what is hit cannot drift apart.
- `ADRs 0057 and 0067 stay as history; their cell order (close box first) is
  superseded.
- Two findings from the same test stay open in #136: a disabled action does not
  show the plugin's reason on the panel, and a menu in which every action is
  disabled says nothing at all.
