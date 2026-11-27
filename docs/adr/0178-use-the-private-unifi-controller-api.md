# 0178: Use the private UniFi controller API for the WAN summary

- Status: Accepted, implemented
- Date: 2026-11-27
- Builds on: ADR 0049 (plugin widgets), 0126 (schedule event), 0127 (conditions are values), 0151 (design language)
- Answers: issue #494

## Context

The `unifi` plugin shows the state of a home network. It shows the device and
client counts, the offline devices, and whether the internet works. The
controller documents an integration API that takes an API key. The plugin reads
its sites, devices and clients there.

Version 11.0.97 of that API has no WAN status. Its health answer carries only
the `id` and the `name` of a WAN, not its state, its address or its latency. A
page could therefore count devices but never say that the line is down.

The controller's own web interface knows more. The private endpoint
`GET /proxy/network/api/s/<internalReference>/stat/health` answers the WAN
status and the latency of the internet tests. It accepts the same `X-API-KEY`
as the integration API, with the site's `internalReference` instead of its
`id`. We verified this on 2026-11-27 on the operator console.

A home page wants one line about the internet and otherwise the documented
counts. It does not want a second account, a second key or a cloud service.

## Decision

### 1. The documented integration API stays the base

Sites, devices and clients come from the documented integration API, below
`/proxy/network/integration/v1` on a UniFi OS console. The site list gives the
`id` for these calls and the `internalReference` for the private path. The
documented API is the stable half: it keeps its contract across controller
releases.

### 2. One private call for the WAN summary

The plugin adds `GET /proxy/network/api/s/<internalReference>/stat/health` and
sends the same key as `X-API-KEY`. It reads only the `wan` and the `www`
entries. The parse is defensive: a missing entry counts as unread, and a
`meta.rc` other than `ok` is an error for that call. The private path sits
beside the documented base and never replaces it.

### 3. The private call is optional

`wan: false` turns the WAN line off and skips the private request. The page
then rests on the documented half: devices, clients and their offline count.
`latency` answers a dash, the WAN summary disappears from `worst`, and `state`
ignores the private status. The default is `true`.

### 4. A failed private call costs only the WAN line

A failed private request is logged once and keeps the fresh devices and
clients. The previous WAN summary stays until a later call succeeds, and the
same message is not repeated on every refresh. The page never goes blank and a
device count is never lost because the WAN answer broke.

### 5. Only a summary, never per-WAN detail

The WAN summary is `<ISP> - <latency> ms`, or nothing when the answer has no
ISP or no latency. The plugin ignores `uptime_stats`: its keys are interface
names, not the configured WANs, and a standby WAN reports 0 percent. A standby
WAN must not degrade the state. Only the `wan` and `www` status decide that,
together with an offline device.

## Consequences

- Every refresh costs one private request more. `wan: false` removes it
  completely.
- No cloud account, no second credential and no Site Manager dependency: the
  plugin stays on the LAN with the key from *Integrations*.
- A private endpoint can change with a controller release. A changed answer
  loses the WAN line, not the page, and the log names the reason.
- Version 11.0.97 is the verified shape. Another firmware may answer
  differently, and the parser follows in a later change.
- The plugin reads a UniFi OS console. A standalone Network application serves
  its integration API elsewhere; that build is not covered.
- The plugin stays read-only and keeps no state and no blob. One fetch
  rebuilds every value.
- The WAN part of `state` follows the controller's status, not the latency. A
  slow line is not a broken line.

## Alternatives

1. **The documented API alone.** It has no WAN status, so the page would lose
   the internet line and the latency. The plugin exists to show both.
2. **The Site Manager API in the cloud.** It needs a cloud account and a second
   key, and it reports no live latency of the local line.
3. **A probe from Paperboy itself.** An ICMP or HTTP probe measures the path of
   the Paperboy host, not the controller's WAN, and duplicates the controller's
   own tests.
4. **The private API for everything.** Devices and clients would come from an
   undocumented path too, so the stable base and its key would go.
5. **Per-WAN detail from `uptime_stats`.** Per-WAN detail is out of scope, and
   a standby WAN at 0 percent would read as an outage.
6. **Scraping the controller's web interface.** It needs a session and login,
   and it breaks with every layout change of the UI.
