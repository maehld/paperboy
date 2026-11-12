# 0092: Scrolling lines on text displays

- Status: Accepted, implemented
- Date: 2026-11-12
- Builds on: ADR 0044 (text displays on the device API), 0045 (text poll), 0049 (plugin widgets), 0051 (power source), 0066 (page API), 0071 (display types in files)
- Answers: issue #173

## Context

A text display is as wide as it is, and every line is cut at the cell width:
plugins trim their text to `w.Width` (`plugin.Align`, `plugin.Columns`,
`fitText` with its ellipsis), `page.RenderText` cuts again with `fit`, and the
firmware prints the result statically. On a 20 × 4 HD44780 the tail of a long
headline or of a free-text line disappears without a trace.

A marquee needs the full line at the display and a clock. Two shapes were
considered:

- **The server scrolls.** The offset follows the clock, every poll answers the
  next window, and the existing firmware repaints on the changed hash. No
  firmware change, but at most one character per second (the text poll is
  paced in whole seconds), one HTTP request and one full repaint per step, and
  a jitter that follows the network. Fine for an occasional line, poor as a
  permanent behaviour.
- **The firmware scrolls.** The server sends the full line once, the display
  shows a window and moves it on a local timer. No traffic per step, smooth and
  independent of the network, and the `shown` hash keeps matching, so the text
  poll stays a `204` while the line runs. Costs a firmware change.

## Decision

### 1. A cell asks for it, and the plugin knows

A cell on a text page may set `scroll: true`. It is off by default, so
removing the key turns scrolling off again; `parse` rejects it on an image page
and `Fits` rejects a cell that is not as wide as the display, because a window
over a row would overwrite the neighbouring cell.

The flag reaches the widget: the text request carries `scroll: true`
(`WidgetRequest.Scroll`, an additive protocol field, no version bump). The
widget may then return lines longer than `width`, and it decides what that
means: the built-in `paperboy/text` returns the untrimmed line, a plugin with
its own ellipsis may keep it. Plugins that ignore the field keep their lines
that fit, so nothing changes for them. A widget that answers a long line in a
cell without `scroll` is still cut, exactly as before.

### 2. The server keeps the overflow and the hash

`RenderText` converts the line to the display's charset first (the displayed
column is the unit, not the UTF-8 byte) and, for a scrolling cell, keeps the
full text of every row that is wider than the display. The visible lines carry
the first window, so a display without scrolling still shows a sensible start.

`RenderText` answers `Text{Lines, Scroll}`. `Scroll` has one entry per line,
empty for a static row. The sync and `GET /api/device/text` add `scroll` to
`text`; the field is omitted for a page without scrolling, so old firmware and
old hashes are unaffected. The content hash covers the scrolling rows: a change
behind column 20 still repaints. A row is cut at 120 columns before it is sent:
that is 36 seconds per cycle and keeps the answer inside the firmware's 2 KiB
response buffer, even with four rows of multi-byte characters.

### 3. The firmware runs the marquee

Firmware 2.2.0 for text displays keeps the `scroll` rows next to `pb_lines` and
resets its offset when new text arrives. A 300 ms interval advances every
scrolling row by one UTF-8 character and repaints the display only while a row
overflows: `pb_row(row, columns)` returns the window of a scrolling row or the
static, space-padded line, and `pb_scroll_step` reports whether anything moved.
The marquee repeats the text after two spaces, holds the first window for about
a second after new content, and starts over after its longest row. Static rows
are untouched, so a header or a clock keeps standing while one line runs.

Text displays run on mains power and never sleep (ADR 0044, 0051), so the
repaints cost bus time, not battery. A full 4 × 20 update on the PCF8574 is
about 40 ms at 100 kHz; the interval leaves room and can be raised with the
I²C frequency if a panel needs it.

## Consequences

- A long line on a 20 × 4 LCD is readable instead of silently cut, and the
  scroll pace no longer depends on the network, the plan's `refresh` or the
  text poll.
- The feature needs the new firmware. An installation that keeps the old image
  keeps showing the first window; the server changelog and the firmware offer
  in the sync carry the update (ADR 0043).
- Only whole-width cells scroll: a cell next to another one cannot have a
  window over the row. A page that needs a scrolling line gives it the full
  width.
- Dashboard, `/api/devices`, the editor's draft preview and the virtual display
  show the first window; they do not simulate the marquee. The `paperboy`
  widget is the first plugin to use the flag; the others opt in when they want
  to (rss, calendar, departures keep their ellipsis for now).
- The page editor keeps `scroll` in its model and offers a checkbox for text
  pages, so an edit does not drop the flag. `Encode` writes it, and the
  round-trip test covers an example page.
- A future scroll speed or a pause would be another field in the text answer;
  the firmware interval is fixed for now.
