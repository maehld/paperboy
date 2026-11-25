# 0163: The readings in the device status and a reading widget

- Status: Accepted, implemented
- Date: 2026-11-25
- Builds on: ADR 0134 (readings from the device), 0115 (device status in a widget), 0123 (a sample device in previews), 0151 (a shared design language)
- Issue: [#444](https://github.com/maehld/paperboy/issues/444)

## Context

ADR 0134 brought the SHT4x readings into the sync, the state and the dashboard. It deferred the plugin status and the page: "the plugin status and the page values follow with the widget that shows them." A page still cannot show the room it hangs in, and issue #326 is closed for the device side.

The header is not the place for that value. Its right side already carries the date, the clock and the battery gauge, and a header redesign is its own task (#298).

## Decision

1. **`plugin.DeviceStatus` carries `Readings`**, a map from catalog name to `Reading{Value, Taken}`. The bare number and the moment travel, no unit and no precision: the catalog in `server/internal/reading` stays the only owner of name, unit, precision and range, so one value keeps one spelling.
2. **The protocol version does not change.** `server_status` is additive and reaches only the built-in plugin, which is compiled with the server ([ADR 0115](0115-device-status-in-a-widget.md)). Every plugin program links the new field, so every plugin version rises ([ADR 0107](0107-plugin-builds-reach-installations.md)), even though no plugin process receives it.
3. **The built-in plugin gains the widget `reading`.** Its `name` is an enum from the catalog, so a later sensor needs no new widget. `device` names another device and defaults to the device that renders the page; `label` draws the catalog title above the value, `unit` adds the unit, `age` adds the time since the measurement. Spans are `1x1`, `2x1`, `3x1` and `3x2`. The value draws through `design.DrawHero` ([ADR 0151](0151-a-shared-design-language-for-plugin-widgets.md)); a short cell drops the label before the value.
4. **The page language decides the number and the label**: `21,5 °C` on a German page, `21.5 °C` on an English one, and `Temperatur` next to `Temperature`. The dashboard keeps its English spelling ([ADR 0031](0031-everything-in-english.md)).
5. **A device without a reading shows a notice in its cell**, not a zero and not a failing page. A reading older than an hour shows its age in the caption, so a fresh page never implies a fresh measurement.
6. **The plugin declares the values `paperboy/temperature` and `paperboy/humidity`** as `number` with the catalog unit and precision, so a text page shows the room too. A device without that reading fails the value, and the cell shows its notice.
7. **A reading widget without `device` makes the page device-scoped**, so the render cache keeps one picture per device, like `battery` and `device-status` ([ADR 0115](0115-device-status-in-a-widget.md)). A named device joins `selected_device_status`.
8. **The sample device of a preview reports 21.5 °C and 45 %** ([ADR 0123](0123-a-sample-device-in-previews.md)), and the screenshots mock carries the same values, so a preview of the widget and the docs show a value.

## Alternatives

- **A header argument `reading` or `climate`:** the header's right side is contested by date, clock and gauge, so the value would be dropped on most cells. The header redesign owns this idea (#298).
- **A widget per sensor:** `temperature` and `humidity` as two widgets means a third widget for the next sensor. The generic readings list of ADR 0134 already answers that with a `name` argument.
- **Unit and precision on the wire:** the firmware would then decide how a value reads, and a wrong constant would look plausible. The catalog stays the single source.
- **No value for text pages:** the text display is the cheapest place for a room value, and issue #326 promised it.
- **Hide a reading older than a threshold:** the page would lose its value silently. The age tells the operator why it is old.

## Consequences

- The built-in plugin is the first drawing code outside `plugins/` that uses `sdk/design`; the design language gains a second kind of user.
- Every plugin version rises, because the field lives in `sdk/plugin` and every program links it ([ADR 0107](0107-plugin-builds-reach-installations.md)).
- The catalog gains a German title next to the English one, because the widget and the values follow the page language.
- A page that shows its own room is rendered and cached per device, so two devices of the same display type keep their own values.
- Calibration stays a device-file offset ([ADR 0134](0134-readings-from-the-device.md), point 8) and applies to the stored value, so dashboard and widget show the same number.
