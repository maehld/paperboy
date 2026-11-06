# Example pages and plans

The pages under `pages/` are grids of widgets from the shipped plugins
(ADR 0049): `weather`, `calendar`, `evcc`, `solar` (the `solardemo` program, a stand-in
for a SolarAI plugin) and the built-in `paperboy` plugin. Copy the files you
need into the `pages/` directory of your instance and adapt the grid, the
spans and the arguments.

- `uebersicht.yaml` – weather, solar values, agenda and a three-day forecast.
- `kalender.yaml` – agenda next to a month grid.
- `wetter.yaml` – current weather with clothing advice, five days and today's
  temperature curve.
- `solar.yaml` – solar tiles and the power chart.
- `laden.yaml` – evcc: energy flow, the charging point and the forecast;
  `laden.lcd2004.yaml` shows site and charging point on 20×4.
- `uebersicht.lcd2004.yaml` – the overview on a 20×4 character display; its
  grid is the character grid.

A page without a display type suffix serves every display type that has no own
variant, so `uebersicht.yaml` works on 800×480 and on 400×300 panels alike: the
plugins draw every widget in the size of its cell. A text display needs its own
`<page>.<display type>.yaml`, because its grid must match the display.

The plugin names in the pages are the keys under `plugins` in `paperboy.yaml`;
`cmd/paperboy/paperboy.example.yaml` declares all of them.

`plans/standard.yaml` pages through the four pages during the day and shows
the overview at night. Copy it into the `plans/` directory of your instance and
point a device at it with `plan: standard`.

`plugins/demo.ics` is a small calendar for trying the calendar plugin without
an account: `config: { ics_files: [/path/to/demo.ics] }`.
