# 0096: A password for the operator UI

- Status: Accepted, implemented
- Date: 2026-11-13
- Builds on: ADR 0040 (dashboard), 0042 (device protocol), 0049 (pages from widgets), 0066 (page and plan API), 0076 (one navigation)
- Supersedes: the "no token" decision of ADR 0040 and ADR 0049 for the operator UI
- Answers: issue #91

## Context

Paperboy v2 has no token: the dashboard, the editors and their API are open to
anyone who can reach the service, and the API writes files (`PUT
/api/pages/{name}`, `/api/plans/{name}`, device and display files), triggers
release checks and deletes what a prune selects. The UI also shows the
instance's content: calendar titles and locations, birthdays, departures, the
home's energy, the MAC addresses of the displays. That was accepted for a
trusted LAN (ADR 0040, 0066); before an instance is exposed beyond it, the UI
needs authentication.

At the same time the device API must not change (ADR 0042): the firmware keeps
its per-device key and downloads page images, notice pages and firmware images
without credentials, because a display has nobody to type a password.

## Decision

### 1. Two route tables, one mux each

The handler is split into `deviceRoutes` and `uiRoutes` (server.go). The UI mux
decides which side a request belongs to, and only the UI side is wrapped in the
password check. The device table stays open: `/api/device/*`, `/pages/`,
`/hints/` and `/firmware/`, nothing else.

This is deliberate instead of a path allowlist in a middleware. A new UI route
is protected by construction, and two tests pin both directions: every UI route
answers `401` without the password (the test walks the table, so a new route is
covered without being listed), and no device route ever carries the UI realm's
`WWW-Authenticate`. A third test rejects a device route outside the four device
prefixes, so a UI route cannot quietly join the open side.

### 2. Basic Auth with one shared password

`ui.password` in `paperboy.yaml` holds the password, `${VAR}` expands from the
environment like every other secret. The password is compared over SHA-256 with
`subtle.ConstantTimeCompare`, the user name is ignored, the realm is `Paperboy`,
and a rejected request answers `401` with a `WWW-Authenticate` header and a hint
that names `ui.password`. Nothing is logged but the path and the address.

Without the key the UI stays open, so an existing instance keeps working after
the upgrade. Paperboy warns at startup when the listener is not loopback and no
password is set. A password added later takes effect on the next request, without
a restart (ADR 0047).

### 3. What the password covers

The UI and its assets, the editor API for pages, plans, devices and display
types, the device list and `DELETE /api/state/devices/{mac}`, `GET /api/profiles`,
`/api/plugins`, the widget renders and both previews, the download check and the
prune. Those endpoints show instance content or write it, which makes them
operator business even when they only read.

The device API keeps its own model (ADR 0042): the per-device key on register,
sync and text poll, and no interactive authentication on the images. Page and
hint downloads get a per-device credential in a follow-up issue, together with
firmware 2.5; until then the network still sees the rendered pages, which is a
matter for TLS, not for this decision.

### 4. A cross-site write is refused

Every `POST`, `PUT` and `DELETE` on the UI side must come from Paperboy's own
origin: an `Origin` header that is present must match the request's host, and
`Sec-Fetch-Site: cross-site` is refused. Requests without an `Origin` (curl,
scripts, tests) stay allowed. The check runs with and without a configured
password, because a browser attaches cached Basic credentials to a cross-site
form post, and `POST /ui/downloads/prune` would delete files for it.

### 5. The editors keep their live view

The previews are same-origin `fetch` calls, and the browser sends the cached
credentials with them, so nothing changes in the normal case. When the password
changed while an editor is open, every request answers `401`: the page editor
stops its preview loop and says that the page has to be reloaded instead of
reporting a render error per keystroke. Both preview responses are
`Cache-Control: no-store`, so a page of the instance does not sit in a shared
cache.

### 6. TLS stays outside

Paperboy does not terminate TLS. Basic Auth over plain HTTP is readable on the
wire, and so is every page image; an instance that leaves the LAN belongs behind
a reverse proxy that terminates TLS. `docs/setup.md` says so next to
`ui.password`.

## Rejected

- **A login page with a session cookie:** needs a session store, expiry, CSRF
  tokens and a logout, and adds a second authentication path next to the device
  key. One shared password with the browser's own Basic prompt is the smaller
  device for a single operator. No logout is the price; closing the browser
  forgets the credentials.
- **A hashed password in the file:** bcrypt or argon2 would be a new dependency
  (ADR 0013), and `${VAR}` already keeps the plaintext out of a tracked file.
- **Signed, expiring page URLs:** they would protect the page route without a
  firmware change, but they are redundant once the device key covers page
  downloads, and they do not stop anyone who can read the traffic.
- **Loopback-only UI behind a reverse proxy as the only mechanism:** unusable
  without a proxy and not what a household instance wants.
- **A second listener (`-listen-ui`):** the dashboard links `last_page_url`
  relative (`/pages/…`), so a separate UI origin needs absolute URLs or serves
  the device images a second time. Deferred until an instance really exposes the
  UI; the route split above is the prerequisite and is already in place.

## Consequences

- The LAN no longer has to be trusted for the dashboard, the editors and their
  API, and a write cannot be triggered from another site.
- One password for everyone, no roles and no audit trail; multi-user accounts
  stay out of scope (issue #91).
- An instance that never sets `ui.password` keeps its old, open behavior and
  gets a startup warning on a non-loopback listener.
- Page delivery is still open until the follow-up with the device key and needs
  TLS for confidentiality; this ADR does not pretend otherwise.
