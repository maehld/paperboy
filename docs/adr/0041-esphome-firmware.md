# 0041: Paperboy firmware built on ESPHome

- Status: Proposed
- Date: 2026-10-31
- Partially supersedes: ADR 0017 (displays run the unmodified TRMNL firmware; ESPHome rejected)
- Related: ADR 0042 (device protocol), ADR 0043 (firmware distribution), ADR 0027 (ESPHome text displays)

## Context

ADR 0017 chose the unmodified TRMNL firmware and rejected ESPHome because every device would need its own YAML and build, and because server-controlled intervals needed extra code. Living with TRMNL since then has shown its limits:

- One wake button with short press and hold; the Sticky's and the E1001's three buttons and the Sticky's touch panel are unusable.
- No "back", no touch, inputs squeezed into headers (`Update-Source`, `special_function`).
- Protocol quirks Paperboy must honour forever: never status 500, never `reset_firmware`, unknown keys answered with 202.
- Firmware updates come from TRMNL's channel, not from us.

Building our own firmware from scratch was estimated at 45–70 person-days (display waveforms, deep sleep, Wi-Fi provisioning, OTA with rollback). The experiment on branch `experiment/esphome-sticky` (`experiments/esphome-sticky/`) tested ESPHome as the base instead, on a reTerminal Sticky and a reTerminal E1001, against the unchanged Paperboy server:

| Question | Result |
|---|---|
| Fetch, paint, deep sleep for `refresh_rate` | works on both; Sticky timer wakes every 30 min for 10 h without a miss |
| Four gray levels | Sticky: native (`Seeed-reTerminal-Sticky-gray4`), Paperboy's 0/85/170/255 mapped into ESPHome's luma bands. E1001: ESPHome drives the UC8179 in black and white only; grays as a 2×2 pattern look readable but not native |
| Three buttons, wake from any of them | works on both (EXT1) |
| Touch (Sticky, GT911) | works; coordinates need `swap_xy` plus both mirrors to match the rotated image |
| Battery | Sticky: BQ27220 fuel gauge (state of charge, mAh, mA; currents and capacities read ten times too large and are scaled). E1001: ADC voltage only |
| Skip unchanged content | content hash kept in RTC memory; no download and no paint when unchanged |
| One image for every installation | Wi-Fi from captive portal or Improv, Paperboy URL in the device web UI, key fetched by MAC; network name `paperboy-<last three MAC bytes>` |
| Firmware update over HTTP | 0.3.3 → 0.3.4 in about 16 s from button wake to reboot, MD5 checked, decision reported to Paperboy's log |
| Costs per wake | PNG decode about 1.8 s for 5 KB, paint 2.7 s (Sticky, black and white) |

Getting both devices to this point took one evening. ADR 0017's main objection (one YAML and build per device) is gone once nothing installation-specific is compiled in.

## Decision

1. **We build our own display firmware on ESPHome.** One firmware image per hardware type (for now `sticky` and `e1001`), identical for every installation and device.
2. **Source in this repository** under `firmware/esphome/`: a shared package (`common.yaml`) with the Paperboy cycle and protocol, a C++ header for helpers, and one file per hardware type with pins, display and battery. The experiment directory moves there when this ADR is accepted.
3. **Nothing installation-specific in the build.** Wi-Fi comes from the captive portal or Improv, the Paperboy URL from the device web UI (optionally preset at build time), the device key from Paperboy by MAC (ADR 0042). All are kept in flash.
4. **Pinned ESPHome version.** Builds use one fixed ESPHome release, raised deliberately; the experiment hit two breaking API changes between versions (header lambdas returning `const char*`, IP address formatting).
5. **Logic moves into C++ where YAML gets in the way.** Protocol handling and drawing live in our own code (first a header, later an ESPHome external component in `firmware/esphome/components/`). It is plain C++ and could be taken along if ESPHome is ever dropped.
6. **Builds happen outside Paperboy** (a script now, CI later). The Paperboy binary and container do not contain ESPHome, Python or the ESP toolchain.
7. **The TRMNL firmware stays supported** until the devices are migrated. Removing the TRMNL path from Paperboy is a later decision.

## Consequences

- All three buttons and touch become usable, and the device protocol can be designed for Paperboy (ADR 0042) instead of around TRMNL.
- We own firmware updates (ADR 0043) and their risks; ESPHome's two OTA slots and `safe_mode` give a rollback, which still has to be tested with a deliberately broken image.
- The E1001 loses native gray levels until we write an ESPHome display component for the UC8179 four-gray mode (estimated 1–3 days; the TRMNL firmware and `bb_epaper` show how). Until then it shows dithered grays.
- ESPHome's C++ runtime is GPLv3. The firmware sources are in this public repository, which covers the obligations when images are handed out; Paperboy itself only serves the files.
- New hardware types need a device file and a pin survey, but no new protocol work.
- Battery life under the new firmware is not measured yet; the Sticky's fuel gauge reports remaining mAh per wake for that purpose.
- `AGENTS.md`, the README and the hardware findings get a firmware section when this is accepted.
