# evcc plugin

Shows data from [evcc](https://evcc.io) over its REST API (`/api/state`). The plugin polls the API in the background and sends a change event when a shown value moves.

## Configuration

| Key | Description |
|---|---|
| `url` | evcc address, e.g. `http://evcc:7070` |
| `refresh` | Poll interval, default `30s`, at least `10s` |

## Widgets

- `site`: PV, home, grid with import/export, home battery or grid price. Args: `columns` (`2` or `4`, default 2).
- `flow`: energy flow diagram arranged like the SolarAI dashboard: the house connection (hub) in the middle, PV above, the battery below, the grid left, the home right and up to three active loadpoints as small nodes right of the hub, below and above the home. Each box shows an [OpenMoji](https://openmoji.org) pictogram when it fits (CC BY-SA 4.0, see `cmd/evcc/icons/README.md`). Like Home Assistant's energy distribution, every node has exactly one link to the hub, always drawn: an idle link is a thin dashed line, an active one is solid with an arrowhead in the direction of the node's power (into the hub for PV, grid import and battery discharge, out of it for the home, the loadpoints, export and charging) and a width that grows with the power. The grid box reads "Grid import" or "Grid export". Idle boxes are dashed and their text is gray on four-level displays, so the diagram reads in two gray levels. In small cells the pictograms go first, then the state of charge in the labels, then the labels. Sizes follow the display ppi.
- `loadpoint`: vehicle, mode, a state-of-charge bar with the limit as a tick, range, charge power and phases, remaining time, session energy and solar share. Args: `index` (default 0).
- `forecast`: solar forecast as an area and the grid price as steps for the next `hours` (default 24). Image only.

`site`, `flow` and `loadpoint` have text variants. In the `flow` variant you get PV, home, one `Car` line per active loadpoint, the grid with + for import and - for export, and the battery with its state of charge and + for discharging / - for charging.
