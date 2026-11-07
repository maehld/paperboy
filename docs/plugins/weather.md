# weather plugin

Weather from [Open-Meteo](https://open-meteo.com/), no API key needed.

## Configuration

| Key | Description |
|---|---|
| `lat`, `lon` | Required |
| `refresh` | Default `15m` |
| `base` | Another Open-Meteo address |

## Widgets

- `current`: pictogram, temperature, condition, apparent temperature, wind force and today's range. `clothing: true` adds a T-shirt or a pullover; the pullover appears below 15 °C apparent temperature or above wind force 3.
- `days`: pictogram and range per day. Args: `days: 1..5`.
- `hours`: today's temperature with the precipitation probability. Image only.

`current` and `days` have text variants.

The pictograms come from [OpenMoji](https://openmoji.org) (CC BY-SA 4.0, see `cmd/weather/icons/README.md`).
