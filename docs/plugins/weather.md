# weather plugin

Weather from [Open-Meteo](https://open-meteo.com/), no API key needed.

## Configuration

| Key | Description |
|---|---|
| `lat`, `lon` | Required |
| `refresh` | Default `15m` |
| `base` | Another Open-Meteo address |

## Widgets

All widgets are for image displays.

- `current`: pictogram, temperature, condition, apparent temperature, wind force and today's range. `clothing: true` adds a T-shirt or a pullover; the pullover appears below 15 °C apparent temperature or above wind force 3.
- `days`: pictogram and range per day. Args: `days: 1..5`.
- `hours`: today's temperature with the precipitation probability.

The pictograms come from [OpenMoji](https://openmoji.org) (CC BY-SA 4.0, see `cmd/weather/icons/README.md`).

## Values

A text page reads these values ([ADR 0111](../adr/0111-text-pages-from-plugin-values.md)):

| Value | Type | Unit | Precision | Meaning |
|---|---|---|---|---|
| `temperature` | number | °C | 1 | now |
| `apparent` | number | °C | 1 | feels like |
| `humidity` | number | % | 0 | relative humidity |
| `precipitation` | number | mm | 1 | now |
| `precipitation_probability` | number | % | 0 | now |
| `wind` | number | km/h | 1 | now |
| `beaufort` | number | Bft | 0 | wind force |
| `condition` | text | | | condition in the global language |
| `min_today` | number | °C | 1 | lowest today |
| `max_today` | number | °C | 1 | highest today |
| `sunrise` | timestamp | | | today |
| `sunset` | timestamp | | | today |
| `updated` | timestamp | | | time of the forecast |
| `forecast` | text | | | one line per day: label, condition and range |

`paperboy` formats the numbers and the timestamps; the plugin never pads or
aligns them.
