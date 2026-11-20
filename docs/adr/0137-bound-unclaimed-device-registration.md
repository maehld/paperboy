# 0137: Bound unclaimed device registration

- Status: Accepted
- Datum: 2026-11-20

## Kontext

`POST /api/device/register` takes a MAC without authentication. For every valid
MAC that no device file uses, Paperboy creates a state entry and a file under
`state/devices/` ([ADR 0080](0080-device-state-per-mac.md)). Any client on the
LAN can therefore flood the state map, the directory and the journal. The
unclaimed list has no limit and the journal logged every registration.

## Entscheidung

Paperboy keeps at most ten unclaimed state entries. A new MAC above the limit
gets `403` and no state file. An existing unclaimed entry keeps updating, so
hardware that registered once stays visible and claimable.

The journal names an unknown MAC once per hour. Paperboy remembers a bounded
number of MACs for that, so a flood cannot grow that memory either.

The limit is a constant, not a configuration key. Ten unclaimed displays cover
the normal case of a fresh installation.

## Konsequenzen

The state map and `state/devices/` stay bounded. An operator with more than ten
unclaimed displays claims or forgets entries first; the display gets `403` and
shows the unknown-device screen until then. An instance that already has more
than ten unclaimed entries keeps them and accepts no new hardware. A flood can
still reach the HTTP server, but it no longer writes files.
