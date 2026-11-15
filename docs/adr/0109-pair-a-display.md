# 0109: Pair a display instead of trusting its MAC address

- Status: Accepted, implemented (firmware not yet flashed)
- Date: 2026-11-15
- Related: ADR 0041, 0042 (firmware, device protocol), 0080 (state per MAC), 0096 (UI password), 0097 (page key), 0108 (TLS), issue #241

## Context

`POST /api/device/register` trades a MAC address for the device key of every configured display. It has no rate limit and no proof that the caller is that display. Until ADR 0096 the MAC addresses were readable from `/api/devices` by anyone in the LAN. Behind a public reverse proxy the endpoint is reachable from anywhere. A MAC is printed on the device, sits in the device file and appears in the journal.

A key is not read access only. It fetches every page of that display type (ADR 0097). It also drives the device menu, whose confirmation is bound to a `shown` hash the caller produces itself. That reaches the plan's actions, for example evcc's charging mode or the Škoda climate.

The instance needs a moment at which a person confirms that a display belongs to it. A MAC address must stop being the only proof. Two properties shape the solution:

- The firmware image is one per hardware type (ADR 0041) and must not carry a per-device secret. The proof cannot live in the build.
- Nothing on the device proves who owns it. Only the operator can, by looking at the panel.

## Decision

### 1. Pairing is the only way to a key

A configured display that registers always gets a six-character code instead of its key. There is no switch and no second path. A display with a working key never calls register, so an existing installation keeps running. A new display, a forgotten one and one that lost its key pair once. Unknown MACs stay unclaimed hardware (ADR 0080).

### 2. A code on the panel, entered in the device editor

The register of a configured display answers `403` with this body until the operator confirmed it:

```json
{ "error": "pairing required: …", "code": "K7M2QF", "expires_in": 600 }
```

The firmware shows the code instead of the setup instructions. The operator opens the device in the editor and presses *Pair*. That asks for the code and posts it to `POST /api/state/devices/{mac}/pair`, a UI route behind `ui.password`. On a match Paperboy marks the state as confirmed. The next register then takes the key. The confirmation is consumed, so one confirmation never hands out two keys.

The code has six characters from an alphabet without `I`, `O`, `0` and `1`. It is valid for ten minutes and limited to five attempts. A wrong attempt is counted and answered without a timing difference. While the code is valid it stays the same, so a display that asks every minute does not repaint. After the expiry the next register issues a new one. The endpoint refuses a MAC without a device file, answers `410` for an expired code and `429` after the fifth wrong attempt.

### 3. The device only shows the code

Nothing travels from the panel to the browser: the operator types what they see. The code is therefore not a secret that the device echoes back. Such an echo would prove nothing, because the device receives the code from the same endpoint.

The firmware keeps the code in a global and draws it once. A repeated identical code does not repaint. A text display prints `Pairing code: <code>` on its two lines.

### 4. `Forget` means pairing again

`DELETE /api/state/devices/{mac}` deletes the state, and with it the key and the confirmation. The display registers again, shows a code, and the operator pairs it once more. A reflashed display that kept its key in flash does not notice.

## Consequences

- A MAC address alone no longer buys a key. Exposing an instance is no longer a question of who knows a label. The code cannot be brute-forced within ten minutes and five attempts.
- A device that loses its key needs one physical pairing step. That covers a lost state, `Forget` and a key mismatch. The code is short and stays on the panel for ten minutes, which keeps the step small.
- Between the confirmation and the next register the authorization is outstanding. A caller who knows the MAC and polls register in that window could take the key first. The window is one register cycle of a display that is awake and shows its code. Closing it would need the key to travel through the browser, which is worse.
- The claim endpoint is a UI route. The UI password and the same-origin check of ADR 0096 apply. Without a password the pairing only stops callers outside the UI's reach, which is what `pair` is for.
- Pairing does not replace TLS. The code travels in clear text on the LAN, like the key and the pages (ADR 0108).
- All four firmware images change: Sticky 2.9.0, E1001 2.10.0, E1003 1.2.0, lcd2004 2.4.0.

## Considered and rejected

- **An enrollment secret per device, set at provisioning:** it puts a secret into the device's flash and into the operator's notes. One image per hardware type must not carry it, and the code needs neither.
- **The device sends the code back:** the device receives the code from the same endpoint, so the echo proves nothing. Only the operator's sight of the panel does.
- **A shared enrollment token for the whole instance:** one leaked value pairs a whole fleet. An operator enters it in the panel's setup screen, which is worse than a short code per display.
- **TOTP or a per-device HMAC:** it is no better than a random code with an attempt limit. It also needs a secret on the device.
- **A claim without a code:** an operator who clicks *Pair* for hardware they cannot see would still hand out a key. The code ties the click to the panel in front of them.
- **Rate-limiting the register instead:** it slows the guess without a moment of confirmation. A leaked MAC would still work eventually.
- **A switch (`enrollment: mac|pair`) that keeps the old path:** it would carry two code paths and a way to leave the hole open by accident. The old path is the problem.
