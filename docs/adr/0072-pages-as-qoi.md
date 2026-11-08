# 0072: Pages as QOI for the firmware

- Status: Accepted, implemented
- Date: 2026-11-08
- Builds on: ADR 0042 (device API), ADR 0049 (pages), ADR 0057 (timings)

## Context

A page change on the Sticky spends about 4.9 s: sync 0.2 s, image 2.6 s, paint
2.1 s. The `timing` lines of the firmware (ADR 0057) show that the image step is
suspicious: 4.5 KB and 13.6 KB take the same 2.6 s, and a separate plain GET of
the same URL answers in 34–61 ms (190–290 KB/s, the server itself in 1.5 ms).
The transfer is therefore not the problem, the PNG decoder is: it has to inflate
and unfilter a frame that is always 800×480, whatever the file size.

ESPHome 2026.10.1 decodes PNG, JPEG, BMP and QOI (`runtime_image`). QOI is a
lossless format that is decoded by a plain byte loop without zlib and without
Huffman coding; on the Sticky that is a few hundred milliseconds instead of
2.6 s. Measured on real pages: a page is 7–14 KB as PNG and 36–50 KB as QOI, so
the transfer grows by about 150 ms. BMP would decode just as fast but costs
385 KB per page at 8 bit and cannot compete.

## Decision

- The rendered page and hint endpoints (`/pages/{profile}/{page}/{file}` and
  `/hints/{file}`) answer QOI when the request carries `?format=qoi`, with
  `Content-Type: image/qoi`. Without the parameter they keep answering PNG, so
  the dashboard, the plan preview and browsers are unaffected.
- The device keeps the relative path and the hash from the sync answer, so the
  `shown` hash is still the hash of the PNG: the encoding is not part of the
  page identity, and a device that misses a change still catches up.
- The firmware asks for `?format=qoi`, fetches the page with a plain
  `http_request.get` into a 96 KiB buffer and decodes it into the panel buffer
  itself (`pb_qoi_each` in `paperboy.h`). ESPHome's decoders are not used any
  more: QOI through `online_image` still took 2.26 s for the same page, so the
  per-pixel work inside its decoder is the cost, not the format. That also drops
  the 384 KB image buffer and the `online_image` component.
- Not `format: AUTO` for a fallback: it would enable every decoder including
  JPEG and its external library. A Paperboy always offers the firmware it
  serves, so a device never meets a Paperboy that only speaks PNG.
- 96 KiB covers the pages measured so far: an 800×480 page with four levels
  encodes to 22–50 KB, the worst case being dithered artwork. `http_request`
  truncates a larger answer, so the firmware validates the stream completely
  before it paints anything: a page that does not fit keeps the old picture,
  reports `page not paintable: <path>` in the next sync's log, and Paperboy
  warns in its own log (`page is too large as QOI for the firmware`). The
  escape hatch, should a real page ever exceed the limit, is fetching the page
  in horizontal bands.
- The encoder lives in `internal/paperboy/qoi.go`; it always writes four
  channels, so an image with transparency round-trips, and the tests decode its
  output with a reference decoder.
- `buffer_size_rx: 2048` in `http_request` stays: it does not change the page
  time, but it halves the number of blocking reads.

## Consequences

- A page change drops from about 4.9 s to about 2.2 s (measured 2026-11-08:
  image 69–112 ms, paint 1984–1991 ms, sync 87–254 ms), and a wake from deep
  sleep from 9.5 s to 3.7 s together with `fast_connect`. What is left is the
  panel's full refresh, which is physical (about 2 s at 10 MHz for four gray
  levels).
- QOI has to be implemented and kept in Paperboy; there is no Go library in the
  standard environment and the format is small enough (a page is a few hundred
  lines of encoder).
- The device downloads three to four times more bytes per page. Over the LAN at
  190–290 KB/s that is ~150 ms; over a slow link (a VPN or a weak access point)
  the trade-off could flip and the PNG path is still served for browsers and
  previews.
- The firmware carries a QOI decoder of its own (about 100 lines) and loses the
  generic `online_image` path, so a future format needs firmware work again. In
  exchange it needs no 384 KB image buffer and no per-pixel conversion inside
  ESPHome.
- If QOI turns out to be too large for a page with much structure, BMP and a
  cheaper PNG bit depth are the alternatives; both are the firmware's choice
  only, because the format travels as a query parameter.
