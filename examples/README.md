# Example pages and plans

The templates under `pages/` are starting points for the SolarAI reference pages
(overview, battery, daily balance and daily history). Copy the files you need
into the pages directory of your installation (`directories.pages`, relative to
`paperboy.yaml`) and adapt the texts, thresholds and sizes to your data.

The display type suffix selects the template: `solar-overview.e1001.yaml` is used
for devices with `display_type: e1001`, `solar-overview.sticky.yaml` for devices
with `display_type: sticky`. Both layouts are independent files because the
compact variant has fewer rows and no chart.

`plans/solar.yaml` shows a plan that pages through the four pages during the day
and returns to the overview at night. Copy it into the plans directory
(`directories.plans`) and point a device at it with `plan: solar`.

The templates expect the data paths described in ADR 0024 and ADR 0034:
`solar/current/latest`, `solar/history/points` and `solar/derived`.
