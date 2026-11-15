# evcc plugin

Shows data from [evcc](https://evcc.io) over its REST API: live values from `/api/state` and the energy history from `/api/history/energy`. The plugin polls the API in the background and sends a change event when a shown value moves.

## Configuration

| Key | Description |
|---|---|
| `url` | evcc address, e.g. `http://evcc:7070` |
| `refresh` | Poll interval for `/api/state`, default `30s`, at least `10s` |
| `history_refresh` | Interval for the energy history, default `15m`, at least `1m` |

## Widgets

- `site`: PV, home, grid with import/export, home battery or grid price. Args: `columns` (`2` or `4`, default 2).
- `flow`: energy flow diagram arranged like the SolarAI dashboard: the house connection (hub) in the middle, PV above, the battery below, the grid left, the home right and up to three active loadpoints as small nodes right of the hub, below and above the home. Each box shows an [OpenMoji](https://openmoji.org) pictogram when it fits (CC BY-SA 4.0, see `cmd/evcc/icons/README.md`). Like Home Assistant's energy distribution, every node has exactly one link to the hub, always drawn: an idle link is a thin dashed line, an active one is solid with an arrowhead in the direction of the node's power (into the hub for PV, grid import and battery discharge, out of it for the home, the loadpoints, export and charging) and the same width for every link. The grid box reads "Grid import" or "Grid export". Idle boxes are dashed and their text is gray on four-level displays, so the diagram reads in two gray levels. In small cells the pictograms go first, then the state of charge in the labels, then the labels. Sizes follow the display ppi.
- `loadpoint`: vehicle, mode, a state-of-charge bar with the limit as a tick, range, charge power and phases, remaining time, session energy and solar share. Args: `index` (default 0).
- `forecast`: solar forecast as an area and the grid price as steps for the next `hours` (default 24). Image only.
- `history`: energy totals for today, yesterday, this week (from Monday), last week, this month, last month and this year. Args: `source` (`pv` default, `battery`, `loadpoint`, `grid` or `home`), `metric` (`energy` default, or `return` for the battery's discharge and the grid's export) and `index` (the loadpoint of `/api/state`, default 0). The values come from evcc's energy history (`/api/history/energy`, one request per group with day buckets), so a year is one request; the long periods drop first when the cell is too small. The battery and the grid show the charged or discharged, imported or exported energy, the loadpoint its charged energy.
- `chart`: one series as bars, with the same `source`, `metric` and `index`. `period` selects what the bars are: `today` and `yesterday` one hour each, `week` and `last_week` one day each, `month` and `last_month` one day each, `year` (default) and `last_year` one month each. The heading names the series and the period, the value at the top right is the sum of the bars, the kWh axis stands on the right. Long labels thin out when the cell is narrow; a value dial is not drawn on the bars. Image only.
- `values`: the values evcc reports in `/api/state`, one row each with a label and a value. `values` selects them (a list, one check box per entry in the editor), `columns` (`1` to `4`, default 1) puts several next to each other for a wide, flat cell. The entries that do not fit are left out.

| `values` key | Shows |
|---|---|
| `pv`, `home`, `grid` | current power; the grid with + for import and - for export |
| `battery`, `battery-power` | state of charge and power of the home battery |
| `grid-price`, `feedin-price` | the tariffs evcc uses, in ct/kWh |
| `pv-energy` | PV energy since the installation (`pvEnergy`) |
| `grid-import`, `grid-export` | imported and exported energy since the installation |
| `battery-charge`, `battery-discharge` | charged and discharged energy since the installation |
| `charge-total`, `charge-total-solar`, `charge-total-price` | evcc's charging statistics over all sessions: energy, solar share, average price |
| `charge-year`, `charge-year-solar`, `charge-year-price` | the same for this year |
| `solar-today`, `solar-tomorrow` | the solar forecast of today and tomorrow |

The values are exactly what evcc reports. Paperboy does not derive numbers of its own: autarky, savings and the self-consumed share that evcc's energy page shows are computed in evcc's web UI from the history and the tariffs and have no API of their own; CO₂ needs a CO₂ tariff.

## Values

A text page reads these values ([ADR 0111](../adr/0111-text-pages-from-plugin-values.md)); a value evcc does not report becomes `–`:

| Value | Type | Unit | Precision | Meaning |
|---|---|---|---|---|
| `pv`, `home`, `grid` | number | kW | 1 | power; the grid with + for import and - for export |
| `battery` | number | % | 0 | state of charge of the home battery |
| `battery-power` | number | kW | 1 | battery power; + for discharge, - for charge |
| `grid-price`, `feedin-price` | number | ct/kWh | 1 | the tariffs evcc uses |
| `pv-energy` | number | kWh | 1 | PV energy since the installation |
| `grid-import`, `grid-export` | number | kWh | 1 | imported and exported energy since the installation |
| `battery-charge`, `battery-discharge` | number | kWh | 1 | charged and discharged energy since the installation |
| `charge-total` | number | kWh | 1 | energy of all charging sessions |
| `charge-total-solar` | number | % | 0 | solar share of all sessions |
| `charge-total-price` | number | ct/kWh | 1 | average price of all sessions |
| `charge-year`, `charge-year-solar`, `charge-year-price` | | | | the same for this year |
| `solar-today`, `solar-tomorrow` | number | kWh | 1 | solar forecast of today and tomorrow |
| `loadpoint` | text | | | vehicle or name of the first loadpoint |
| `loadpoint-mode` | text | | | its charge mode |
| `loadpoint-status` | text | | | charging, connected or not connected |
| `loadpoint-soc` | number | % | 0 | its state of charge |
| `loadpoint-limit` | number | % | 0 | its charge limit |
| `loadpoint-range` | number | km | 0 | its range |
| `loadpoint-power` | number | kW | 1 | its charge power |
| `session-energy` | number | kWh | 1 | energy of its session |
| `session-solar` | number | % | 0 | solar share of its session |
| `remaining` | duration | | | time until it is full |

The loadpoint values describe the first loadpoint, because a values request carries no arguments.

The history needs an evcc with the energy database (the *Energie* page in evcc's UI). An older evcc leaves the other widgets working: the history cell shows the HTTP error instead of values.

```yaml
cells:
  - { at: [0, 1], span: [2, 7], widget: evcc/history, args: { source: pv } }
  - { at: [2, 1], span: [2, 7], widget: evcc/history, args: { source: battery, metric: return } }
  - { at: [4, 1], span: [2, 7], widget: evcc/history, args: { source: loadpoint, index: 0 } }
```

## Actions

The plugin offers one action, `mode`, which sets an evcc charge mode from the device menu ([ADR 0057](../adr/0057-plugin-actions-from-the-plan.md)). A mode change affects the car, so Paperboy asks for a confirmation before the action runs.

| Argument | Description |
|---|---|
| `loadpoint` | Required. Index into `loadpoints` of `/api/state`, the same order the `loadpoint` widget uses. |
| `mode` | Required. One of `off`, `minpv`, `pv`, `now`. |

The title names the loadpoint and the mode, e.g. "Wohnmobil: jetzt laden"; a plan's `title` overrides it. The action is unavailable while evcc has no data yet, the loadpoint index does not exist or evcc already runs that mode. When evcc is down the entry stays visible as unavailable with the reason; nothing else breaks.

```yaml
actions:
  - action: evcc/mode
    args: { loadpoint: 0, mode: now }
    id: charge-now
    title: "Jetzt laden"
  - action: evcc/mode
    args: { loadpoint: 1, mode: pv }
    id: charge-pv
    title: "Nur PV"
```

A successful action reads `/api/state` again at once instead of waiting for the next poll, so the `loadpoint` widget shows the new mode after the next sync. Actions set a target state and never toggle, so a repeated sync is safe.
