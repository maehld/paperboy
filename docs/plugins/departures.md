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

Each line shows the line number, the destination, the minutes until departure (a clock time from one hour on; `time`: `auto` (default), `minutes` or `clock`) and the delay as `+N`. Cancelled trips are struck through with „Ausfall“. `header: true` adds the stop name. The text variant fits 20 × 4 displays (`examples/pages/abfahrten.lcd2004.yaml`).

EFA names destinations `<town>, <stop>` (e.g. „Bielefeld, Sieker“). The arg `locality` decides about the town:

| Value | Effect |
|---|---|
| `auto` (default) | Drop the town when it is the monitored stop's own town: „Sieker“, but „Herford, Bahnhof“ |
| `always` | Always drop it: „Sieker“, „Bahnhof“ |
| `never` | Always keep it |

Image and text variants follow the same rule, and `directions` still matches the full name including the town. On image displays all destinations of a widget share one font size: the smallest size any of them needs to fit, but not below 0.8 of the base text. Destinations still too long at that size end with an ellipsis; line, delay and time keep their size.

The plugin reports a change when a departure, a delay or a cancellation changes.

The Westfalenfahrplan publishes no terms of use for this interface. Keep the refresh moderate and check the terms of any other installation you point it at.
