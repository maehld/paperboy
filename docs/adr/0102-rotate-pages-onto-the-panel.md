# 0102: Rotate pages onto the panel

- Status: Accepted, implemented (firmware not yet flashed)
- Date: 2026-11-13
- Builds on: ADR 0041 (firmware), 0042 (device API), 0049 (pages from widgets), 0050 (physical scaling), 0070 (device files), 0071 (display types), 0072 (QOI pages), 0097 (page downloads)
- Answers: issue #117

## Context

A page is composed for a display type, and the display type's `size` is the
page in pixels, not necessarily the panel's native orientation. The ePaper
panels of the STICKY and the E1001 are landscape (800 × 480), and the firmware
rotates them into the mounting orientation it was compiled for (`rotation: 180`
on the Sticky, `0` on the E1001). A portrait display type (`480 × 800`) was
therefore clipped below the panel, because the QOI decoder draws into a buffer
that is still 800 × 480. The touchscreen is calibrated in the compiled
orientation as well, so a rotated page would also move the menu's hit areas away
from the taps.

The goal is one firmware image per hardware type with the mounting orientation
chosen in the instance, not per reflash. Two facts decide the shape:

- The **page orientation** (portrait or landscape) selects the page layout and
  is already expressed by the display type's `size`. Page variants are keyed by
  the display type, so a layout difference belongs there.
- The **mounting** (which edge is up) is a property of the individual device,
  like its power source or its digitiser. Two devices of the same display type
  can hang differently, and their pages must not be duplicated for that.

## Decision

### 1. A display type names the usual rotation

`displays/<name>.yaml` gains `rotation: 0|90|180|270`, default `0`, in degrees
clockwise **relative to the orientation the firmware was built for**. It only
applies to `kind: image`; a text display rejects it. `size` stays the page:
`size: [480, 800]` with `rotation: 90` is a portrait page on a landscape panel.
`0` keeps every existing display type unchanged.

### 2. A device may override the mounting

`devices/<id>.yaml` gains the same optional `rotation`. Absent inherits the
display type's value, an explicit `0` overrides it, and only quarter turns are
accepted. This is the same inheritance as `power` (ADR 0051): the type decides
the usual case, the device file fixes one unit.

### 3. The sync answer carries the effective rotation

The device sync answer names `rotation` (omitted when it is 0). An older
firmware ignores the field and keeps painting in its compiled orientation, so
the server stays compatible with images that predate this decision. The device
API has no version negotiation; the field is additive.

### 4. The firmware applies it before every paint and to touches

The firmware stores the relative rotation from the sync and calls
`set_rotation` on the display with `(built-in rotation + rotation) % 360` at the
top of the display lambda. That is necessary on every wake, because the display
component initialises from the compiled rotation after each boot and the
firmware deep-sleeps. The generic `DisplayBuffer` rotation moves both
`draw_pixel_at` and `get_width()`/`get_height()`, so the QOI, the splash screen
and the setup screen follow it on both panel drivers.

The touchscreen captures `display_width_`/`display_height_` at setup, before the
sync can set the rotation, and its transform was measured in the compiled
orientation. `pb_touch` therefore turns a touch by the same relative rotation
into page coordinates before it queues the event. The server keeps comparing
taps against the menu's page-space cells, so no menu code changes. The Sticky is
the only shipped hardware with a digitiser; the E1001 has buttons only.

A QOI whose dimensions do not match the rotated display is refused with a note
and a log line, like a truncated one. `draw_pixel_at` clips silently, so the
alternative would be half a page over the previous picture — the exact bug this
decision removes.

The splash and setup screens appear before the first sync, when the relative
rotation is still 0. They stay in the compiled orientation; persisting the
rotation in flash for a cosmetically correct boot screen is not worth it.

## Consequences

- Page composition, the page cache, the PNG/QOI routes, plans and the virtual
  display stay unchanged. Rotation is applied when the firmware paints, not
  when Paperboy composes.
- A portrait device needs a portrait display type. Pages whose grid works in
  both orientations can stay shared; only layouts that really differ need a
  `pages/<page>.<type>.yaml` variant.
- A wrong rotation no longer clips silently: the firmware refuses the page and
  says so in the sync log.
- The Sticky's menu taps must be verified on hardware, because the touch
  transform and the display rotation are measured properties.
- Both image firmwares change: the Sticky rises to 2.7.0 and the E1001 to 2.8.0.
- Text displays are unaffected and reject a non-zero rotation.

Considered and rejected:

- **Rotation only on the display type:** two devices with the same panel but a
  different mounting would need two types and therefore duplicate page
  variants, although the pages are identical.
- **An absolute rotation with the hardware files built at 0:** every existing
  Sticky device would have to move to `rotation: 180` by hand. The relative
  value keeps an instance that never sets the field exactly as it is.
- **Rotating the QOI on the server:** the page cache is keyed by display type,
  so it would need one entry per rotation; touch events would have to be turned
  back on the server; previews and `last_page_url` would show the panel
  orientation instead of the page; and every page would cost an image rotation.
