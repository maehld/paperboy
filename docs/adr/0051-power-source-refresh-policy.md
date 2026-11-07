# 0051: Refresh policy by power source

- Status: Accepted, implemented (firmware compiled, not yet flashed)
- Date: 2026-11-07
- Amends: ADR 0041 (firmware), ADR 0042 (device protocol), ADR 0045 (text poll interval)

## Context

How often a device may refresh depends less on its display kind than on how it is powered. Today the display kind stands in for it:

- Image displays (`kind: image`) never report a `refresh_s` below one minute (`minRefresh`) or their `min_refresh` (default one minute); the ESPHome firmware (`common.yaml`) always enters deep sleep for `refresh_s` after `listen_s`.
- Text displays (`kind: text`, ADR 0044, 0045) are assumed to be on mains: `min_refresh` defaults to `1s`, the firmware (`text-common.yaml`) never sleeps, syncs every `refresh_s` (at most once a minute) and polls `GET /api/device/text?shown=<hash>` every `refresh_s`; unchanged text is a `204`.

That leaves two gaps. An ePaper device on USB power (an E1001 or Sticky on a charger) is forced into deep sleep and minute-long intervals although it could follow a clock or energy flow more closely. And nothing describes the power source, so the rules cannot be stated or validated; the minute floor for image displays is an implicit battery assumption.

Short intervals also cost server work: every sync chooses a page, composes it and hashes it. The widget cache (ADR 0049) keeps results up to `max_age` (default five minutes) and refreshes them in the background, so a short poll mostly hits the cache, but composing, quantizing and PNG encoding still run per request.

## Decision

1. **`power: auto|battery|mains`** on the display type, overridable per device (device wins). Strictly validated; other values are an error. Default: `auto`.
   - **The hardware reports its power source.** The sync body (and register) gains an optional `power: battery|mains`. The firmware sets it from the hardware: the hardware file fills `pb_power` (`lcd2004`: `mains`; `e1001`: `battery`; `sticky`: measured at every wake, see below). The last reported value is kept in `state/devices.json` (`reported_power`) and shown in `/api/devices` and the dashboard.
   - **`auto`** uses the last reported value; when the device has never reported one (old firmware, before the first sync), it behaves as `battery`.
   - `battery`/`mains` set explicitly override what the hardware reports, e.g. for hardware that cannot detect it.
   - Consequence for existing text displays: their current firmware reports nothing, so under `auto` they fall back to the battery floor (one minute) until they run firmware that reports `mains`, or until `power: mains` is configured. This is intended: unknown means the safe behaviour.
   - Deriving the power source from the presence of battery values is not used: a battery-backed device on a charger still reports a battery.
2. **Floors per power source.** The interval floor (`minRefresh`) applies to `battery` only:
   - `battery`: `refresh_s` ≥ max(1 min, `min_refresh`), as today.
   - `mains`: `refresh_s` ≥ `min_refresh`, whose default becomes `1s` for text and `10s` for image displays (an ePaper redraw takes seconds; shorter wakes would only queue). An explicit `min_refresh` still wins and must be at least `1s`.
   Device `refresh`, plan rule `refresh` (already allowed down to `1s` by ADR 0045) and `UntilNextBoundary` are clamped by this floor exactly as now; a plan shared by battery and mains devices keeps battery devices at their floor. Device-level `refresh` may go down to `1s` in the configuration; the floor is applied at runtime because it may depend on the device's report.
3. **`listen_s`** stays `0` after timer wakes and 20 s after input. For `mains` devices it is irrelevant (they never sleep) and stays in the response for compatibility.
4. **Sync answers `sleep`.** The sync response gains `"sleep": true|false` (`false` for `mains`). Firmware that does not know the field keeps deep sleeping, so old firmware is safe; new firmware treats a missing field (older server) as `sleep: true`.
5. **Firmware.**
   - `common.yaml` (image displays): when the sync says `sleep: false`, the device stays awake and runs the sync loop itself: after `listen_s` it waits `refresh_s` (buttons and touch trigger an immediate sync as during the listen window) and syncs again, instead of `deep_sleep.enter`. Wi-Fi stays connected. The device sends `shown` and only downloads and redraws when the returned hash differs from what it shows (existing behaviour), so an unchanged page costs one small JSON request and no ePaper refresh. `reason` for these wakes is `timer`. A power cycle still starts with `powercycle`.
   - `text-common.yaml`, `lcd2004.yaml`: no behavioural change (already mains, never sleeping); the comments reference this ADR, every sync reports `mains`, and the `sleep` field is ignored because the hardware has no deep sleep path.
