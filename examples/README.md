# Example pages and plans

The templates under `pages/` are starting points for the SolarAI reference pages
(overview, battery, daily balance and daily history) and for the weather page of
the Open-Meteo provider. Copy the files you need into the `pages/` directory of
your instance directory and adapt the texts, thresholds and sizes to your data.

The display type suffix selects the template: `solar-overview.e1001.yaml` is used
for devices with `display_type: e1001`, `solar-overview.sticky.yaml` for devices
with `display_type: sticky`. Both layouts are independent files because the
compact variant has fewer rows and no chart.

`plans/solar.yaml` shows a plan that pages through the four pages during the day
and returns to the overview at night. Copy it into the `plans/` directory of your
instance directory and point a device at it with `plan: solar`.

The templates expect the data paths described in ADR 0024, ADR 0034 and ADR 0036:
`solar/current/latest`, `solar/history/points`, `solar/derived` and `weather`
(the branch of `cmd/weather`).

`icons/` holds single-colour PNG pictograms for the `icon` block (ADR 0038):
`sun`, `cloud`, `rain`, `snow`, `thunder`, `tshirt` and `pullover`. They are
taken from [OpenMoji](https://openmoji.org) (black style, CC BY-SA 4.0); see
`icons/README.md` for the source files and the licence.

Copy the files you need into the `icons/` directory of the instance. Paperboy
reduces each picture to a mask, trims the margins, so black motifs on a
transparent or white background both work and the same file renders on 1-bit
and 4-gray displays.
