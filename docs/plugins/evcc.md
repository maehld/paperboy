# evcc plugin

Shows data from [evcc](https://evcc.io) over its REST API (`/api/state`). The plugin polls the API in the background and sends a change event when a shown value moves.

## Configuration

| Key | Description |
|---|---|
| `url` | evcc address, e.g. `http://evcc:7070` |
| `refresh` | Poll interval, default `30s`, at least `10s` |

## Widgets

- `site`: PV, home, grid with import/export, home battery or grid price. Args: `columns: 2|4`.
- `flow`: energy flow diagram with boxes for PV, grid, home, the home battery with its state of charge and up to three active loadpoints. Arrows join the boxes; their direction shows the flow and their width grows with the power. Idle links are dashed, so the diagram reads in two gray levels. Sizes follow the display ppi.
- `loadpoint`: vehicle, mode, a state-of-charge bar with the limit as a tick, range, charge power and phases, remaining time, session energy and solar share. Args: `index` (default 0).
- `forecast`: solar forecast as an area and the grid price as steps for the next `hours` (default 24). Image only.

`site`, `flow` and `loadpoint` have text variants. In the `flow` variant you get PV, home, one `Car` line per active loadpoint, the grid with + for import and - for export, and the battery with its state of charge and + for discharging / - for charging.
