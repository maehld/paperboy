[← Design documents](README.md) · [Paperboy](../../README.md)

# Devices

A device is one panel with Paperboy's firmware. It identifies itself by MAC, keeps a key, and asks for a page every time it wakes.

## Display types

A display type is a file under `displays/<name>.yaml` (ADR 0071). The file name is the stable name that pages and devices use.

| Key | Meaning |
|---|---|
| `kind` | `image` or `text` |
| `size` | Width and height, in pixels for image types and in characters for text types |
| `gray_levels` | 1, 4 or 16 for image types (ADR 0105) |
| `ppi` | Pixels per inch, 20–1000, for image types |
| `charset` | `utf8`, `ascii` or `hd44780` for text types |
| `min_refresh` | Fastest refresh the panel accepts, in seconds |
| `power` | `auto`, `battery` or `mains` (ADR 0051) |
| `rotation` | 0, 90, 180 or 270 for image types: the quarter turn from the page to the panel (ADR 0102) |

The loader and the API validate with the same function. An image type needs `ppi` and rejects `charset`. A text type allows at most 80 × 24 characters and rejects `ppi` and `gray_levels`. A leftover `width_mm` is rejected with a hint: the physical size follows from `ppi` (ADR 0050).

## Device files

A device is a file under `devices/<id>.yaml` (ADR 0070). The file name is the stable ID.

| Key | Meaning |
|---|---|
| `display_type` | Name of a file under `displays/` |
| `protocol` | `paperboy`; OpenDisplay is not delivered yet |
| `mac` | The MAC the device registers with; one device per MAC |
| `plan` | Name of a file under `plans/` |
| `refresh` | Seconds between syncs, down to 1 |
| `power` | Overrides the display type (ADR 0051) |
| `actions` | `allow` (default) or `deny`: whether `home` opens the menu |
| `menu_show_duration` | Seconds until the menu expires, at most 60 (ADR 0057) |
| `touch` | Draws the menu's tap controls (ADR 0082) |
| `rotation` | Overrides the display type's rotation (ADR 0102) |
| `battery` | Thresholds of the battery warning |
| `firmware` | `hold` keeps the device on its firmware |
| `address`, `buttons` | Reserved for OpenDisplay |

A device whose `display_type` is not configured still loads, with a `Problem` set (ADR 0079). Its sync and text poll answer 503 with the reason, never 401, so the firmware keeps its key and cannot loop through registration.

## The device API

Both routes are open; a device proves itself with the key from registration (ADR 0042, 0064).

**`POST /api/device/register`** takes `mac`, `hardware` and `firmware`. A configured MAC gets `{key, device}`, and an existing key stays. An unknown MAC gets 403, but only after Paperboy wrote `state/devices/<MAC>.json` with hardware, firmware and the first and last contact. The devices tab shows it as unclaimed hardware (ADR 0080). `DELETE /api/state/devices/{mac}` forgets it.

**`POST /api/device/sync`** requires the bearer key and answers 401 without it; the firmware then drops its key and registers again. The body is strict:

| Field | Content |
|---|---|
| `reason` | `timer`, `button`, `touch`, `powercycle`, `menu` or `result` |
| `events` | Button roles `next`, `previous` and `home`, in order, each with a monotonic `id`; `hold_ms` for long presses |
| `shown` | Hash of the page the device displays, which guards the confirmation screen |
| `battery` | `volts` (required), `percent`, `mah`, `ma` (positive while charging) and `charging` |
| `rssi` | Signal strength |
| `log` | Lines that Paperboy writes one by one; a repeated entry carries `time` and `id` and is written once |
| `power` | `battery` or `mains`, if the device can tell |

Events are applied in order before the page is chosen. `previous` wraps. Touches operate the menu when it is open and are only logged otherwise. Paperboy drops an event `id` that is not newer than `last_event_id` in the state file. A repeated request therefore cannot move the cursor twice (ADR 0064).

