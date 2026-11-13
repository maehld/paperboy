# Paperboy firmware on ESPHome

Firmware for the Seeed reTerminal **Sticky** and **E1001** (ADR 0041) and for
HD44780 **text displays** (`lcd2004`, ADR 0044). It speaks
Paperboy's device API (ADR 0042); one image per hardware type serves every
installation, nothing installation-specific is compiled in.

| File | Content |
|---|---|
| `common.yaml` | Shared cycle and device API (register, sync, download, sleep), included as a package |
| `paperboy.h` | C++ helpers: sync body, event and log queue, RTC state, gray-level mapping, dithering (mono fallback), text lines and HD44780 character mapping |
| `sticky.yaml` | reTerminal Sticky: pins, gray4 panel, touch, BQ27220 fuel gauge |
| `e1001.yaml` | reTerminal E1001: pins, four-level UC8179 panel via `uc8179_gray4`, ADC battery |
| `text-common.yaml` | Shared cycle of text displays: register, sync, text poll at the plan's `refresh_s`, no sleep; a row whose cell set `scroll: true` moves one character per 300 ms (ADR 0092) |
| `lcd2004.yaml` | ESP32 dev board with a 20×4 HD44780 LCD behind a PCF8574 (I²C 0x27, SDA GPIO21, SCL GPIO22); prints every row through `pb_row`, so a scrolling line shows its window |
| `fonts/` | Go Mono Bold as a subset, the face of the logo's wordmark and the firmware version (ADR 0098), with its licence |
| `build.ps1` | Builds one hardware type with the pinned ESPHome version, optionally publishes it to an instance |

## Cycle

