# 0044: Text displays on the device API

- Status: Accepted
- Date: 2026-10-31
- Builds on: ADR 0027 (text displays), ADR 0042 (device protocol), ADR 0041 (Paperboy firmware on ESPHome)
- Supersedes: ADR 0027 sections 2 and 3 (`text-http`, `image-http`, one token per device)

## Context

ADR 0027 planned text displays with their own protocol `text-http`: one token per device from the environment, `GET /api/text` with `ETag`/304 and a separate button endpoint. Since then ADR 0042 introduced the device API: devices register by MAC and receive a key from Paperboy, one sync per wake carries battery, log, input and the firmware offer (ADR 0043), and ADR 0041 put our own ESPHome firmware on the displays.

The existing text display is an ESP32 (`esp32dev`) with an HD44780 LCD of 20 × 4 characters behind a PCF8574 I2C expander (ESPHome `lcd_pcf8574`, address 0x27). It runs on mains power, has no buttons and today shows live PV values from Home Assistant, refreshed every second.

A sync writes `state/devices.json` and runs the plan logic; doing that every second would wear the disk and blur the device status. Text rendering, on the other hand, is cheap.

## Decision

### 1. Display types and templates

- A display type with `kind: text` has `size: [columns, rows]` in characters and an optional `charset`: `utf8` (default, unchanged), `ascii` (ä → ae, ° → o, everything else outside ASCII → `?`) or `hd44780`. `gray_levels` and `width_mm` are rejected for text display types.
- `hd44780` keeps `ä ö ü ß ° µ` as UTF-8 and transliterates everything else to ASCII (Ä → A, € → EUR, — → -). The firmware maps the kept characters to the codes of the controller's A00 character ROM; the server never sends raw controller bytes, so JSON stays valid UTF-8.
- Page templates keep the name `pages/<page>.<display type>.yaml` (ADR 0024). For a text display type they contain `lines` instead of `rows`/`blocks`, as in ADR 0027 section 1:

  ```yaml
  watch:
    - { path: solar/current, stale_after: 1m }
  lines:
    - "PV   {{ power .solar.current.pv }}"
    - "{{ pad 10 \"Akku\" }}{{ right 10 (percent .solar.current.soc) }}"
  ```

- Every line is a Go text template with the helpers of ADR 0024/0034 plus `pad n s` (left-aligned, cut or filled to n), `right n s` and `center n s`. Lines are not trimmed, cut to the column count and padded with spaces; missing lines are empty, more lines than rows is a template error. Missing values render as an em dash (`-` after charset conversion).
- `notices: mark` (default) puts `!` into the last column of the first line while a watched path is stale; `notices: none` disables it. Battery banners do not exist on text displays; a critical battery shows the warning texts as notice lines.
- Notices (no plan, broken plan, broken template) are word-wrapped into the available lines instead of drawn.
- Plans (ADR 0025) apply unchanged; a page name may have image and text templates side by side.

### 2. Sync

- Text displays register and sync like ePaper displays (ADR 0042). For a text display the sync response has `"image": null` and `"text": {"lines": [...], "hash": "..."}`. Image displays get no `text` field.
- The sync remains the place for paging, rotation, the page cursor, log entries, battery and firmware offers. A mains-powered display syncs every `refresh_s`; `listen_s` is always 0 for text displays because they never sleep.

### 3. Polling the text

- `GET /api/device/text` with `Authorization: Bearer <device key>` returns `{"lines": [...], "hash": "..."}` with `ETag: "<hash>"`. With a matching `If-None-Match` the answer is `304` without body.
- It renders the page the device's cursor points to without advancing it: if the current rule differs from the stored one, the first page of the current rule is shown and the next sync persists the switch. Notices follow the same rules as in the sync.
- It writes nothing to `state/devices.json`; only the last contact is updated in memory and persisted with the next sync. Unknown key → 401 (the firmware registers again), an image display → 409.
- The firmware polls every second. Rendering happens on every poll and is not cached, so `now` in a line is always current.

### 4. Firmware

- `firmware/esphome/lcd2004.yaml` with its own `text-common.yaml`: register, sync on boot and every `refresh_s`, poll the text every second, show the lines, OTA via `ota: http_request` from the sync's offer (ADR 0043). Wi-Fi via captive portal, Paperboy URL in the web UI, nothing installation-specific compiled in. `common.yaml` stays with the ePaper displays.

### 5. Visibility

- The device state records the last served lines (`last_text`); `/api/devices` and the dashboard show them instead of an image. The preview `GET /api/preview/{display}/{page}` returns `text/plain` for text display types.

## Consequences

- ADR 0027's own tokens, `GET /api/text` and `POST /api/text/button` are not built; text and image devices share registration, keys, plans, logs and firmware updates.
- Polling every second only costs an HTTP request and a cheap render; the state file is written once per sync.
- Rotation happens on sync, so pages of a text display rotate at most once per `refresh_s` (at least one minute).
- The HD44780 mapping assumes the A00 character ROM; a display with A02 needs another charset later.
- The existing display needs one USB flash or a last native OTA from the old configuration to get the Paperboy firmware.
