# 0043: Firmware distribution through Paperboy

- Status: Proposed
- Date: 2026-10-31
- Builds on: ADR 0041 (Paperboy firmware on ESPHome), ADR 0042 (device protocol)
- Extends: ADR 0037 (instance directory layout), ADR 0026 (configuration), ADR 0039 (device status)

## Context

The ESPHome experiment (ADR 0041) updated a Sticky over HTTP: the device read an ESP Web Tools style manifest from a URL entered in its web UI, compared the version, checked the battery, downloaded the image, verified its MD5 and rebooted into it. The manifest and image were written by a script (`publish-firmware.ps1`) and served by a throwaway HTTP server.

For real use the device should not need a second URL, and the decision when to update belongs to the server: it knows the battery history, which devices an operator wants to hold back, and whether an earlier attempt failed. ESPHome checks the MD5 and keeps the previous image, and `safe_mode` rolls back an image that does not boot; a device that keeps failing would still download and flash on every wake if nothing stops it.

Once Paperboy makes the offer, the manifest has no reader left on the device: Paperboy can take the version from the file name and compute the MD5 itself.

## Decision

### 1. Firmware in the instance directory

The instance directory (ADR 0037) gains an operator-owned `firmware/` directory, next to `pages/` and `icons/`. Paperboy reads it and never creates or writes it:

```
firmware/
  sticky/
    sticky-0.4.0.bin
  e1001/
    e1001-0.4.0.bin
```

- One directory per hardware type, named as the firmware reports it in `hardware` (ADR 0042).
- **Exactly one image per directory**, named `<hardware>-<version>.bin` (the ESPHome OTA image). The version comes from the file name; Paperboy computes the MD5 when it reads the file. There is no manifest.
- Rolling out means replacing the file; rolling back means putting the older file there. A directory with no image or several images is an error: Paperboy offers nothing for that hardware and reports it in the log and the dashboard.
- The publish script writes the file and removes the previous one (`-Out <instance>/firmware`). Paperboy does not build firmware and does not download it from the internet.

Paperboy serves the image at `GET /firmware/<hardware>/<file>` without a token, like page images. Names are validated as for icons (ADR 0038); paths outside `firmware/` are rejected. The directory is read again when it changes, like plans.

### 2. Configuration

```yaml
firmware:
  update: auto            # auto or off; default off
  min_battery_volts: 3.6

devices:
  kueche:
    firmware: hold        # optional: hold keeps this device on its version
```

Updates are off until the operator turns them on.

### 3. Offer in the sync response

On `POST /api/device/sync` Paperboy offers an update when all of these hold:

- `firmware.update` is `auto` and the device is not on `hold`;
- an image exists for the device's `hardware` and its version **differs** from the reported `firmware` (a lower version is a deliberate rollback);
- the reported battery is at least `min_battery_volts` and not at warning or critical level (ADR 0020);
- the same version has not failed three times for this device.

The response then carries:

```json
"firmware": { "version": "0.4.1", "path": "/firmware/sticky/sticky-0.4.1.bin", "md5": "…" }
```

The device flashes and reboots; its next sync reports the new version. If the next sync still reports the old version, Paperboy counts a failed attempt. After three, it stops offering that version to that device until the image changes or the operator clears the counter. The firmware's own log entries (`log` in the sync) explain the failure.

### 4. State and visibility

`state/devices.json` records per device `firmware` (reported version), `firmware_offered` (version and time of the last offer) and `firmware_failures` (count for that version). `GET /api/devices` and the dashboard (ADR 0040) show the running version, the available version and a status: `current`, `offered`, `failed`, `held` or `none` (no single image for the hardware).

### 5. Firmware side

The device's own manifest URL field from the experiment is removed: Paperboy is the only update source. Updates are only applied when offered in a sync, so a device never polls a second server.

## Consequences

- Rolling out a new version means copying one file into the instance's `firmware/` directory; devices update on their next sync, within one refresh interval, if the battery allows.
- Holding single devices back and pinning a rollback need no firmware changes.
- A broken image costs at most three attempts per device, not one per wake.
- Integrity rests on the MD5 Paperboy computes; authenticity on the LAN, like the rest of the device API. Signed images are possible later if ESPHome's OTA supports them.
- The version lives only in the file name. A renamed file announces a version the image does not report; the device then fails to reach that version and the offer stops after three attempts.
- New work: the `firmware` section and per-device `firmware` key in the configuration, the image reader and file handler, offer and failure tracking in the sync handler, devices API and dashboard fields, and the publish script moving to `firmware/esphome/`.
- Still to test before `update: auto` is used: the rollback with a deliberately broken image.
