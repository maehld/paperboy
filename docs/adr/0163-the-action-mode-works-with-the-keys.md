# 0163: The action mode works with the keys

- Status: Accepted, implemented
- Date: 2026-11-24
- Builds on: [ADR 0162](0162-widget-actions-and-the-plugins-choices.md) (widget actions and the plugin's choices)
- Amends: [ADR 0161](0161-home-syncs-and-the-menu-goes.md) §2 (a long home does nothing), [ADR 0162](0162-widget-actions-and-the-plugins-choices.md) §3 and §6 (the touch requirement, the keys only page)
- Answers: issue #441

## Context

ADR 0162 opened the action mode only on a device with `touch: true`. The E1001
and other devices without a digitiser therefore had no way to run an action.
The mode already knows every option and the reason a choice cannot run. Only
the input was missing.

The keys are `previous`, `next` and `home`. A short `home` already syncs the
served page. A long `home` did nothing after ADR 0161. `next` and `previous`
paged.

## Decision

1. **A digitiser is no longer required.** A short `home` opens the action mode
   on every device whose `actions` is not `deny` and whose page has an
   actionable cell. `touch: true` still means that a tap runs an option.

2. **The first runnable option is selected.** The order is the page's cell
   order, then the enum order of each action. A disabled option, including the
   plugin's current value, is skipped. Without a runnable option the selection
   is empty.

3. **A device without a digitiser draws the selection.** The selected tile is
   inverted: a black tile with a white label. A touch device keeps the plain
   tiles of ADR 0162.

4. **The keys operate the key-only mode.**
   - `next` and `previous` move the selection in reading order. They wrap and
     skip disabled options. Every move restarts the 20-second deadline.
   - A short `home` runs the selected option through the invoke path of ADR
     0162 and closes the mode. Without a runnable option it only closes the
     mode, so the reason tiles stay until then.
   - A long `home` closes the mode without an invocation.
   - The deadline wake closes the mode as in ADR 0162.

5. **The touch path stays as ADR 0162.** A tap runs the option under the
   finger. `next` and `previous` close the mode and page. A second short `home`
   asks the plugin again. On a touch device the keys do not move a selection.

6. **`next` and `previous` page only while the mode is closed.** An open
   key-operated mode consumes both keys as the selection.

7. **The virtual display gets a key-only switch.** `touch=0` plays a device
   without a digitiser: the keys select and run, a click is no tap, and the
   server still never invokes the plugin. Without the parameter the browser
   plays a digitiser.

8. **The protocol does not change.** The server uses the options of ADR 0162.
   The selection lives in the device state.

## Alternatives

- **Draw the selection on every device.** Rejected. A touch device acts on the
  tile under the finger; a second marker would suggest that a second home runs
  that tile.
- **Move the selection with the keys on a touch device too.** Rejected. The
  selection would be invisible, and #432 already gives the touch device a
  complete key map.
- **Open the mode only when an option can run.** Rejected. The reason tiles
  explain why nothing runs. The operator sees a broken plugin or a small cell
  instead of a home press that does nothing.
- **Keep the deadline fixed from the opening press.** Rejected. A user who
  steps through several options would lose the mode mid-way.
- **A new protocol request with an active flag.** Rejected for this step. The
  disabled current value of protocol 9 is enough to skip it.

## Consequences

- The E1001, the E1003 without `touch` and the TRMNL run widget actions with
  the keys. The sticky keeps its tap path.
- An operator removes `touch: true` to get the key view. The switch no longer
  gates the mode.
- A page with only disabled options shows its reason tiles for 20 seconds, or
  until a short or long `home`.
- The state file gains the selected index of the action mode.
- No plugin version rises. The protocol, the firmware and the plugin programs
  do not change.