The answer carries `image` with a relative `path` and its `hash`, `refresh_s`, `wake` with `reason` and `after_s`, `listen_s`, `sleep` and `time`. It also carries `rotation` when it is not 0, and the firmware offer of ADR 0043 or `null`.

## Timing

The refresh has a floor per power source (ADR 0051):

- Battery: at least one minute, and at least `min_refresh`.
- Mains: `min_refresh`, which defaults to 1 second for text types and 10 seconds for image types.

A device's `refresh` may go down to one second and is clamped at runtime. `sleep: false` tells a mains device to stay awake and sync again after `refresh_s`.

`listen_s` is 0 for a timer wake and 20 seconds after input, so the firmware can wait for the next press without registering again. While a menu is open, `listen_s` shortens so that `listen_s` plus `wake.after_s` cover the time left until the menu expires (ADR 0078).

`wake` is a one-shot next wake: the menu deadline or an action result. The firmware uses it once and goes back to its normal cycle.

After `page_timeout` the device returns to the first page of its rule.

## State

`state/devices/<MAC>.json` holds everything the service knows about a device (ADR 0080):

- the page cursor and the rule it came from
- the last battery reading, the last charging episode and the hourly battery samples of the last week
- the menu, with its phase, selection, session and confirmation hash
- the address of the last contact (ADR 0083)
- `last_event_id`, the reported power source and the key

State belongs to the MAC, not to the file name. A renamed device keeps its cursor, its battery reading and its menu (ADR 0079). A lost `state/devices/` costs only that: the device registers again and starts at its first page.

## Power and battery

The charger and the power source drive three things: the refresh floor, the charge state on the dashboard, and how long the device lasts.

- `power: auto` uses the report of the device and falls back to `battery`. The report is stored as `reported_power`.
- A change of the report or of the charger state gets one `power changed` journal line instead of one per sync.
- Below `warn_volts` the battery warning draws a bar on plan pages. Below `critical_volts` it replaces the page with a full-screen warning, with `hysteresis_volts` to avoid flapping (ADR 0020).
- The dashboard reads the last charge from the gauge current (`charge.go`). It estimates the runtime from the hourly samples (`runtime.go`): the gauge's capacity first, then its percentage, then the voltage, with a least-squares slope.

## Pages to the device

A composed page is served as `/pages/{display type}/{page}/{hash}.png`, or as QOI with `?format=qoi` for the firmware (ADR 0072). Both carry the device key and only serve the display type of the downloading device (ADR 0097). A notice is `/hints/{hash}.png` in the same two formats.

- Rendered pages live in memory only. After a restart the dashboard omits `last_page_url` and falls back to the page preview.
- QOI never exceeds the firmware's 512 KiB response buffer. No page of the shipped panels can: a paletted page costs at most one QOI byte per pixel (ADR 0088). A page above the limit loses its dither and logs a line.
- `rotation` turns the page onto the panel: 0, 90, 180 or 270 degrees clockwise, omitted when 0 (ADR 0102). The firmware applies it to the image and to the touch coordinates.

Text displays poll instead: they fetch their lines every `refresh_s`, and an unchanged page answers 304 (ADR 0044, 0045).

## Firmware

The firmware lives in `firmware/esphome/` and is built on ESPHome (ADR 0041).

| File | Content |
|---|---|
| `common.yaml` | Register, sync, download, listen, deep sleep |
| `paperboy.h` | Sync body, event queue, wake helpers, rotation, QOI paint, the lockup of the setup screens |
| `sticky.yaml`, `e1001.yaml`, `e1003.yaml` | Hardware, buttons, panel driver, battery and charger |
| `text-common.yaml`, `lcd2004.yaml` | Text displays |
| `build.ps1` | Build with a pinned ESPHome (2026.10.1); `-Instance` publishes `firmware/<hardware>-v<version>.bin` |

A cycle runs one at a time; a second trigger is dropped, so it cannot move the page cursor behind a running download. The firmware connects to the saved BSSID and channel without a scan. It decodes QOI itself into the panel buffer, because ESPHome's decoders spend 2.2–2.6 seconds on 384k pixels. Input during sync or paint is queued for one follow-up sync with a 150 ms settle, so a burst of presses becomes one sync. Failures are noted in RTC memory and reported in the next sync's `log`.

