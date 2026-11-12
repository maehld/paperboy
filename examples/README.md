# Example pages, plans and devices

The pages under `pages/` are grids of widgets from the shipped plugins
(ADR 0049): `weather`, `calendar`, `evcc`, `departures`, `pokedex`, `xkcd`,
`myskoda`, `countdown`, `rss` and the built-in `paperboy` plugin. Copy the
files you need into the `pages/` directory of your instance and adapt the
grid, the spans and the arguments.

- `uebersicht.yaml` - weather, energy flow from evcc, agenda and a three-day
  forecast.
- `kalender.yaml` - agenda next to a month grid.
- `woche.yaml` - the next seven days as calendar columns.
- `wetter.yaml` - current weather with clothing advice, five days and today's
  temperature curve.
- `laden.yaml` - evcc: energy flow, the charging point and the forecast;
  `laden.lcd2004.yaml` shows site and charging point on 20×4.
- `auto.yaml` - myskoda: battery, charging, climate, the vehicle image, charging
  plans and position; `auto.lcd2004.yaml` shows the overview on 20×4.
- `energie.yaml` - evcc energy flow with site values and the charging point.
- `ertrag.yaml` - evcc energy history: PV yield, battery discharge and the
  first loadpoint for today, yesterday, the week, the month and the year.
- `werte.yaml` - evcc values: meter readings and charging statistics as a
  list, without a graph.
- `abfahrten.yaml` - real-time departures; `abfahrten.lcd2004.yaml` on 20×4.
- `nachrichten.yaml` - two RSS feeds: headlines with summaries and a mixed
  list of all sources.
- `unterwegs.yaml` - current weather with clothing advice and the next
  departures, for leaving the house.
- `pokemon.yaml` - a full-screen Pokédex card.
- `comic.yaml` - a full-screen xkcd comic with title and alt text.
- `countdown.yaml` - countdowns to Christmas and New Year (yearly, one with a
  progress bar) and two fixed dates, the last with a time and a bar.
- `geburtstage.yaml` - the next birthdays with the age next to a short agenda
  that shows the age too; `geburtstage.lcd2004.yaml` puts them on 20×4.
- `uebersicht.lcd2004.yaml` - the overview on a 20×4 character display; its
  grid is the character grid.
- `hinweise.lcd2004.yaml` - a scrolling line on a 20×4 character display: a
  full-width cell with `scroll: true` shows a line that is too wide as a
  marquee instead of cutting it (ADR 0092).

A page without a display type suffix serves every display type that has no own
variant, so `uebersicht.yaml` works on 800×480 and on 400×300 panels alike: the
plugins draw every widget in the size of its cell. A text display needs its own
`<page>.<display type>.yaml`, because its grid must match the display.

The plugin names in the pages are the keys under `plugins` in `paperboy.yaml`;
`cmd/paperboy/paperboy.example.yaml` declares all of them.

`plans/standard.yaml` pages through its pages during the day and shows
the overview at night. Copy it into the `plans/` directory of your instance and
point a device at it with `plan: standard`. Its commented `evcc/mode` actions
switch a loadpoint's charge mode from the device menu, one "now" and one "PV
only" entry per loadpoint; uncomment the ones you need.

`plans/tour.yaml` rotates through every example page and so through all
shipped plugins: on weekday mornings weather, departures, the week and the
energy flow every 5 minutes, during the day all pages every 10 minutes, at
night only the overview.

`displays/e1001.yaml`, `displays/sticky.yaml` and `displays/lcd2004.yaml` are
three display types (ADR 0071): an 800 x 480 ePaper panel, the Sticky's denser
panel and a 20x4 text display. Copy them into the `displays/` directory of your
instance; the file name is what pages and devices use, and the editor at
`/ui/editor?tab=displays` creates more.

`devices/wohnzimmer.yaml` and `devices/lcd.yaml` are two devices (ADR 0070):
an ePaper display on the tour plan and a 20×4 text display on the standard plan.
Copy one into the `devices/` directory of your instance, set the MAC address and
the display type, or create the device in the editor at `/ui/editor`.

`plugins/demo.ics` is a small calendar for trying the calendar plugin without
an account: `config: { ics_files: [/path/to/demo.ics] }`.
