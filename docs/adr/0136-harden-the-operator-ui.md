# 0136: Harden the operator UI against framing and DNS rebinding

- Status: Accepted
- Datum: 2026-11-20

## Kontext

The UI of [ADR 0096](0096-a-password-for-the-operator-ui.md) rejects a
state-changing request whose `Origin` differs from the request's `Host`. Two
gaps remain:

1. A page of another site can embed the UI in a frame. The frame's own requests
   are same-origin, so the CSRF check passes and a click writes.
2. DNS rebinding makes `Host` and `Origin` both name the attacker's domain, so
   the check passes as well. With an empty `ui.password` the attacker reads the
   whole UI.

The password protects the UI, but it does not stop a framed click or a page
that the browser believes to be same-origin.

## Entscheidung

Paperboy sets `X-Frame-Options: DENY`, `Content-Security-Policy:
frame-ancestors 'none'` and `X-Content-Type-Options: nosniff` on every answer.

`ui.hosts` is an optional list of host names. With a list, a request to a UI
route whose `Host` header names another host gets `403`. The comparison ignores
the port and the case. Without a list the UI accepts every host, as before, and
logs a warning at startup when the listener is not loopback.

The device routes stay outside the host check. A display keeps the Paperboy URL
it was configured with, often an IP address, and a wrong host list must not
stop it.

## Konsequenzen

A framed click and a rebinding page fail. An operator who reaches the UI by a
name other than the host in `ui.hosts` gets `403` until the list names it.
Reverse proxies must forward the original `Host` header, or the list must name
the proxy's host. Without `ui.hosts` the rebinding gap stays, and the startup
warning says so.
