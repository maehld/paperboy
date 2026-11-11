# 0083: The address of a device in its state

- Status: Accepted
- Date: 2026-11-11

## Context

The dashboard and the devices tab showed what a display reports and how long ago
it was seen, but not where it is on the network. Finding a display's IP address
meant opening the router or the ESPHome logs. Paperboy already receives every
register, sync and text poll from the device, so the source address of that
connection is known without asking the firmware.

## Decision

We keep the source address of the last request per device in its state file:
`client_ip` in `state/devices/<MAC>.json` (ADR 0080). It comes from the
connection, never from the request body or a header such as `X-Forwarded-For`:
the device API is reached directly, and a header would be content the caller
chooses. Register, sync and text poll update it; a revoked or unknown key does
not, because there is no device state to write to.

The address is shown on the dashboard card, in the devices and unclaimed
hardware lists of the editor, and offered as `client_ip` in `GET /api/devices`.
It is a string, so IPv6 addresses fit. Nothing is sent to the device and the
firmware does not change.

## Consequences

The operator can find a display in the network without another source. The state
file grows by one short field; a device that never contacted Paperboy shows
none. Behind a reverse proxy the address is the proxy's, so a deployment that
wants the device addresses exposes the device API directly. The address is only
as trustworthy as the network it arrives from: it is a hint for the operator,
not a credential.