An `https://` Paperboy URL is verified against the ESP-IDF CA bundle, which contains the Let's Encrypt roots (ADR 0108). `http://` in the LAN stays unencrypted. The check needs a clock. SNTP provides it after a power-on, the RTC keeps it across deep sleep, and the sync answer sets it from `time`. A device without a clock shows the setup screen with the reason.

The only update path is `ota: http_request` with the offer from Paperboy. There is no native ESPHome OTA and no upload in the web UI; a rescue needs USB.

## Hardware findings

Facts from the hardware, kept because they cost time to find.

**E1001** (7.5 in, 800 × 480): the panel is a GDEY075T7 with a UC8179 controller and four gray levels. ESPHome's preset drives it in black and white with a fixed 2 × 2 dither. That puts a dot ring around every antialiased glyph. Since firmware 2.4.0 the pinned external component `uc8179_gray4` draws the four levels directly (ADR 0086). Data polarity and LUT bank differ per batch and are set per device in the web UI (ADR 0087). Buttons left to right: GPIO5 `previous`, GPIO4 `next`, GPIO3 (green) `home`. Battery on ADC GPIO1 ×2 with enable GPIO21, mapped with Seeed's E10xx curve. USB power is not visible to the ESP32, so an E1001 on a charger needs `power: mains`.

**E1003** (10.3 in, 1872 × 1404, 227 ppi): 16 gray levels, IT8951 controller with a 3 s full refresh. ESP32-S3 with 8 MB PSRAM. Buttons: green GPIO3 `home`, right white GPIO4 `next`, left white GPIO5 `previous`. Battery on ADC GPIO1 ×2 with enable GPIO40, SY6974B charger on I2C 0x6B for `power` and `charging`. Touch is a GT911 on I2C GPIO19/20 without a transform. The preset forces 16 °C and a mirrored X. Deep sleep needs GPIO11/21/38/39/40/45/48 held low and I2C disabled, otherwise the device draws about 4 mA. The image sets `CONFIG_SPIRAM_USE_MALLOC` so that the response copy fits. Measured since 0.1.2: nine pages cycled, sync 0.7 s, paint 1.9–2.7 s.

**Sticky**: gray4 panel that bands luma at 32/96/160, so the firmware maps 0/85/170/255 into the bands. Touch needs `swap_xy`, `mirror_x`, `mirror_y` and rotation 180. Power latch on GPIO45/46 through deep sleep, BQ27220 gauge with currents and capacities ×10, BQ25616 `STAT` on GPIO40 reported as `charging`, charge enable GPIO39.

**Battery readings** for calibration: the Sticky "Küche" reads 3.40 V after a long time without charging. The E1001 "Wohnzimmer" reads 3.75 V. Both fluctuate by about 0.02 V.

**OpenDisplay** is reachable over TCP and Bluetooth (`internal/opendisplay`, `cmd/odtest`), but the service does not deliver to OpenDisplay devices yet. `cmd/odtest -buttons` polls the 16-byte status block (`0x0044`). Byte 5 holds the last pressed button (0 = top, 1 = page up, 2 = page down). Each button has its own 4-bit counter, so a short press often shows only as a counter change. Touch contacts 1–5 mean touched and 6 released. Flags 0b111 invert and swap the coordinates, whose range is not calibrated yet.

## Decisions

ADR 0020 (battery warning) · 0041 (firmware) · 0042 (device protocol) · 0043 (firmware distribution) · 0044, 0045 (text displays) · 0051 (power source) · 0064 (protocol v4) · 0070, 0071 (device and display type files) · 0072, 0088 (QOI and the buffer) · 0079 (a missing display type) · 0080 (state per MAC) · 0083 (address) · 0086, 0087 (gray levels and panel settings on the E1001) · 0097 (the device key on downloads) · 0098 (lockup and version) · 0102 (rotation) · 0105 (16 gray levels)
