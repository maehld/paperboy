# paperboy plugin

The built-in plugin `paperboy` runs inside Paperboy and needs no configuration. Its widgets draw on image pages; its values feed text pages ([ADR 0111](../adr/0111-text-pages-from-plugin-values.md)).

## Values

A text page lays out values ([ADR 0111](../adr/0111-text-pages-from-plugin-values.md)). The plugin declares:

| Value | Type | Content |
|---|---|---|
| `title` | text | the page title |
| `time` | timestamp | the current moment |
| `date` | timestamp | midnight of the current day, so Paperboy writes the date |
| `now` | timestamp | the current moment, for a layout test |
| `long` | text | a line wider than a 20-column display, for `overflow: scroll` |
| `block` | text | four lines, one with a very long word, for `overflow: wrap` |

```yaml
cells:
  - { at: [0, 0], span: [20, 1], value: paperboy/title }
  - { at: [0, 1], span: [20, 1], value: paperboy/date, label: Datum }
  - { at: [0, 2], span: [20, 1], value: paperboy/time, label: Zeit, align: right }
```

## Widgets

- `header`: page header. Args: `title` (default the page title), `subtitle`, `clock`, `date` (weekday and date before the clock, in the longest of four forms that fits, e.g. „Samstag, 7. November 2026“, „Sa, 7.11.2026“, „Sa, 7.11.“, „7.11.“; left out when none fits), `page_indicator`, `align` (`left`, `center` or `right`, default `left`) and `battery` (default `false`: the gauge of this device at the right, as high as a capital letter of the date, without a number).
- `battery`: the battery of one device as a gauge with a label. Args: `device` (id; default the device that shows the page), `label` (`auto`, `percent`, `volts` or `none`, default `auto`: the percentage, otherwise the voltage) and `align` (`left`, `center` or `right`, default `center`). Spans: `1x1`, `2x1` and `3x1` put the value beside the gauge, `2x2` puts it below the gauge ([ADR 0121](../adr/0121-segmented-battery-gauge.md)). The gauge fills the cell height and draws the charge in four or five segments, so 84 % and 100 % differ at a glance. A bolt marks a charging battery. A slash marks a missing reading instead of an empty outline, and `-` replaces the value. A `warn` or `critical` level thickens the outline. The value shrinks to keep the gauge, and it wins when the cell is too narrow for both.
- `device-status`: one row per device with battery, estimated runtime and the time since the last contact. Args: `devices` (ids; default the device that shows the page), `battery`, `runtime` and `seen` (each `bool`, default `true`). A device that is overdue carries `!`; a device that never contacted Paperboy or has no device file carries `?`. The stalest device comes first.
- `text`: fixed text. Args: `text` (with `\n` for line breaks), `align` (`left`, `center` or `right`, default `center`), `size`, `bold`.
- `clock`: the current time. Args: `format` (`time`, `date` or `datetime`, default `time`).
- `logo`: the Paperboy mark — the page of four by three cells with one cell drawn and the top-right corner turned down ([docs/logo](../logo/README.md)) — centered in the cell and as large as the cell allows. Arg `wordmark` (default `false`) adds the wordmark in Go Mono Bold, so the cell shows the lockup. Image displays only.

```yaml
- { at: [0, 0], span: [4, 3], widget: paperboy/logo, args: { wordmark: true } }
- { at: [0, 0], span: [6, 1], widget: paperboy/header, args: { battery: true } }
- { at: [0, 1], span: [6, 2], widget: paperboy/device-status }
- { at: [0, 3], span: [6, 2], widget: paperboy/device-status, args: { devices: [wohnzimmer, flur] } }
- { at: [0, 5], span: [2, 2], widget: paperboy/battery }
```

Arguments with a fixed set of values reject anything else ([ADR 0055](../adr/0055-allowed-arg-values-and-gallery-form.md)); the Playground offers them as choices. The page editor offers the configured device ids for `battery.device` and `device-status.devices`, and names the empty choice *the device that shows the page* ([ADR 0123](../adr/0123-a-sample-device-in-previews.md)).

The battery, `device-status` and the header's battery read `server_status` from the request (ADR 0115). Only the built-in plugin gets this object; Paperboy keeps no key, MAC or address in it. The page that shows its own device is rendered once per device, so two displays of the same type keep their own status. A preview without a device shows the sample device `preview` with fixed values, 78 %, 4.05 V, not charging and a runtime of about 12 days ([ADR 0123](../adr/0123-a-sample-device-in-previews.md)).

A text page shows these values, not widgets ([ADR 0118](../adr/0118-text-boxes-and-overflow.md)). A box that scrolls takes its content from a value and moves it with the display type's `scroll.step`; a box that wraps or strips cuts it instead.
