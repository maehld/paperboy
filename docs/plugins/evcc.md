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
- `history`: energy totals for today, yesterday, this week (from Monday), last week, this month, last month and this year. Args: `source` (`pv` default, `battery`, `loadpoint`, `grid` or `home`), `metric` (`energy` default, or `return` for the battery's discharge and the grid's export) and `index` (the loadpoint of `/api/state`, default 0). The values come from evcc's energy history (`/api/history/energy`, one request per group with day buckets), so a year is one request; the long periods drop first when the cell is too small. The battery and the grid show the charged or discharged, imported or exported energy, the loadpoint its charged energy. Text displays get one line per period with short labels.

`site`, `flow`, `loadpoint` and `history` have text variants. In the `flow` variant you get PV, home, one `Car` line per active loadpoint, the grid with + for import and - for export, and the battery with its state of charge and + for discharging / - for charging.

The history needs an evcc with the energy database (the *Energie* page in evcc's UI). An older evcc leaves the other widgets working: the history cell shows the HTTP error instead of values.

```yaml
cells:
  - { at: [0, 1], span: [2, 7], widget: evcc/history, args: { source: pv } }
  - { at: [2, 1], span: [2, 7], widget: evcc/history, args: { source: battery, metric: return } }
  - { at: [4, 1], span: [2, 7], widget: evcc/history, args: { source: loadpoint, index: 0 } }
```

## Actions

The plugin offers one action, `mode`, which sets an evcc charge mode from the device menu (ADR 0057). A mode change affects the car, so Paperboy asks for a confirmation before the action runs.

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
