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

EFA names stops and destinations `<town>, <stop>` (e.g. „Bielefeld, Sieker“). The arg `locality_order` decides where the town sits, both in the header and in the destinations:

| Value | Effect |
|---|---|
| `after` (default) | `<stop>, <town>`: „Marktplatz, Beispielstadt“, „Bahnhof, Nachbarstadt“ |
| `before` | EFA's order: „Beispielstadt, Marktplatz“, „Nachbarstadt, Bahnhof“ |
| `none` | Only the stop: „Marktplatz“, „Bahnhof“ |

The header takes the stop from EFA's `disassembledName` and its town from `parent`; without them the part after the comma is the stop and the part before it the town.

The arg `locality` decides whether a destination shows its town at all:

| Value | Effect |
|---|---|
| `auto` (default) | Drop the town when it is the monitored stop's own town: „Sieker“ for „Bielefeld, Sieker“, but „Bahnhof, Herford“ for „Herford, Bahnhof“ (with `locality_order: before` both keep EFA's order) |
| `always` | Always drop it: „Bielefeld, Sieker“ becomes „Sieker“ |
| `never` | Always keep it; `locality_order` still decides where |

`locality_order: none` suppresses the town in every destination, whatever `locality` says. Image and text variants follow the same rules, and `directions` still matches the full name including the town. On image displays all destinations of a widget share one font size: the smallest size any of them needs to fit, but not below 0.8 of the base text. Destinations still too long at that size end with an ellipsis; line, delay and time keep their size.

The plugin reports a change when a departure, a delay or a cancellation changes.

The Westfalenfahrplan publishes no terms of use for this interface. Keep the refresh moderate and check the terms of any other installation you point it at.
