# Example pages, plans and devices

The pages under `pages/` are grids of widgets from the shipped plugins
([ADR 0049](../docs/adr/0049-plugin-widgets.md)): `weather`, `calendar`, `evcc`, `departures`, `pokedex`, `xkcd`,
`myskoda`, `countdown`, `rss`, `github`, `dexcom`, `prusalink` and the built-in `paperboy` plugin. Copy the
files you need into the `pages/` directory of your instance and adapt the
grid, the spans and the arguments. A text page's value cells read the same
`args` as the widget, so a page picks its stop, loadpoint, calendar window or
date ([ADR 0120](../docs/adr/0120-arguments-for-values.md)).

- `uebersicht.yaml` - weather, energy flow from evcc, agenda and a three-day
  forecast.
- `status.yaml` - the battery of this device in the header, the status of this
  device and of two devices from the device files, and two batteries as large
  gauges with the clock beside them. Image displays only: a text page shows
  plugin values (ADR 0111).
- `kalender.yaml` - agenda next to a month grid.
- `woche.yaml` - the next seven days as calendar columns.
- `wetter.yaml` - current weather, five days and today's temperature curve.
- `spritpreise.yaml` - the cheapest station, the nearest stations and the price
  history of the day from Tankerkönig; `spritpreise.lcd2004.yaml` shows two
  prices, the cheapest station, the time of the fetch and a list on 20×4.
- `laden.yaml` - evcc: energy flow, the charging point and the forecast;
  `laden.lcd2004.yaml` shows PV, home and the first charging point from the
  evcc values on 20×4.
- `auto.yaml` - myskoda: battery, charging, climate, the vehicle image, charging
  plans and position; `auto.lcd2004.yaml` shows battery, charging and climate
  from the myskoda values on 20×4.
- `energie.yaml` - evcc energy flow with site values and the charging point.
- `energie-woche.yaml` - evcc: the energy flow and the balance of this week in
  kWh, with the page header naming the period.
- `ertrag.yaml` - evcc energy history: PV yield, battery discharge and the
  first loadpoint for today, yesterday, the week, the month and the year.
- `werte.yaml` - evcc values: today's energy, meter readings and charging
  statistics as lists, without a graph.
- `abfahrten.yaml` - real-time departures; `abfahrten.lcd2004.yaml` lists the
  stop and the departures with clock times on 20×4.
- `glukose.yaml` - the glucose reading next to the day with the numbers of the
  day below; `glukose.lcd2004.yaml` puts the reading, the trend, the change,
  the age and the labelled state on 20×4.
- `nachrichten.yaml` - two RSS feeds: headlines with summaries and a mixed
  list of all sources.
- `github.yaml` - the state of one repository: counts and the newest commit,
  open issues and pull requests, workflow runs and commits; `github.lcd2004.yaml`
  shows the counts and the state on 20×4.
- `drucker.yaml` - a Prusa printer from PrusaLink: the progress as a ring, the
  temperatures and the state card; `drucker.lcd2004.yaml` shows the state, the
  file, the progress and the temperatures on 20×4.
- `unterwegs.yaml` - current weather and the next departures, for leaving the
  house.
- `pokemon.yaml` - a full-screen Pokédex card.
- `comic.yaml` - a full-screen xkcd comic with title and alt text.
- `countdown.yaml` - countdowns to Christmas and New Year (yearly, one with a
  progress bar) and two fixed dates, the last with a time and a bar;
  `countdown.lcd2004.yaml` shows the countdown values with their dates on 20×4.
- `geburtstage.yaml` - the next birthdays with the age next to a short agenda
  limited to the private calendar; `geburtstage.lcd2004.yaml` scrolls through
  the birthday lines of the next 30 days on 20×4.
- `uebersicht.lcd2004.yaml` - boxes on a 20×4 character display with the
  built-in title and the values of weather, evcc and calendar
  ([ADR 0118](../docs/adr/0118-text-boxes-and-overflow.md)).
- `hinweise.yaml` - a text page with literal text and boxes of built-in values.

A page file `<page>.yaml` serves every display type of its kind, so
`uebersicht.yaml` works on 800×480 and on 400×300 panels alike: the plugins draw
every widget in the size of its cell. Its `variants` map names the exceptions
([ADR 0110](../docs/adr/0110-pages-serve-their-kind-and-name-variants.md)):
`uebersicht.yaml` names `lcd2004: lcd2004`, so `uebersicht.lcd2004.yaml` serves
the text display. A text page carries boxes of values or literal text, and every
box must fit the character grid of the display.

The plugin names in the pages are the keys under `plugins` in `paperboy.yaml`;
`server/cmd/paperboy/paperboy.example.yaml` declares all of them.

`plans/standard.yaml` pages through its pages and keeps the day fast and the
night quiet: the refresh is 30 minutes, with a 10-minute window from 07:00 to
22:00. Copy it into the `plans/` directory of your instance and point a device
at it with `plan: standard`. Its commented trigger shows a glucose page while
`dexcom/state` is low or urgent, and its commented `evcc/mode` actions switch a
loadpoint's charge mode from the device menu, one "now" and one "PV only" entry
per loadpoint.

`plans/tour.yaml` rotates through every example page and so through all
shipped plugins: it shows the glucose page while `dexcom/state` is out of its
band, brings `unterwegs` to the front on weekday mornings, and refreshes every
10 minutes during the day and every 30 minutes at night.

`displays/e1001.yaml`, `displays/sticky.yaml`, `displays/e1003.yaml` and
`displays/lcd2004.yaml` are four display types ([ADR 0071](../docs/adr/0071-display-types-in-files-of-their-own.md)): an 800 x 480 ePaper
panel, the Sticky's denser panel, the E1003's 1872 x 1404 16-level panel and a
20x4 text display. Copy them into the `displays/` directory of your instance;
the file name is what pages and devices use, and the editor at
`/ui/display-types` creates more.

`devices/wohnzimmer.yaml` and `devices/lcd.yaml` are two devices ([ADR 0070](../docs/adr/0070-devices-in-their-own-files.md)):
an ePaper display on the tour plan and a 20×4 text display on the standard plan.
Copy one into the `devices/` directory of your instance, set the MAC address and
the display type, or create the device in the editor at `/ui/devices`.

`plugins/demo.ics` is a small calendar for trying the calendar plugin without
an account: `config: { ics_files: [/path/to/demo.ics] }`.
