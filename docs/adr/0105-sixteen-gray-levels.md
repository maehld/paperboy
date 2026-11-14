# 0105: Sixteen gray levels on the panel

- Status: Accepted
- Date: 2026-11-13
- Related: ADR 0049 (plugins), ADR 0050 (sizes in points), ADR 0071 (display types in files), ADR 0086 (four levels on the E1001), ADR 0088 (pages that do not fit the buffer), issue #192

## Context

Paperboy's palette had two tones (black and white) or four (0, 85, 170, 255). The E1003's IT8951 panel draws 16 real gray levels and its firmware maps every luminance of a QOI page onto them, but a display type could not ask for more than four: `internal/paperboy/displays.go` rejected every `gray_levels` other than 1 or 4, `pkg/render` built only the two palettes, and the plugin protocol accepted only `levels` 2 and 4. Antialiased text, charts and photographs therefore stayed at four tones on a panel that can show 16.

## Decision

1. **`pkg/render` grows a 16-level palette.** `Gray16Palette` holds 16 evenly spaced tones from 0 to 255 (step 17). 85 and 170 are part of it, so a four-level page keeps its exact grays when it is drawn on a 16-level panel. `NewCanvas` selects 2, 4 or 16 tones, `Levels` reports the number of tones of a palette, and `Quantize` already scales by the palette's length. Drawing code (fonts, fills, icons) needs no change: Go maps every colour onto the destination palette.
2. **A display type may say `gray_levels: 16`.** The runtime model carries `Levels int` (2, 4 or 16) instead of a `Gray bool`; `page.Display.Levels` travels the same value into composition and widget requests. `gray_levels: 1` still means black and white (two tones). The display type editor offers 16 next to 1 and 4.
3. **The plugin protocol accepts `levels: 16`.** This is an additive value of `render`, not a new action, so the protocol version stays 6 (ADR 0081). A plugin built before this change answers "expected 2 or 4" and its cells show that error: a 16-level display needs current plugin builds.
4. **Plugins draw with the palette they are given.** Widgets that conditionally use gray decoration (the calendar month grid, evcc's flow boxes) treat every palette above two tones as gray instead of comparing with four. Dithering in xkcd, pokedex and myskoda derives its step from `render.Levels`, so it spreads over 16 tones as well.
5. **The QOI limit follows the firmware buffer: 512 KiB for the 800×480 panels, 1 MiB for the E1003.** ESPHome allocates the entire `max_response_buffer_size` before it reads a byte (`http_request.h`) and copies the answer into a second string, so the size is paid on every download. The E1003 has 8 MB PSRAM, runs with `CONFIG_SPIRAM_USE_MALLOC` (both buffers live there) and its pages are 190–205 KB, so 1 MiB leaves room for a photograph that is dithered (about 1.5 MB, simplified by the median filter) without touching the 1.31 MB framebuffer. The 800×480 panels keep 512 KiB; their pages cannot exceed 384 KB. A page above the limit of its hardware is simplified by the median filter or refused (ADR 0088), which the dashboard shows.
6. **No new protocol action and no per-display configuration.** The number of tones is a property of the display type; plugins, pages and plans stay unchanged.

## Alternatives

- **A fixed 16-tone palette for every display:** would waste memory and draw 4-level panels through a 16-tone image, only to quantize it back down in the firmware.
- **Arbitrary palette sizes:** `gray_levels: 8` has no panel behind it; only 1, 4 and 16 are accepted, and `NewCanvas` falls back to the next supported count for other values.
- **Bumping the minimum protocol version:** would refuse every older plugin for every display, although only a 16-level display needs the new value. The error of the old plugin names the problem.
- **Dithering in `Quantize`:** stays out of the shared page composition; plugins that need it (photographs) dither themselves, text and lines stay crisp.

## Consequences

- A 16-level display type composes pages at 16 tones without any plugin change in how it is configured. Antialiased fonts and shaded fills use the extra tones.
- A 16-level page of the E1003 is composed at 16 tones and sent within 1 MiB, so ordinary pages arrive as drawn; a dithered photograph is simplified by the median filter like any other page above the limit. The extra tones still show in text, rules and fills.
- The display type API changes `gray` (boolean) to `levels` (number); the shipped editors and the `/api/profiles` model follow. There is no migration because display types are files of their own (ADR 0071).
- The E1003 is the first panel that uses 16 tones; the E1001 keeps four and the Sticky four.
