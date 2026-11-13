# 0098: The device shows the lockup and its firmware version

- Status: Accepted, implemented
- Date: 2026-11-13
- Builds on: ADR 0041 (ESPHome firmware), 0043 (firmware distribution), 0095 (the logo on pages and on the device)
- Answers: issue #221

## Context

ADR 0095 gave the device the mark alone: above the instructions of the setup
screens and for 5 s at power-on. A device therefore said "Paperboy" but not
which device it is or which image runs on it - and on the setup screens the
operator needs the version when a firmware update is part of the problem.

## Decision

### 1. The splash and the setup header show the lockup

A power-on shows the mark, the wordmark and the version, centered on the panel,
for the 5 s of ADR 0095. The setup screens use the same block as a header, with
the version in the top right corner above the instructions. The version is the
`fw_version` of the image, written as `v2.6.0`, so it matches the name of the
image Paperboy offers (`<hardware>-v<version>.bin`).

The block keeps the proportions of `docs/logo`: the mark, a gap of 0.35 of its
height and the wordmark on the design's baseline at 0.80 of the height.

### 2. The wordmark is a font, not a bitmap

The mark is geometry, so the firmware draws it as before. The wordmark's
outlines are too complex for a predicate, and a bitmap would either be blocky
when scaled up or fixed to the two sizes of the screens.

Go Mono Bold is the face of the wordmark and is not on Google Fonts, so the
firmware ships a subset (13 KB, the letters of `paperboy`, the digits, `v` and
`.`) under `firmware/esphome/fonts/`, taken from `golang.org/x/image` - the
module `pkg/render` draws the same face from. ESPHome rasterizes it at build
time, so the image holds the glyphs of the four sizes the screens use, not the
font file. The subset carries the font's name table and ships with its
BSD-3-Clause licence; `fonts/README.md` says how to regenerate it.

### 3. The version sits in the same face

`v2.6.0` under the wordmark and in the corner is Go Mono Bold too, so the block
reads as one lockup. Inter stays the typeface of the instructions.

## Consequences

- A power-on and every setup screen say which firmware runs on the device. That
  is the one place where an operator looks after flashing over USB.
- The wordmark in the firmware and in the `logo` widget can drift if the design
  changes: both name `docs/logo` as their source, and the font subset has to be
  regenerated from the same face the wordmark was converted from.
- The subset adds no build-time download; the firmware workflow keeps working
  offline for the font.
