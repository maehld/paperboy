# 0087: Panel settings live on the device

- Status: Accepted, implemented
- Date: 2026-11-11
- Builds on: ADR 0086 (four gray levels on the E1001)
- Amends: ADR 0086 (its "panel variance stays with the image" decision)

## Context

The first E1001 on firmware 2.4.0 (release 3.9.0) shows every page as a
negative. That is one of the two panel properties the `uc8179_gray4` component
documents: some UC8179 batches drive the two data planes with the opposite
polarity, and the grayscale LUT bank (`otp` or `custom`) differs between
batches as well. Both are properties of the individual panel and cannot be
detected over the write-only SPI wiring.

ADR 0086 left both as substitutions in `e1001.yaml`. That means one image for
all E1001s, a rebuild and a `fw_version` bump per panel, and a value that is
silently wrong for the other batch. A wrong value looks like a broken page, not
like a configuration error.

## Decision

- **Both settings are runtime settings of the device.**
  - `switch: platform: template`, "Invert colors", `RESTORE_DEFAULT_OFF`: the
    data polarity.
  - `select: platform: template`, "LUT mode", options `otp`/`custom`,
    `restore_value: true`, `initial_option: ${lut_mode}` so a build keeps its
    default and the device remembers a change.
  - The display lambda calls `set_invert_colors` and `set_lut_mode` on the
    driver before it draws, so every paint uses the stored choice, including
    the first one after a wake.
- **A button repaints on demand.** Paperboy only sends a new image when the
  composed page changes, so a changed setting would otherwise show its effect
  days later. "Apply display settings" runs `component.update` and paints the
  current buffer with the new settings.
- **No repaint from the switch or select themselves.** A template switch runs
  its triggers before it publishes the new state, and the restore at component
  setup runs them too, which would paint a white page on every wake.
- **The image only carries the default.** The `invert_colors` substitution goes
  away; `lut_mode` stays as the select's initial option. The symptom and the fix
  are documented in `firmware/esphome/README.md` instead of a build flag.
- `fw_version` becomes **2.4.1**.

## Alternatives

- **Hard-code `invert_colors: true` in the image.** Fixes this device in one
  build and breaks the other batch; there is no way to serve both.
- **Flip the picture on the server.** Wrong layer: Paperboy's pages are correct,
  the panel misreads them, and a second page format for one device would leak
  into the editors and the previews.
- **Probe the panel OTP at runtime and pick the LUT bank.** Seeed_GFX does that,
  but it needs a readback over SPI that this board does not have.
- **Two images per batch** (`e1001-otp`, `e1001-custom`, …). More hardware types
  for a property of the unit, and still guesswork at flash time.

## Consequences

- One firmware image serves every E1001; a wrongly configured panel is fixed
  with a switch instead of a new build, and the `Apply` button shows the result
  immediately.
- The settings are per device and easy to get wrong once. The symptoms are
  named in the README, and a wrong setting is visible at once, so that is
  acceptable.
- Every image display hardware type now owns a `wait_paint` script and the
  E1001 additionally three entities in its web UI; the web UI is also the only
  way to these settings, which is fine because it is reachable whenever the
  device is awake.
- ADR 0086's sentence that panel variance stays with the image no longer holds;
  the rest of that decision (real gray levels, the component pin, the blocking
  refresh) stays.
- Verified on one device only: the plan is 2.4.1, and the polarity and the LUT
  bank of further batches remain to be seen.