| Step | Request | Notes |
|---|---|---|
| Power-on | none | the lockup of the logo and the firmware version in the middle of the panel for 5 s (ADR 0095, 0098); a timer or button wake gets no splash, so a page change stays as fast as it is |
| First contact | `POST /api/device/register` with `mac`, `hardware`, `firmware` | the key is kept in flash; `403` shows "Paperboy does not know this display" with the MAC |
| Every wake | `POST /api/device/sync` with `reason`, `events`, `shown`, `battery`, `rssi`, `power`, `log` | `401` drops the key, registers again and repeats the sync; a failed request keeps the event queue and is retried up to three times (ADR 0064) |
| No key | `POST /api/device/register` is retried three times as well | without a key and with an unreachable server the device shows the setup screen with its address and the URL it has, instead of sleeping silently |
| Unchanged content | `image.hash` equals the hash on the panel | no download, no paint |
| Firmware offer | `firmware` in the sync answer (ADR 0043) | flash `<Paperboy URL><path>` with its MD5 and reboot; a failure is reported in the next sync's `log` |
| Changed content | `GET <Paperboy URL><image.path>` | PNG, painted with the panel's gray mapping |
| After painting | the shown hash is stored only now, and input that arrived meanwhile is sent in one more sync | a confirmation screen can only be confirmed after the panel really shows it |
| Settle | presses that arrived during a paint wait 150 ms before that follow-up sync | a burst of presses becomes one sync instead of two cycles (issue #136) |
| Sleep | stay awake `listen_s`, then deep sleep for `refresh_s` or a one-shot `wake` | wake on any button (EXT1) |
| Mains power | the sync answers `sleep: false` (ADR 0051) | stay awake with Wi-Fi up, sync again after `refresh_s` as a `timer` wake; input syncs at once |

Every step of a cycle is logged with its time:
`timing boot: 412 ms (+0)`, then `wifi`, `sync`, `image` (download and decode)
and `paint` (panel done), each with the milliseconds since boot and the delta
to the line before. A button press wakes the chip from deep sleep, so these
numbers start at the press and show where a page change spends its time;
`unchanged` replaces `image`/`paint` when the hash still matches the panel.

Measured on the Sticky (firmware 2.1.0, 2026-11-08): boot 228 ms, Wi-Fi 4.0 s,
sync 0.2 s, image 2.6 s, paint 2.1 s — about 4.9 s per page change. Wi-Fi and
image dominated. A plain GET of the same page took 34–61 ms while the same bytes
through `online_image` took 2.6 s whatever their size, and QOI through it still
took 2.26 s: the per-pixel work of ESPHome's decoders was the cost, not the
transfer or the format (the frame is always 800×480). The firmware therefore
fetches the page itself (`http_request.get` into a 512 KiB buffer in PSRAM) and
decodes it into the panel buffer with `pb_paint_qoi_gray4`/`pb_paint_qoi_dithered`
(ADR 0072, 0088); `online_image` and its 384 KB image buffer are gone. Together with
`fast_connect` (saves BSSID and channel, no scan before every wake) a page
change is now: Wi-Fi 1.2 s, sync 0.1 s, image 0.07–0.11 s, paint 2.0 s — 3.7 s
from a battery wake instead of 9.5 s, and the panel refresh is 90 % of it. The
page travels as QOI, 22–50 KB for a four-level 800×480 page.

The buffer is a compile-time constant and 512 KiB big, since the boards have
8 MB PSRAM and the firmware reads the answer into it before painting. A page of
Paperboy's palette costs at most one QOI byte per pixel, so even an 800×480 page
of alternating black and white (the worst case, measured 384 KB) fits; Paperboy
keeps its own `qoiLimit` in step and only has to simplify a page for a display
with many more pixels (ADR 0088). The response is moved into `pb_page_qoi`
(`std::move(body)`), which keeps the peak at twice the buffer: the raw read
buffer plus the string.

Only one cycle runs at a time. A second trigger — the flash-restored Paperboy
URL at boot used to start one while `on_boot` was already running the cycle —
is dropped with `a cycle is already running, keeping its page`. Otherwise the
second sync moves Paperboy's page cursor (with `rotate` in the plan) while the
first image is still downloading, and the panel never shows that page.

The firmware reports its power source in every sync (ADR 0051). The Sticky
measures USB power on GPIO9 (`PWR_IN_VOLT`, 5.1k/5.1k divider: about 2.5 V
with USB) and reports `mains` above 1.5 V at the pin; the E1001 has no pin that
sees USB power and always reports `battery`, so an E1001 on a charger needs
`power: mains` in `paperboy.yaml`; text displays report `mains`.

Buttons are sent by role. Sticky: **down = `next`, up = `previous`, ok =
`home`**. E1001 from left to right: **`previous`, `next`, `home` (green)**.
Every event carries a monotonically increasing `id` and stays in an RTC queue
until Paperboy confirms the sync (ADR 0064), so a retry is applied exactly
once. The queue is appended to while a request is in flight, and only the
events that request carried are dropped from the answer — a press made during
the sync is not swallowed (issue #136). A press is queued when the button is
released or after 700 ms while it is held, with the measured `hold_ms`; what
counts as a long press is decided by Paperboy (ADR 0067). The Sticky sends taps,
not the continuous touch stream: `pb_touch` takes the first report of a contact
and drops the rest until `on_release`, so one tap is one event and a deliberate
second tap is not swallowed; Paperboy maps the tap onto the device menu
(ADR 0057, issue #54). A failed download, sync or
Wi-Fi connection is kept in RTC memory and reported in the next sync's `log`.
After power-on the device stays awake 3 minutes so its web UI can be used; a
limit of 120 s after the last input forces deep sleep otherwise.

## Settings on the device

| Setting | Source | Kept in |
|---|---|---|
| Wi-Fi | captive portal on the hotspot `paperboy-<id>`, or Improv over USB | flash (ESPHome) |
| Paperboy URL | device web UI `http://<device>/`, field "Paperboy URL" (`default_paperboy_url` can preset it); saving it starts the cycle at once | flash |
| Device key | `POST /api/device/register` by MAC | flash |

While Wi-Fi or the Paperboy URL is missing, or Paperboy does not know the MAC,
the display shows instructions with the device's address and MAC, the lockup of
Paperboy's logo and the firmware version above them, and stays awake for up to
10 minutes before retrying in an hour. The same screen appears when the URL is
set but no server answers there; it then also prints the URL the device has, so a
typo is visible without a serial log. A power-on shows the lockup and the version
alone for 5 s before the cycle starts (ADR 0095, 0098); the mark comes from
`pb_logo_ink` in `paperboy.h`, the same geometry the `logo` widget of the
built-in plugin draws, so no bitmap sits in flash. The wordmark and the version
use Go Mono Bold from `fonts/` (a subset, see [`fonts/README.md`](fonts/README.md))
because the face is not on Google Fonts. The splash drops the shown hash on
purpose: after a firmware update the RTC memory survives the reboot, and an
unchanged page must still repaint over the lockup.

## Text displays

`lcd2004` runs on mains power and never sleeps. After registering it syncs at
boot and every `refresh_s`, at most once a minute (paging, log, firmware
offers), and polls `GET /api/device/text?shown=<hash>` every `refresh_s`, which
the plan decides (ADR 0045; one minute until the first sync, registration is
retried once a minute). Unchanged text is a `204` and leaves the LCD alone;
ESPHome would log any non-2xx answer as a failure. Setup instructions (Wi-Fi hotspot, address for
the Paperboy URL, unknown MAC) appear on the LCD itself. Paperboy converts the
text for `charset: hd44780` and keeps only `ä ö ü ß ° µ`, which the firmware
maps to the A00 character ROM (`pb_hd44780`); a display with the A02 ROM would
show other glyphs for them.

The display type as `displays/lcd2004.yaml` (ADR 0071):

```yaml
kind: text
size: [20, 4]
charset: hd44780
```

A display that still runs a Home Assistant configuration with native ESPHome
OTA can take `firmware.ota.bin` once through that OTA (`esphome upload` with
the old configuration's address and password) or over USB; afterwards only
Paperboy's offers update it.

## Building and flashing

ESPHome is pinned to **2026.10.1** and runs through `uvx`, outside Paperboy.

```powershell
cd firmware/esphome
.\build.ps1 -Hardware sticky                      # or e1001; image in .esphome/build/<hardware>/build/
uvx esphome==2026.10.1 run sticky.yaml            # first flash over USB-C
.\build.ps1 -Hardware sticky -Instance <dir>      # publish to <dir>/firmware/sticky-v<version>.bin (ADR 0043, 0060)
```

The build needs no secrets. Over the network the firmware only installs images
that Paperboy offers in the sync (ADR 0043); there is no native ESPHome OTA and
no upload in the web UI, so nobody on the LAN can push an image. Everything
else, including the rescue of a device that no longer syncs, goes over USB-C.

`e1001.yaml` pulls the `uc8179_gray4` component from GitHub (ADR 0086), so its
build needs network access once; ESPHome keeps the checkout under
`~/.cache/esphome` and reuses it for a day (`refresh`). The component is pinned
to a commit, and the *Firmware* workflow fetches it the same way.

The *Firmware* workflow compiles all three hardware types with the same pinned
version whenever `firmware/esphome/` changes, so a broken configuration fails on
a pull request. A release tag attaches the images as one bundle,
`paperboy-firmware.zip`: unpacked in the instance directory it creates
`firmware/<hardware>-v<version>.bin` for every hardware type (ADR 0043, 0059,
0060, 0063). `fw_version` in `<hardware>.yaml` names the image inside the bundle,
the release tag names the release.

A device that runs the TRMNL firmware is flashed over USB-C once. A device on
the ESPHome experiment can switch without a cable through the experiment's own
manifest update: serve the built `firmware.ota.bin` together with a
`manifest.json` (`{"version": "0.4.0", "builds": [{"ota": {"path":
"firmware.ota.bin", "md5": "<md5>"}}]}`), enter its URL as "Firmware manifest
URL" in the device's web UI and press a button. Its key stays valid: Paperboy
hands out the existing key for the MAC.

After flashing, join the hotspot `paperboy-<id>`, pick the Wi-Fi network, then
open the address shown on the display and enter the Paperboy URL (for example
`http://10.1.1.2:8090`). Configure the device in `paperboy.yaml` with
`protocol: paperboy` and its `mac`.

Raise `fw_version` in `<hardware>.yaml` for every release and publish it with
`-Instance`; Paperboy offers it in the sync once `firmware.update: auto` is set
(ADR 0043). ESPHome keeps the previous image and `safe_mode` rolls back one
that does not boot; that was tested with a deliberately broken image. Restoring the original firmware: Seeed's playground (Sticky) or the SenseCraft/TRMNL flasher
(E1001).

## Hardware facts

**Sticky**: ESP32-S3R8, 8 MB PSRAM, 32 MB flash; panel SSD1677 800×480, ESPHome
model `Seeed-reTerminal-Sticky-gray4`. ESPHome bands gray4 luma at 32/96/160,
so Paperboy's 0/85/170/255 are drawn inside the bands (`pb_paint_qoi_gray4`).
Buttons OK GPIO4, Up GPIO5, Down GPIO6 (active low). Touch GT911 on I²C
GPIO2/3, INT GPIO21, RST GPIO41, power GPIO42; needs `swap_xy`, `mirror_x` and
`mirror_y` with `rotation: 180`. Power latch GPIO45/GPIO46 held through deep
sleep. BQ27220 fuel gauge at 0x55 on I²C GPIO0/1: state of charge 0x2C,
voltage 0x08, current 0x0C, remaining capacity 0x10; currents and capacities
read ten times too large and are divided by ten. The BQ25616's charge enable
(`EN_BAT_CHGn`, GPIO39) is active low: the firmware drives it low at boot and
holds it through deep sleep (like the power latch), because Seeed's Arduino and
ESPHome examples do the same and a floating pin leaves charging to chance. Its
`STAT` (GPIO40) is read with a pull-up and reported as `battery.charging`, so
Paperboy can cross-check the fuel gauge current.

**E1001**: ESP32-S3, octal PSRAM; panel GDEY075T7 (UC8179) 800×480 with four gray
levels (2 bpp, LUTs, full refresh only, a few seconds). Seeed's ESPHome model
`Seeed-reTerminal-E1001` drives it in black and white, so the firmware uses the
external component `uc8179_gray4` instead (ADR 0086): it takes Paperboy's
0/85/170/255 as black, dark gray, light gray and white, and the antialiased font
edges of a page survive. That component is fetched at build time and pinned to a
commit in `e1001.yaml`.

The battery is read through the ADC on the divider (GPIO1, enabled by GPIO21).
There is no fuel gauge, so the firmware maps the voltage to a percentage with
Seeed's discharge curve for the E10xx (`3.27 V` = 0 % … `4.15 V` = 100 %,
linearly interpolated) and sends it as `battery.percent`. The ADC measures under
the device's own load, so the percentage reads a little low while it is awake;
the voltage stays in the sync for the battery warning.

Two properties differ between panel batches and belong to the device, not the
image (ADR 0087); both are set in the device's web UI and survive a wake:

| Setting | When |
|---|---|
| *Invert colors* (off by default) | The page is a negative: some batches drive the data planes with the opposite polarity. |
| *LUT mode* (`otp`, or `custom` when the image was built with that) | The four levels look washed out, swapped or inverted with one bank. |
| *Apply display settings* (button) | Repaints the current page, so a change is visible at once. |

An unchanged page is not repainted by Paperboy, so without the button a changed
setting would wait for the next page change. The driver refreshes
synchronously, so the main loop pauses during a paint and presses in that window
are not queued, and it has no partial-update mode.
Buttons left to right GPIO5 (`previous`),
GPIO4 (`next`), GPIO3 (green, `home`). Battery voltage on ADC GPIO1 through a 1:2 divider,
enabled by GPIO21; no fuel gauge.

Measured on the experiment: paint 2.7 s, PNG decode about 1.8 s for 5 KB,
timer wakes reliable over 10 h on the Sticky.
