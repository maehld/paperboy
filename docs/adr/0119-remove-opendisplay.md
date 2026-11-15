# 0119: Remove OpenDisplay from Paperboy

- Status: Accepted
- Date: 2026-11-15
- Related: ADR 0012 (ePaper over OpenDisplay in the LAN), ADR 0016 (OpenDisplay over Bluetooth), ADR 0018 (OpenDisplay in the display service), ADR 0031 (English in the software), ADR 0041 (ESPHome firmware), ADR 0042 (device protocol), issue #261

## Context

Paperboy carried the OpenDisplay protocol in `internal/opendisplay`, its Bluetooth transport in `odble` and the test program `cmd/odtest`. ADR 0018 delivered images to OpenDisplay devices over TCP.

The image-upload pipeline that this delivery relied on went away. Paperboy no longer delivers to OpenDisplay devices, and plans for it were only a TODO. The packages, the test program, the `address` and `buttons` device keys and the Bluetooth dependency stayed.

The Paperboy firmware on ESPHome (ADR 0041) with the device protocol (ADR 0042) is the delivery path. OpenDisplay has no place in it.

## Decision

1. **Delete the code.** `internal/opendisplay/`, `cmd/odtest/` and `docs/opendisplay.md` go away.
2. **Remove the dependency.** `tinygo.org/x/bluetooth` leaves `go.mod` and `go.sum`. ADR 0016 introduced it only for `odble`.
3. **Remove the device keys.** `address` and `buttons` leave the device files and the editor model. Strict parsing rejects them like any unknown key.
4. **Keep the protocol name as a hint.** `protocol: opendisplay` fails with a message that names the removal, like `trmnl`. A device file with that protocol does not load.
5. **Clean the documents.** The design documents, the setup page and the build script lose every mention. ADR 0012, 0014, 0016 and 0018 stay as history.

## Alternatives

- **Keep the code as an unused package:** it costs attention, carries a dependency and suggests support that does not exist.
- **Keep only the test program:** it serves nobody who tests OpenDisplay hardware, and the hardware findings stay in `03-devices.md` until this decision.
- **Keep `address` and `buttons` as ignored keys:** a silent ignore would hide a mistake, and strict parsing rejects them anyway.

## Consequences

- Paperboy has one dependency less and no Bluetooth code.
- An existing device file with `protocol: opendisplay` fails to load with a clear hint.
- The protocol code and the OpenDisplay hardware findings exist only in the Git history.
- New OpenDisplay support needs a new ADR and a new package.
