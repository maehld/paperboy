# 0086: Four gray levels on the E1001

- Status: Accepted, implemented
- Date: 2026-11-11
- Builds on: ADR 0041 (ESPHome firmware), ADR 0072 (pages as QOI)
- Related: issue #151 (partial updates for overlay screens)

## Context

The E1001's panel (GDEY075T7, UC8179, 800×480) has four gray levels in
hardware: 2 bits per pixel with grayscale LUTs, full refresh only, a few
seconds per refresh. ESPHome's model `Seeed-reTerminal-E1001` drives it in
black and white (1 bpp, the same driver as the Waveshare 7.5in V2), so the
firmware has to approximate Paperboy's four levels with a fixed 2×2 dither
(`pb_paint_qoi_dithered`).

That dither is applied to every midtone, including the antialiased edges of
every glyph. A ring of single dots around each letter is what the device
shows. With `gray_levels: 1` on the display type instead, the server
thresholds the page: no dots, but hard, slightly fat glyphs. Both look clearly
worse than the Sticky, which has real grays, and the panel is the larger of the
two.

`formorer/uc8179_gray4_esphome` provides an ESPHome display component for
exactly this panel: a `DisplayBuffer` with `DISPLAY_TYPE_GRAYSCALE`, 2 bpp
framebuffer, the Seeed_GFX grayscale LUTs (MIT), a `lut_mode` switch for the
two panel batches and an `invert_colors` flag for panels with reversed data
polarity.

## Decision

- **The E1001 draws real gray levels.** `firmware/esphome/e1001.yaml` replaces
  the `epaper_spi` display with `platform: uc8179_gray4` on the same pins. The
  busy pin is **not** inverted, unlike Seeed's `waveshare_epaper` template: the
  driver reads the active-low signal itself and an inverted flag only adds a
  30 s timeout per refresh.
- **No server change.** The `e1001` display type keeps `gray_levels: 4`;
  Paperboy sends 4-level QOI as before. The component quantizes with
  `luma >> 6`, so Paperboy's 0/85/170/255 become black, dark gray, light gray
  and white, and the existing band table in `pb_paint_qoi_gray4` (0/64/128/255)
  hits levels 1 and 2 on both drivers.
- **The framebuffer starts white.** `pb_paint_qoi_gray4` skips white pixels, so
  the display lambda calls `it.fill(Color::WHITE)` before painting; otherwise a
  repaint within one wake cycle keeps the previous page where the new one is
  white.
- **The component is a pinned build dependency.** `external_components` fetches
  it from GitHub at commit `61f881b7e970e454a2e8ee503a003276cb0c1b6f` (the
  repository has no releases). It is referenced, not vendored: upstream has no
  license file, only the LUT tables carry their Seeed_GFX MIT origin, and
  vendoring would redistribute the code. The *Firmware* workflow fetches it the
  same way as a local build.
- **Panel variance stays with the image.** `lut_mode` (`otp`/`custom`) and
  `invert_colors` are substitutions in `e1001.yaml`, because both are
  properties of the individual panel: a wrong LUT order or a negative picture
  is fixed there, not in Paperboy and not per device.
- **The paint wait becomes hardware-specific.** `common.yaml` waited for
  `epaper_spi.is_updating`; the new platform does not have that condition. The
  wait moves into a `wait_paint` script that every image hardware type
  provides: the Sticky keeps its condition, the E1001's driver refreshes
  synchronously and has nothing to wait for.
- `fw_version` becomes **2.4.0**.

## Alternatives

- **Keep the dither.** No dependency and no risk, but the dotted halos around
  every glyph remain; it is the reason for this ADR.
- **`gray_levels: 1` and bigger text.** Removes the halos but not the hard
  edges, and it throws away the gray levels of the whole page.
- **Write our own UC8179 grayscale driver.** Same commands and LUTs, more code
  to maintain, and the LUT data still comes from Seeed_GFX.
- **Wait for ESPHome to support grayscale on the E1001.** The model is
  explicitly mono; there is no upstream work to wait for.
- **Vendor the component into `firmware/esphome/components/`.** Reproducible
  without network, but takes over code whose license is not stated, and
  upstream fixes would have to be merged by hand.

## Consequences

- Font antialiasing survives on the E1001, so pages look like the preview and
  like the Sticky instead of like a dotted bitmap.
- **No partial updates.** Grayscale is full-refresh only, so each paint takes a
  few seconds and flashes. That also means the E1001 cannot take the partial
  update path for menu overlays (issue #151); it would need a hybrid driver.
- **The main loop blocks during a paint.** `component.update` returns only
  after the panel is idle, so button presses in that window are not queued any
  more (the Sticky's asynchronous driver still queues them). Measure on the
  device; if it matters, the driver needs a state machine.
- A firmware build now needs network access to GitHub, and the image depends on
  a third-party commit. Pinning makes builds reproducible but freezes upstream
  fixes: raising the commit is a deliberate change.
- The grayscale output is only as good as the panel's LUT mode. Both
  substitutions and the expected symptoms are in
  `firmware/esphome/README.md`; the level order, a negative picture, the
  refresh time and presses during a paint have to be verified on the device.
- `pb_paint_qoi_dithered` has no user in the shipped firmware any more. It stays
  as the fallback for a mono panel and for a batch that cannot do grays.
