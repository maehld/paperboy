# paperboy plugin

The built-in plugin `paperboy` runs inside Paperboy and needs no configuration. All widgets work on image and text displays.

## Widgets

- `header`: page header. Args: `title` (default the page title), `subtitle`, `clock`, `date` (weekday and date before the clock, in the longest of four forms that fits, e.g. „Samstag, 7. November 2026“, „Sa, 7.11.2026“, „Sa, 7.11.“, „7.11.“; left out when none fits), `page_indicator`, `align` (`left`, `center` or `right`, default `left`).
- `text`: fixed text. Args: `text` (with `\n` for line breaks), `align` (`left`, `center` or `right`, default `center`), `size`, `bold`.
- `clock`: the current time. Args: `format` (`time`, `date` or `datetime`, default `time`).

Arguments with a fixed set of values reject anything else (ADR 0055); the widget gallery offers them as choices.
