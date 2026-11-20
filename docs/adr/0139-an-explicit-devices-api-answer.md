# 0139: An explicit answer for the devices API

- Status: Accepted
- Datum: 2026-11-20

## Kontext

`deviceInfo` embedded the whole `deviceState` and shadowed only `APIKey` with a
second field. Every other state field therefore reached `GET /api/devices`,
including `pair_code`, `pair_expiry` and `pair_authorized`. The endpoint is open
when `ui.password` is empty. Field shadowing is also fragile: a state field that
is added later leaks the same way.

## Entscheidung

`GET /api/devices` answers a `deviceAPIInfo` with an explicit field list. The
list holds the device, the derived status and the state that the dashboard and
the documented API show. It never holds the device key or the pairing state.
`pairingOf` answers a waiting pair separately.

`deviceInfo` keeps the state in memory for the dashboard, and the embedded
struct is excluded from JSON. A test fixes the field set and proves that neither
the key nor a pair code appears in the answer.

## Konsequenzen

A new state field stays out of the API until the list names it. That is one more
line for a new field and one fewer accidental secret. A script that read an
undocumented field loses it; the documented fields of [ADR 0039](0039-device-status.md),
[0043](0043-firmware-distribution.md), [0044](0044-text-displays-on-the-device-api.md),
[0051](0051-power-source-refresh-policy.md) and [0083](0083-device-address.md)
stay.
