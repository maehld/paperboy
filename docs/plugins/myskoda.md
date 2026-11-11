# myskoda plugin

Shows the state of a Škoda with a connected-vehicle subscription over the [MyŠkoda Public API](https://public.api.connect.skoda-auto.cz/docs). The plugin reads the vehicle in the background, keeps the last answer in memory and sends a change event when a shown value moves. It offers two device-menu actions: start and stop charging, and start and stop air conditioning (see [Actions](#actions)).

The API returns a snapshot: every part carries its own `carCapturedTimestamp`, so the widget shows when the car last sent its data. Parts a vehicle does not support are absent, and a field without a usable value is omitted — the widgets show `–` or leave the line out instead of inventing a value.

## Configuration

```yaml
plugins:
  myskoda:
    config:
      api_key: ${SKODA_API_KEY}
      vin: TMBJB9NY5RF999999
      refresh: 1h
```

| Key | Description |
|---|---|
| `api_key` | Required. API key created in the MyŠkoda app, bound to the vehicle. |
| `vin` | Required. Vehicle identification number, 17 characters. The API has no vehicle list. |
| `refresh` | Poll interval, default `1h`, at least `10m`; shorter values are raised to `10m` with one log line. |
| `base` | API base URL, default `https://public.api.connect.skoda-auto.cz`. |

**Rate limit:** the API allows 20 requests per hour and vehicle, commands included. The default of one request per hour leaves almost the whole quota free; a `429` is answered by waiting for `Retry-After` or `RateLimit-Reset`, and the last data stays on screen. A failing request is retried at the next interval, not in a loop.

**Key expiry:** every answer carries `X-API-Key-Expires-At`. The plugin logs one line when less than seven days are left; an expired key (`401`, `api-key-expired`) fails the widgets with a notice that names the MyŠkoda app, because a key can only be created there — there is no refresh flow. A `403` (`api-key-not-authorized`) means the key does not cover this VIN.

**Restarts:** the plugin keeps what it needs between restarts as plugin state and a blob (ADR 0081): `state/plugins/myskoda/state.json` holds the last answer, when it was loaded, the next allowed attempt, the key expiry, a fingerprint of the key and the last error, and `state/plugins/myskoda/blobs/render.png` the vehicle image. Paperboy fills both from the plugin's answers and hands them back in the `state` request after a restart. A Paperboy restart within the refresh interval therefore costs no request and shows the last state at once, and a `429` with `Retry-After` stays in force across a restart. A changed VIN or API key, or state that cannot be read, discards it; the key itself is never written.

**Licence:** the MyŠkoda Public API is for personal use only. Commercial use, resale or redistribution is not allowed.

## Widgets

All widgets except `card` have text variants for character displays.

- `battery`: state of charge as a large number with a bar, range, charging state and power, remaining time or time of full charge, target state of charge, and the capture time. Args: `range` (default `true`).
- `charging`: charging state and power, state of charge against the target, remaining time, the current charging profile with its next charging time, capture time. Args: `profile` (default `true`).
- `profiles`: the saved charging profiles with target state of charge and their preferred charging times and timers. The current profile is marked with `*`. Args: `max` (default 2, 1 to 5).
- `location`: the formatted address, the coordinates, `PARKED` or `IN_MOTION` and the capture time. Args: `coords` (default `true`).
- `climate`: air conditioning state and target temperature, the expected time to reach it, window heating, auxiliary heating and active ventilation when they run, capture time.
- `card`: the vehicle render image dithered onto the display's gray levels (Floyd–Steinberg, like the Pokédex card) with the vehicle name and a state line (state of charge, range, charging state). Args: `name` (default `true`), `state` (default `true`). Image only.
- `car`: a compact overview for small cells: name, optional license plate, state of charge and range, charging state, lock state, odometer, and a state-of-charge bar. Args: `plate` (default `false`).

The render image comes from `info.renderUrl` and is downloaded once per URL, cached in memory and never committed; a missing image (`RENDER_UNAVAILABLE`) leaves the card without it instead of failing. The API key is not sent to the image host.

The charging state comes from `charging.status.state` and keeps short labels, because it shares a line with the power and, on character displays, with the range: `Getrennt` (`CONNECT_CABLE`), `Bereit` (`READY_FOR_CHARGING`), `Lädt`, `Erhaltung` (`CONSERVING`), `Entlädt` and `Unterbrochen` (`CHARGING_INTERRUPTED`); in English `Unplugged`, `Ready`, `Charging`, `Holding`, `Discharging`, `Interrupted`. An unknown value is shown as it is. Whether the cable is connected is already part of that state, so no widget repeats it from `plugConnectionState`.

Text variants:

| Widget | Lines |
|---|---|
| `battery` | `Akku  71 %`, charging state with power and range, remaining time, target |
| `charging` | state and remaining time, state of charge against the target, current profile |
| `profiles` | one or two lines per profile, current one marked with `*` |
| `location` | wrapped address, coordinates, capture time (or `Unterwegs`) |
| `climate` | state and target temperature, expected time, window heating and auxiliary states |
| `car` | name and state of charge, range and charging state with lock state, odometer |

## Actions

The plugin offers two actions for the device menu (ADR 0057). Both target a state instead of toggling, so a retried request is safe, and both ask for confirmation before they run.

| Action | Args | Endpoint |
|---|---|---|
| `charging` | `state: start\|stop` | `POST /api/v1/vehicles/{vin}/charging/{start\|stop}` |
| `climate` | `state: start\|stop`, `temperature` (optional) | `POST /api/v1/vehicles/{vin}/air-conditioning/{start\|stop}` |

A plan lists them as `myskoda/charging` or `myskoda/climate`:

```yaml
actions:
  - { action: myskoda/charging, args: { state: start }, id: charge-start, title: "Laden starten" }
  - { action: myskoda/charging, args: { state: stop }, id: charge-stop, title: "Laden stoppen" }
  - { action: myskoda/climate, args: { state: start }, id: climate-start, title: "Klima starten" }
  - { action: myskoda/climate, args: { state: stop, temperature: 21 }, id: climate-stop, title: "Klima stoppen" }
```

`temperature` is the target in °C, 16 to 30 in 0.5 steps; without it the car's current target is used, rounded to 0.5 °C. Air conditioning does not need a security PIN — only auxiliary heating does, and that command is not part of the plugin.

An action is disabled in the menu when there is no data yet, when the vehicle does not list the command in `operations`, or when the target state already holds: `start` charging needs a connected cable and no running charge, `stop` charging needs a running one; `start` air conditioning needs it to be off, `stop` needs it to run. A `202` answer counts as success, other errors (`403`, `422`, `429`) become a short message. After a successful command the plugin reloads the state at once and reports `changed`; each command counts against the same 20 requests per hour as a read.

## Differences from the MyŠkoda app

The API also offers:

- further remote commands: charging limit, charge mode, charging profile updates, auxiliary heating (needs the security PIN `spin`) and active ventilation;
- fuel status for combustion and plug-in hybrid cars;
- door, window, sunroof and light detail, tire pressure is not part of the API.

Doors, windows and lights beyond the overall lock state, fuel status and those further commands are not implemented yet.

## Example

`examples/pages/auto.yaml` shows battery, charging, climate, the vehicle image, charging plans and position on a 6×8 grid; `examples/pages/auto.lcd2004.yaml` shows the overview and climate on a 20×4 character display. The tour plan `examples/plans/tour.yaml` includes the page.
