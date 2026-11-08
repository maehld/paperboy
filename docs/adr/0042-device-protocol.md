# 0042: Paperboy device protocol

- Status: Accepted
- Date: 2026-10-31
- Builds on: ADR 0041 (Paperboy firmware on ESPHome)
- Partially supersedes: ADR 0017 (TRMNL as the device protocol and its workarounds), ADR 0027 section 3 (`image-http` with a configured token per device and `POST /api/image/button`)
- Related: ADR 0017 (TRMNL protocol), ADR 0020 (battery warning), ADR 0025 (plans), ADR 0039 (device status), ADR 0043 (firmware distribution)

## Context

The ESPHome experiment (ADR 0041) spoke the TRMNL API so Paperboy could stay unchanged. It worked, but the TRMNL protocol got in the way at every input:

- Inputs travel as headers: `Update-Source` for the wake reason, `special_function` for "first page". Header values had to live in globals because ESPHome's header lambdas return `const char*`.
- Every button means "next page"; there is no "back".
- Touches and the fuel gauge reading went to `POST /api/log` as fake log lines, an extra request per wake.
- Error handling is shaped by the TRMNL firmware: never status 500, never `reset_firmware`, unknown keys answered with 202 inside a 200 response.
- The device compares `filename` to decide whether to repaint; the image URL is absolute and depends on `public_url`.

ADR 0027 proposed `image-http` for graphical ESPHome displays, with a token per device in the firmware configuration. That contradicts ADR 0041: one image per hardware type must not contain per-device secrets.

## Decision

We replace the TRMNL API with a native device API for our firmware (ADR 0041). The TRMNL endpoints `/api/setup`, `/api/display` and `/api/log` and their workarounds are removed once every display runs our firmware.

### 1. Configuration

Devices are configured with `protocol: paperboy` and their `mac`; `trmnl` is no longer accepted. The existing entries in `state/devices.json` (key, page cursor, battery level) stay valid, so a display keeps its state when it is flashed.

### 2. Registration

`POST /api/device/register` with a JSON body:

```json
{ "mac": "AA:BB:CC:DD:EE:03", "hardware": "sticky", "firmware": "0.4.0" }
```

- Known MAC → `200 {"key": "…", "device": "sticky1"}`. The key is the existing one from `state/devices.json` if there is one.
- Unknown MAC → `403`. The firmware shows its MAC and retries later.

### 3. One sync per wake

`POST /api/device/sync` with `Authorization: Bearer <key>` replaces display request, log upload and button headers:

```json
{
  "firmware": "0.4.0",
  "hardware": "sticky",
  "reason": "button",
  "events": [
    { "type": "button", "button": "next" },
    { "type": "touch", "x": 412, "y": 233 }
  ],
  "shown": "1092f2e91bf8",
  "battery": { "volts": 3.70, "percent": 60, "mah": 449, "ma": -68 },
  "rssi": -63,
  "log": [ { "level": "warn", "message": "image download failed" } ]
}
```

- `reason`: `timer`, `button`, `touch` or `powercycle`.
- `events`: everything since the last sync, in order. Buttons are reported by **role**, not by pin: `next`, `previous`, `home`, optionally with `hold_ms`. The firmware maps physical buttons to roles per hardware type (Sticky and E1001: down = `next`, up = `previous`, ok = `home`). Presses made while the panel is still painting are sent together in the next sync, so Paperboy renders only the final page.
- `shown`: hash of the image on the panel.
- `battery`: `volts` is required and feeds the battery warning (ADR 0020); `percent`, `mah` and `ma` are optional (fuel gauge).
- `log`: firmware log entries, written like today's device log lines.

Response `200`:

```json
{
  "image": { "path": "/pages/sticky/wetter/1092f2e91bf8.png", "hash": "1092f2e91bf8" },
  "refresh_s": 1800,
  "listen_s": 20,
  "time": 1793450000,
  "firmware": null
}
```

- `image.path` is relative to the Paperboy URL the device was configured with; `public_url` does not matter for these devices. The device downloads only if `hash` differs from what it shows.
- `refresh_s`: deep-sleep time, computed as today (plans, `min_refresh`, rule boundaries, page timeout).
- `listen_s`: how long the device stays awake for further input before sleeping. Paperboy decides it: `0` for timer wakes, a short window after input. The firmware no longer hard-codes it.
- `time`: server time in Unix seconds, so log entries and an RTC can be set.
- `firmware`: update offer per ADR 0043, otherwise `null`.

### 4. Input semantics now

- `next` and `home` behave as button wake and `special_function` do today.
- `previous` is new: one page back within the active rule, wrapping around.
- Several events are applied in order before rendering.
- Touches and `hold_ms` are recorded and logged but change nothing yet; what they mean (regions in templates, long press actions) is a later decision.

### 5. Errors

Plain HTTP status codes, no TRMNL workarounds:

- `401` (unknown or wrong key): the firmware drops its key and registers again.
- `403` on registration: not configured.
- `5xx` or no answer: the firmware keeps its image and retries after the last `refresh_s` (or 10 minutes).

## Consequences

- One request per wake instead of up to three (log, display, button headers), and the image download only when the hash changed.
- A real "back" button, and the transport for touch and long presses is in place before their meaning is decided.
- The firmware's protocol code shrinks to register, sync and download; awake windows move to the server.
- Paperboy gains two handlers, a "previous" step in the plan cursor, and new fields in `devices.json` and `GET /api/devices` (battery percent and mAh).
- Removing TRMNL removes `/api/setup`, `/api/display` and `/api/log`, the 202 and "never 500" rules, `special_function`, the `Update-Source` parsing and the absolute image URLs; the TRMNL rules in `AGENTS.md` go with them. The battery warning (ADR 0020) and the device status (ADR 0039) carry over to the new API.
- Order of work: implement the new API, flash every display, then remove the TRMNL code. A display left on the TRMNL firmware stops getting pages at that point.
- Images stay PNG. A raw packed framebuffer would save about 1.8 s of PNG decoding per changed image on the device; it can be added later as an image format the device asks for.
- ADR 0027's `text-http` is unaffected; text displays may adopt the sync model later.
- The device API stays reachable without a user token: registration is limited to configured MACs, everything else needs the device key, and it runs in the LAN.
