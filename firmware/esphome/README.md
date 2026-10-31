# Paperboy firmware on ESPHome

Firmware for the Seeed reTerminal **Sticky** and **E1001** (ADR 0041). It speaks
Paperboy's device API (ADR 0042); one image per hardware type serves every
installation, nothing installation-specific is compiled in.

| File | Content |
|---|---|
| `common.yaml` | Shared cycle and device API (register, sync, download, sleep), included as a package |
| `paperboy.h` | C++ helpers: sync body, event and log queue, RTC state, gray-level mapping, dithering |
| `sticky.yaml` | reTerminal Sticky: pins, gray4 panel, touch, BQ27220 fuel gauge |
| `e1001.yaml` | reTerminal E1001: pins, black-and-white panel, ADC battery |
| `build.ps1` | Builds one hardware type with the pinned ESPHome version, optionally publishes it to an instance |

## Cycle

| Step | Request | Notes |
|---|---|---|
| First contact | `POST /api/device/register` with `mac`, `hardware`, `firmware` | the key is kept in flash; `403` shows "Paperboy does not know this display" with the MAC |
| Every wake | `POST /api/device/sync` with `reason`, `events`, `shown`, `battery`, `rssi`, `log` | `401` drops the key, registers again and repeats the sync |
| Unchanged content | `image.hash` equals the hash on the panel | no download, no paint |
| Firmware offer | `firmware` in the sync answer (ADR 0043) | flash `<Paperboy URL><path>` with its MD5 and reboot; a failure is reported in the next sync's `log` |
| Changed content | `GET <Paperboy URL><image.path>` | PNG, painted with the panel's gray mapping |
| After painting | input that arrived meanwhile is sent in one more sync | Paperboy renders only the final page |
| Sleep | stay awake `listen_s`, then deep sleep for `refresh_s` | wake on any button (EXT1) |

Buttons are sent by role: **down = `next`, up = `previous`, ok = `home`** on
both devices (E1001 from left to right: down, up, ok/green). Sticky touches are
sent as `touch` events; Paperboy only logs them for now. A failed download,
sync or Wi-Fi connection is kept in RTC memory and reported in the next sync's
`log`. After power-on the device stays awake 3 minutes so its web UI can be
used; a limit of 120 s after the last input forces deep sleep otherwise.

## Settings on the device

| Setting | Source | Kept in |
|---|---|---|
| Wi-Fi | captive portal on the hotspot `paperboy-<id>`, or Improv over USB | flash (ESPHome) |
| Paperboy URL | device web UI `http://<device>/`, field "Paperboy URL" (`default_paperboy_url` can preset it) | flash |
| Device key | `POST /api/device/register` by MAC | flash |

While Wi-Fi or the Paperboy URL is missing, or Paperboy does not know the MAC,
the display shows instructions with the device's address and MAC and stays
awake for up to 10 minutes before retrying in an hour.

## Building and flashing

ESPHome is pinned to **2026.10.1** and runs through `uvx`, outside Paperboy.

```powershell
cd firmware/esphome
.\build.ps1 -Hardware sticky                      # or e1001; image in .esphome/build/<hardware>/build/
uvx esphome==2026.10.1 run sticky.yaml            # first flash over USB-C
.\build.ps1 -Hardware sticky -Instance <dir>      # publish to <dir>/firmware/sticky/sticky-<version>.bin (ADR 0043)
```

The build needs no secrets. Over the network the firmware only installs images
that Paperboy offers in the sync (ADR 0043); there is no native ESPHome OTA and
no upload in the web UI, so nobody on the LAN can push an image. Everything
else, including the rescue of a device that no longer syncs, goes over USB-C.

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
that does not boot; test that with a deliberately broken image before turning
updates on. Restoring the original firmware: Seeed's playground (Sticky) or the SenseCraft/TRMNL flasher
(E1001).

## Hardware facts

**Sticky**: ESP32-S3R8, 8 MB PSRAM, 32 MB flash; panel SSD1677 800×480, ESPHome
model `Seeed-reTerminal-Sticky-gray4`. ESPHome bands gray4 luma at 32/96/160,
so Paperboy's 0/85/170/255 are drawn inside the bands (`pb_draw_gray4`).
Buttons OK GPIO4, Up GPIO5, Down GPIO6 (active low). Touch GT911 on I²C
GPIO2/3, INT GPIO21, RST GPIO41, power GPIO42; needs `swap_xy`, `mirror_x` and
`mirror_y` with `rotation: 180`. Power latch GPIO45/GPIO46 held through deep
sleep. BQ27220 fuel gauge at 0x55 on I²C GPIO0/1: state of charge 0x2C,
voltage 0x08, current 0x0C, remaining capacity 0x10; currents and capacities
read ten times too large and are divided by ten.

**E1001**: ESP32-S3, octal PSRAM; panel UC8179 800×480, ESPHome model
`Seeed-reTerminal-E1001`, black and white only: Paperboy's gray levels are
drawn as a fixed 2×2 pattern (`pb_draw_dithered`). Buttons left to right GPIO5,
GPIO4, GPIO3 (green). Battery voltage on ADC GPIO1 through a 1:2 divider,
enabled by GPIO21; no fuel gauge.

Measured on the experiment: paint 2.7 s, PNG decode about 1.8 s for 5 KB,
timer wakes reliable over 10 h on the Sticky.
