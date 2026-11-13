# 0097: Page downloads take the device key

- Status: Accepted, implemented
- Date: 2026-11-13
- Builds on: ADR 0040 (dashboard), 0042 (device protocol), 0096 (operator UI password)
- Answers: issue #219

## Context

The composed pages are the instance's content: calendar titles and locations,
birthdays, departures, the home's energy, and the menu overlay is a page with
its action names. ADR 0096 closed the operator UI behind a password, but the
pages themselves stayed open: `/pages/{display}/{name}/{hash}.png` and
`/hints/{hash}.png` answered everyone in the network, because the firmware
fetched them without credentials. A URL that appears on the dashboard, in a
browser's history or in a proxy's access log was enough to read the household's
week.

The device API already identifies a display: register and sync carry the
per-device key (ADR 0042). Nothing speaks against sending it on the image
download as well.

## Decision

### 1. No page without a key

`/pages/` and `/hints/` require the device key as a bearer token, checked the
same way sync checks it. There is no switch: the firmware that sends the key
and the server that asks for it ship in the same release (ADR 0056), so the
release is the cutover. A refusal is written to the journal once per path and
reason instead of once per wake, and the note is cleared when the path is
served again.

A page download that fails leaves the panel with its last image, so a device
that has not been updated yet keeps showing something until it takes the
firmware offer from its next sync.

### 2. A key only reaches its own display type

The device the key belongs to decides what the request may read. For a
rendered page the path names the display type; a notice page carries the type
it was drawn for (`hintPage`), because the menu is drawn over a page of that
type. A key of the kitchen display gets `403` for the living-room page.

Hints stay content-addressed and the URL is the same for every device of a
type, so a hint an operator links cannot break.

### 3. The dashboard reads the last image through the UI

`last_page_url` in `/api/devices` named `/pages/…`, which a browser can no
longer open. It now names `GET /api/devices/{id}/image`, a UI route behind the
UI password (ADR 0096) that serves the PNG the panel received, with
`Cache-Control: no-store`. It answers `404` when the render was replaced by a
newer one or the server restarted, and the card falls back to the live preview
exactly as before (ADR 0040).

### 4. The firmware sends the key

The page download in `common.yaml` carries `Authorization: Bearer <key>`, the
header it already sets for register and sync. Sticky and E1001 raise
`fw_version` in the same change. The firmware image download (`/firmware/…`)
stays open: those are public release artifacts.

## Rejected

- **A config switch (`pages: open|key`) for the rollout:** server and firmware
  ship together, so the switch would only add a second state that nobody needs
  and a way to leave the hole open by forgetting it. The journal names a device
  that still downloads without a key, which is enough to find it.
- **Signed, expiring URLs:** already rejected in ADR 0096; a valid device key
  covers the same ground and survives a changed URL.
- **Authenticating the browser instead of the device:** the dashboard is not a
  display, and giving it a device key would blur the two APIs again.

## Consequences

- Nobody in the LAN can read a page by opening its URL any more, and the
  pictures in the log of a proxy are worthless without a key.
- The key travels over plain HTTP like the image itself; confidentiality beyond
  the LAN still needs TLS in front (ADR 0096).
- A device with `firmware: hold` keeps its last image until it is updated; the
  journal says which path was refused and why.
- Text displays are untouched: they fetch their lines from `/api/device/text`
  with the key they always sent.
- `last_page_url` changes meaning in the devices API: it is a UI route, not the
  device's own image path.
