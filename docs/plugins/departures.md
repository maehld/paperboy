# departures plugin

Real-time public transport departures from an EFA installation (ADR 0053). The default is the Westfalenfahrplan (Lippe Mobil, OWL), which needs no API key.

## Configuration

| Key | Description |
|---|---|
| `stop` | Required: a stop name (resolved once) or a stop ID like `de:00000:1001` |
| `lines`, `directions` | Filters; `directions` matches words in the destination |
| `max` | Number of departures, default 8 |
| `refresh` | Default `60s`, at least `30s` |
| `walk_minutes` | Hide departures you can no longer reach |
| `base` | Another EFA installation, e.g. `https://efa.vrr.de/standard` |
| `backend` | Only `efa` |

## Widget `departures`

Each line shows the line number, the destination, the minutes until departure (a clock time from one hour on; `time: auto|minutes|clock`) and the delay as `+N`. Cancelled trips are struck through with „Ausfall“. `header: true` adds the stop name. The text variant fits 20 × 4 displays (`examples/pages/abfahrten.lcd2004.yaml`).

The plugin reports a change when a departure, a delay or a cancellation changes.

The Westfalenfahrplan publishes no terms of use for this interface. Keep the refresh moderate and check the terms of any other installation you point it at.
