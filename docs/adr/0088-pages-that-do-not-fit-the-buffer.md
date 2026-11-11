# 0088: Pages that do not fit the firmware's buffer

- Status: Accepted, implemented
- Date: 2026-11-12
- Builds on: ADR 0072 (pages as QOI)

## Context

A device fetches its page as QOI into a response buffer
(`max_response_buffer_size` in `common.yaml`), a compile-time constant that
`http_request` allocates for each fetch. A larger answer is truncated, and the
firmware keeps the old picture and reports `page not paintable` (ADR 0072).

The buffer was 96 KiB, chosen from pages that were 22–50 KB. QOI grows with the
content, not with the display: a whole row of one colour becomes a run, but a
pattern that changes from pixel to pixel costs about a byte per pixel. Two
things Paperboy draws itself are close to that:

| Content | QOI, 800×480, four levels |
|---|---|
| page with nine hatched notice cells | 93.7 KB |
| full-page `hatch` (`(x+y)%4`, one pixel in four) | 281 KB |
| full-page `shade` on a two-level palette (a checkerboard) | 375 KB |
| dithered photo in a 400×300 cell | 72 KB (105 KB with two levels) |
| dithered photo filling the page | 233 KB (325 KB with two levels) |
| ordinary text and table pages | 15–50 KB |

So an error page was just under the limit and over it with one more cell, and
dithered artwork (Pokédex, MyŠkoda) exceeds it in normal operation on a
two-level display.

## Decision

- The fill patterns become rows: `hatch` is a one-pixel rule every four rows and
  `shade` on a two-level palette is every other row instead of a checkerboard.
  Ink coverage and look stay the same; the notice page drops to 6.2 KB, a
  full-page hatch to 6.4 KB and a full-page `shade` to 6.6 KB.
- The firmware buffer grows to **512 KiB**, as much as the page path needs and
  no more than PSRAM gives for free: a page of Paperboy's palette costs at most
  one QOI byte per pixel, because every colour has its own slot in QOI's index,
  so 800×480 tops out at 384 KB (measured: 384,028 bytes for alternating black
  and white). A page can therefore never exceed the buffer, whatever a plugin
  draws.
  - Both image displays have 8 MB PSRAM (octal), and ESP-IDF's default
    `SPIRAM_USE_MALLOC` lets `malloc` use it. `http_request` allocates with
    `heap_caps_malloc_prefer`, PSRAM first.
  - The response is moved into `pb_page_qoi` (`std::move(body)`) instead of
    copied, which keeps the peak at twice the buffer (the raw read buffer plus
    the string) instead of three times.
  - The internal RAM of an ESP32-S3 (512 KiB) cannot hold this; PSRAM must stay
    configured for the image displays. The text display does not fetch pages at
    all.
- Paperboy keeps its own limit at the same size (`qoiLimit` in `server.go`). A
  page above it — a display type with many more pixels, or a future format —
  loses its dither pattern first (a 3×3 median: it keeps text, rules and fills
  and flattens the noise, which is what makes such a page large) and is logged
  once per page. If even that is too large it is sent anyway and the firmware
  keeps its old picture, as before.
- No scaling and no block painting: with this buffer there is nothing to shrink
  for the shipped panels, and a halved page would blur text that is fine.

## Consequences

- Every page of every shipped display reaches the panel, in full quality, and
  the lossless QOI of a normal page is unchanged (22–50 KB).
- Peak heap during a fetch is about 1 MB in PSRAM; the buffer stays allocated
  with its content until the next fetch. PSRAM is powered down in deep sleep, so
  a battery device gains nothing to worry about.
- A page that is genuinely large now travels in full: the 96 KiB limit also kept
  the transfer short. A 380 KB page takes roughly 1–2 s over the LAN instead of
  ~0.2 s, on top of the ~2 s panel refresh.
- The firmware needs an update (`fw_version` bump), which Paperboy offers in the
  sync (ADR 0043). `qoiLimit` and `max_response_buffer_size` must stay in step.
- A future panel with far more pixels would need a bigger buffer again or the
  band-wise fetch named in ADR 0072; the median fallback covers the transition,
  not a permanently oversized page.