6. **Server cost for short intervals.** No new cache is introduced. The widget cache keeps serving results until `max_age`, so plugins are not asked more often because devices poll more often. Paperboy still composes the page on every sync (cheap with cached widget results), but compares the composed pixels with the last render of that display type and page; when they are identical it reuses the stored PNG and hash instead of encoding again. A full fingerprint of all widget inputs was dropped: built-in widgets such as the clock run uncached in process, so only the pixels tell reliably whether the page changed. An unchanged hash never causes a redraw on the device: image firmware skips the download, text polls get `204`.
7. **Out of scope:** push delivery to text displays stays open. Mains-powered devices that stay connected are the precondition for it; push would replace the poll loop of points 4 and 5 later, while the power policy and floors stay.

## Consequences

- Existing image displays keep their behaviour (`auto` without a report = `battery`). Existing text displays drop to a one-minute floor until they get firmware that reports `mains` or are configured `power: mains`; the README migration note must say so.
- The sync and register bodies ignore unknown fields, so new firmware that sends `power` also works against an older Paperboy (it then never gets `sleep: false`). A `power` value other than `battery` or `mains` is answered with `400`.
- A device whose power source changes (charger plugged in or out) switches behaviour with its next sync when the hardware detects it.
- A USB-powered ePaper device can refresh every 10 s on a fitting plan without deep sleep; a mistakenly configured `mains` on a battery device drains it quickly, which the battery warning will show but not prevent.
- New config key and response field: README, AGENTS.md and the firmware README need updating; firmware needs a new version and has to be flashed per device to benefit.
- Tests: refresh floor per effective power source (`auto` with and without report, explicit override), plan refresh and rule boundaries for both, `sleep` in the sync response, validation of `power`, page reuse with an unchanged hash.
- Open questions: is 10 s the right image default floor? Should a mains image device be allowed to skip `listen_s` entirely (it is always listening anyway)? The GPIO9 threshold on the Sticky is taken from the schematic and has not been measured on a device yet.

## Hardware detection (checked 2026-11-07 against the Seeed schematics)

- **reTerminal Sticky** (schematic `reTerminal_Sticky_Schematic_diagram_260609.pdf`): USB `VIN_5V` reaches **GPIO9** as `PWR_IN_VOLT` through a 5.1 kΩ/5.1 kΩ divider (about 2.5 V with USB, 0 V without; GPIO9 is ADC1). The BQ25616 charger's `STAT` is on GPIO40 (`CHARGE_STATE`), its charge enable on GPIO39 (`EN_BAT_CHGn`). The firmware reads GPIO9 with the ADC at every wake and reports `mains` above about 1.5 V. `STAT` and the BQ27220 current (Seeed's playground treats > 20 mA as charging) only show charging, not a full battery on USB, and are not used.
- **reTerminal E1001** (schematic `202004307_reTerminal_E1001_V1.0_SCH_250805.pdf`): the ETA6003 charger's `STAT` drives only the red LED, its `USB_DET` is an input of the charger, and no ESP32 GPIO sees VBUS (the ADC on GPIO1 measures the battery). The only hint is the CH340C USB-UART bridge, which is powered from USB, but its TX idles on UART0 that the logger uses; this is not reliable. The E1001 firmware therefore reports the compile-time value `battery`; an E1001 on USB needs `power: mains` in the configuration. The OpenDisplay firmware (checked 2026-11-07) does not detect it either: its charge state comes only from a BQ25616 `STAT` pin with a BQ27220 (Sticky) or from an AXP2101, and its E1001 preset uses the fixed power profile `battery-2000`.
- **lcd2004**: reports `mains` (no battery).
