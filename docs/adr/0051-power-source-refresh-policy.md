# 0051: Refresh policy by power source

- Status: Proposed
- Date: 2026-11-07
- Amends: ADR 0041 (firmware), ADR 0042 (device protocol), ADR 0045 (text poll interval)

## Context

How often a device may refresh depends less on its display kind than on how it is powered. Today the display kind stands in for it:

- Image displays (`kind: image`) never report a `refresh_s` below one minute (`minRefresh`) or their `min_refresh` (default one minute); the ESPHome firmware (`common.yaml`) always enters deep sleep for `refresh_s` after `listen_s`.
- Text displays (`kind: text`, ADR 0044, 0045) are assumed to be on mains: `min_refresh` defaults to `1s`, the firmware (`text-common.yaml`) never sleeps, syncs every `refresh_s` (at most once a minute) and polls `GET /api/device/text?shown=<hash>` every `refresh_s`; unchanged text is a `204`.

That leaves two gaps. An ePaper device on USB power (an E1001 or Sticky on a charger) is forced into deep sleep and minute-long intervals although it could follow a clock or energy flow more closely. And nothing describes the power source, so the rules cannot be stated or validated; the minute floor for image displays is an implicit battery assumption.

Short intervals also cost server work: every sync chooses a page, composes it and hashes it. The widget cache (ADR 0049) keeps results up to `max_age` (default five minutes) and refreshes them in the background, so a short poll mostly hits the cache, but composing, quantizing and PNG encoding still run per request.

## Decision

1. **`power: battery|mains`** on the display type, overridable per device. Strictly validated; other values are an error. Default: `mains` for `kind: text`, `battery` for `kind: image`, so existing configurations behave as before. The power source is not derived from the reported battery: a battery-backed device on a charger still reports a battery, and a guess would silently change sleep behaviour. A device configured `mains` that reports falling battery volts is only visible through the existing battery warning (ADR 0020).
2. **Floors per power source.** The interval floor (`minRefresh`) applies to `battery` only:
   - `battery`: `refresh_s` ≥ max(1 min, `min_refresh`), as today.
   - `mains`: `refresh_s` ≥ `min_refresh`, whose default becomes `1s` for text and `10s` for image displays (an ePaper redraw takes seconds; shorter wakes would only queue). An explicit `min_refresh` still wins and must be at least `1s`.
   Device `refresh`, plan rule `refresh` (already allowed down to `1s` by ADR 0045) and `UntilNextBoundary` are clamped by this floor exactly as now; a plan shared by battery and mains devices keeps battery devices at their floor. Device-level `refresh` validation moves from the fixed one-minute minimum to the device's floor.
3. **`listen_s`** stays `0` after timer wakes and 20 s after input. For `mains` devices it is irrelevant (they never sleep) and stays in the response for compatibility.
4. **Sync answers `sleep`.** The sync response gains `"sleep": true|false` (`false` for `mains`). Firmware that does not know the field keeps deep sleeping, so old firmware is safe. The firmware also keeps a compile-time fallback (substitution `power`, see below) for the time before the first sync.
5. **Firmware.**
   - `common.yaml` (image displays): when the sync says `sleep: false`, the device stays awake and runs the sync loop itself: after `listen_s` it waits `refresh_s` (buttons and touch trigger an immediate sync as during the listen window) and syncs again, instead of `deep_sleep.enter`. Wi-Fi stays connected. The device sends `shown` and only downloads and redraws when the returned hash differs from what it shows (existing behaviour), so an unchanged page costs one small JSON request and no ePaper refresh. `reason` for these wakes is `timer`. A power cycle still starts with `powercycle`.
   - `text-common.yaml`, `lcd2004.yaml`: no behavioural change (already mains, never sleeping); the comments reference this ADR, and a `sleep: true` answer is ignored with a log line because the hardware has no deep sleep path.
6. **Server cost for short intervals.** No new cache is introduced. The widget cache keeps serving results until `max_age`, so plugins are not asked more often because devices poll more often. Paperboy keeps the last composed page per device and page and reuses it while all widget results are the same cache entries (no plugin call, no stale change, same page number and banners); then the hash is unchanged and no PNG is encoded. An unchanged hash never causes a redraw on the device: image firmware skips the download, text polls get `204`.
7. **Out of scope:** push delivery to text displays stays open. Mains-powered devices that stay connected are the precondition for it; push would replace the poll loop of points 4 and 5 later, while the power policy and floors stay.

## Consequences

- Existing configurations keep their behaviour: image displays default to `battery`, text displays to `mains`.
- A USB-powered ePaper device can refresh every 10 s on a fitting plan without deep sleep; a mistakenly configured `mains` on a battery device drains it quickly, which the battery warning will show but not prevent.
- New config key and response field: README, AGENTS.md and the firmware README need updating; firmware needs a new version and has to be flashed per device to benefit.
- Tests: refresh floor per power source and kind, plan refresh and rule boundaries for both, `sleep` in the sync response, validation of `power`, page reuse with an unchanged hash.
- Open questions: is 10 s the right image default floor? Should a mains image device be allowed to skip `listen_s` entirely (it is always listening anyway)? Should the hardware substitution `power` default per hardware file (E1001 `battery`, LCD `mains`)?
