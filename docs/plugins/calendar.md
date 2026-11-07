# calendar plugin

Calendars from CalDAV and iCalendar (ADR 0048). The plugin reads the calendars in the background and reports changes.

## Configuration

| Key | Description |
|---|---|
| `caldav`, `user`, `password` | CalDAV access; set the password with `${VAR}` |
| `calendars` | Calendar names; default all. An unknown name fails and lists the available ones |
| `ics` | Links; `webcal://` works |
| `ics_files` | Local iCalendar files |
| `days` | Default 14 |
| `max_events` | Maximum number of events |
| `refresh` | Default `15m` |

For iCloud, create an app-specific password at [account.apple.com](https://account.apple.com) and use your Apple ID as `user`. Nextcloud works with an address like `https://cloud.example.org/remote.php/dav`.

The plugin expands recurring events itself. If an event uses a rule it does not support (hourly, `BYWEEKNO`, `BYYEARDAY`), it skips that event and logs a warning.

## Widgets

- `agenda`: day headings, all-day events first, events running into the range from earlier days count, ended events disappear, „+N weitere“ when there are more events than fit. Args: `lines`, `headings`, `location`; `days` limits the range to today plus the following days, 1 to the configured `days` (values outside are clamped), e.g. `days: 2` for today and tomorrow; `empty` (default true when `days` is set) keeps days without events with „Keine Termine“/"No events".
- `month`: month grid with titles or event bars. Args: `titles: false`.
- `week`: one column per day. Args: `days` 1-14 (default 7, e.g. 5 for a work week) and `start: week|today`; the week starts on Monday for German and on Sunday otherwise. Headings show the weekday and date, today inverted. All-day and multi-day events appear as bars across their days, timed events with their start time and an ellipsized title, and „+N weitere“ marks events that did not fit. The text variant lists one line per day starting today.

All widgets have text variants.
