# 0181: ViCare authenticates with a refresh token in plugin state

- Status: Accepted, implemented
- Date: 2026-11-28
- Builds on: [ADR 0081](0081-plugin-state-over-the-protocol.md) (plugin state), [ADR 0126](0126-a-schedule-event-for-plugin-demand.md) (demand-driven reads), [ADR 0075](0075-plugin-versions.md) (a version per plugin)
- Answers: issue #506

## Context

Viessmann heating systems report their data to the ViCare cloud. Paperboy can
read it through the official API of the Viessmann developer portal; no local
interface exists for the current device generation.

The API has three properties that decide how a plugin reaches it:

1. **OAuth2 with PKCE.** The user creates a personal client in the developer
   portal and logs in with the ViCare account. A client ID and an access token
   are needed; no password grant is supported for new code. PyViCare still has a
   password flow that posts credentials to the authorization endpoint, but
   marks it as deprecated.
2. **A rotating refresh token.** The access token lasts one hour. The refresh
   token is replaced on renewal, so the value in `paperboy.yaml` is a seed, not
   the current token.
3. **A daily quota.** The free Basic plan allows about 1450 calls a day, and
   one gateway-wide features call returns every device of the installation.

A plugin may not keep files itself ([ADR 0081](0081-plugin-state-over-the-protocol.md)),
and `paperboy.yaml` is configuration, not mutable state.

## Decision

1. **The plugin reads the ViCare cloud API.** One call per refresh reads
   `/iot/v2/features/installations/{id}/gateways/{serial}/features?includeDevicesFeatures=true`,
   which carries the features of every device on the gateway. The plugin keeps
   the device that answers; a configured `installation`, `gateway` and `device`
   wins over the discovered ones.
2. **A helper obtains the first refresh token.** `vicare -login` runs the
   authorization code flow with PKCE in the terminal: it prints the URL, reads
   the code or the redirect address, exchanges it and prints the refresh token
   and the IDs of the account. The password never enters the configuration.
3. **The configuration holds the client ID and the seed refresh token.** The
   plugin renews the access token itself and keeps the current access and
   refresh tokens in plugin state. A fingerprint of client ID, e-mail, IDs and
   seed discards a state of another configuration; a new login therefore
   replaces the stored tokens.
4. **E-mail and password may seed the chain instead.** When no token is stored
   or the API refuses the token, the plugin posts the credentials to the
   authorization endpoint and reads the code from its redirect. It keeps the
   password in the configuration and never in the state, and it uses it only
   for that seed. The browser helper stays the reliable way, because Viessmann
   can require a login page and the flow is not a documented grant.
5. **Reads follow demand.** The plugin implements `Starter` and uses
   `plugin.Demand` ([ADR 0126](0126-a-schedule-event-for-plugin-demand.md)), so
   it reads only when a display needs the data. The default refresh is 10
   minutes, the minimum 5 minutes.
6. **The heating curve is computed.** The API reports slope and shift only.
   The plugin applies the two shapes PyViCare found in the firmware, and the
   `formula` argument can name one. The widget marks the target at the current
   outside temperature.

## Consequences

- The rotating refresh token has a place to live. A lost
  `state/plugins/vicare/state.json` costs a new login when the seed has already
  rotated away.
- An operator who configures a password accepts a long-lived credential in the
  configuration. The plugin narrows that to the cases where the token chain
  breaks, but Viessmann's login page or two-factor authentication can still
  make the browser helper necessary.
- `state/plugins/vicare/state.json` holds a credential. It is as sensitive as
  `paperboy.yaml`; the instance directory belongs to the operator.
- The heating system is only reachable while it is online in the ViCare app.
  A cloud outage leaves the last snapshot on the page.
- Feature names differ between boilers, heat pumps and pellets systems. A
  missing feature stays absent and the widget leaves its line out.
- Actions (setpoints, DHW one-time charge, `setCurve`), consumption statistics
  and ventilation devices are follow-up work. They reuse the same client.
