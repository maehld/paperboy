# 0080: Device state in a file per MAC

- Status: Accepted, implemented
- Date: 2026-11-11
- Builds on: ADR 0037 (instance directory layout), ADR 0070 (devices in files of their own), ADR 0079 (renaming)
- Supersedes: the `state/devices.json` part of ADR 0037 and the "a device rename moves its state too" rule of ADR 0079

## Context

Device state lived in one file, `state/devices.json`, keyed by the device ID. That ID is the file name of the device's configuration, so the key changes whenever the operator renames `devices/<id>.yaml`. ADR 0079 therefore had to move the entry during a rename, and had to refuse a rename whose target ID still had state from a deleted device.

The file also mixed hardware facts (key, reported firmware, battery readings, page cursor, menu) with a configuration identity. And an unknown display — the normal case while setting up new hardware — was invisible: register answered `403` and logged one line, so the operator had to read the log to find the MAC before writing a device file by hand.

Every sync rewrote the whole file, for every device, which grows with the installation and turns one device's wake into a write of all devices' state.

## Decision

- **One state file per MAC.** `state/devices/<MAC>.json`, where the name is the MAC in upper case hex without separators, e.g. `A0B1C2D3E4F5.json`. The MAC is what the firmware registers with and what hardware keeps across reflashes and renames; a device file is only the operator's claim on it.
- **The content is the former entry**, unchanged: API key, page cursor, first and last contact, battery, RSSI, firmware, power source, last served page, text lines, event ID, menu and firmware offers. No claim flag: hardware is claimed exactly when a `devices/<id>.yaml` names its MAC.
- **An unknown MAC leaves a state file and still gets `403`.** The register writes hardware, firmware, first and last contact and no key, then answers as before, so the firmware neither keeps a key nor starts syncing. The devices tab lists such files as unclaimed hardware: MAC, hardware, firmware, first and last contact, with *Claim* (opens a new device file with the MAC prefilled) and *Forget* (`DELETE /api/state/devices/{mac}`).
- **Renaming and deleting device files never touches state.** The key, page cursor, battery reading and menu stay with the MAC; a recreated device file picks them up. A rename can no longer take over another device's state, so the `409` of ADR 0079 is gone.
- **Only the device that changed is written**, after the state lock is released, atomically and with mode `0600` because the file holds the API key.
- **No migration.** `state/devices.json` is ignored and named once in the log. Splitting it by hand is a copy of each entry to `state/devices/<MAC>.json`; without it, devices register again and start with an empty cursor, battery reading and menu.
- **One unusable file does not take the others down.** A file in `state/devices/` whose name is no MAC or whose content does not parse is logged and skipped. This is the opposite of the strict configuration parsing: state is a cache of what hardware reported, and a damaged file must not keep every other display off the air.

## Alternatives

- **Keep one file and key it by MAC.** Smallest change, but every sync still rewrites every device's state, and a `grep` for one device means reading all of it.
- **A directory per device** (`state/devices/<MAC>/state.json`). Leaves room for later blobs, but device state is a small JSON object and will stay one; the extra level buys nothing today.
- **Keep the device ID as the key and copy on rename** as ADR 0079 did. It works, but it makes an operator-facing name part of the hardware's identity and needs the collision rules that come with it.

## Consequences

- An installation has one small file per MAC; deleting a device file keeps its state, which is intentional and now has a visible cleanup path (*Forget*).
- Unknown hardware appears in the editor without a log hunt, and claiming it is a form, not a hand-written MAC.
- The API key lives in many files instead of one; a backup has to include the whole `state/devices/` directory.
- A disabled or replaced display leaves a state file behind until someone forgets it; nothing is deleted automatically.
