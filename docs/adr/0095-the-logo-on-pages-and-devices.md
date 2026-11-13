# 0095: The Paperboy logo on pages and on the device

- Status: Accepted, implemented
- Date: 2026-11-13
- Builds on: ADR 0041 (ESPHome firmware), 0049 (plugins and widgets), 0071 (display types)
- Answers: issue #209

## Context

The project has a logo (`docs/logo`), and the operator pages show it: the lockup
in every header, the small cut as the favicon. Pages and devices had nothing:
a page could not carry the mark, and a device showed instructions and pages but
never said whose device it is.

The mark is a simple thing: filled shapes only, one colour, a page of four by
three cells with one cell drawn and the top-right corner turned down. There is
no gradient and no stroke that depends on a renderer.

## Decision

### 1. A logo widget draws the design, not a picture of it

The built-in plugin offers `paperboy/logo`: the mark centered in the cell, as
large as the cell allows, with `wordmark: true` for the lockup. The widget tests
the geometry of `docs/logo` per pixel (a small predicate, `markInk`) and
supersamples its edges, so it stays crisp at any cell size and the four-level
palette antialiases the diagonals. No raster asset is generated and kept in
step, and the widget needs no files at runtime.

The wordmark is drawn in Go Mono Bold (`render.MonoFace`), the face the lockup
converts to outlines, so page and UI show the same wordmark.

### 2. Image displays only

A 4:3 mark does not fit a character grid, and a character display shows pages,
not pictures. A text page therefore has no logo widget; the mark stays out of
`paperboy/logo`'s kinds, and the editor does not offer it there.

### 3. The firmware draws the same geometry

`pb_logo_ink` in `paperboy.h` is the counterpart of the Go predicate: the setup
screens draw the mark above their instructions and a power-on shows it alone
for 5 s. Nothing is embedded as a bitmap, so the images stay small and the mark
looks like the one on the pages.

### 4. The splash is a power-on splash

Only `wake_reason == "powercycle"` gets it. A timer or button wake paints its
page as before: a full refresh costs about 2 s on both panels, and a splash on
every wake would add five more seconds to every page change and every menu step
for no information. Wi-Fi connects while the splash is on the panel, so the
first page of a power-on loses little.

### 5. The splash drops the shown hash

The panel shows the mark, not the page. RTC memory survives a reboot (a firmware
update reboots into the new image), so an "unchanged" answer would leave the
splash on the panel until the next page change. Clearing `pb_shown_hash` before
the cycle makes the next sync repaint.

## Consequences

- The geometry exists twice, in Go (`internal/builtin/logo.go`) and in C++
  (`paperboy.h`). Both name `docs/logo` as their source; a change to the design
  has to touch both.
- The splash repaints once more after a power-on or a firmware update, which is
  exactly what it is for.
- The setup screens start their text lower (96 px instead of 24 px) to make room
  for the mark; both panels are 800 × 480, so the longest instruction still fits.
