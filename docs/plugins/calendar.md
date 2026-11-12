# calendar plugin

Calendars from CalDAV and iCalendar (ADR 0048). The plugin reads the calendars in the background and reports changes.

## Configuration

| Key | Description |
|---|---|
| `caldav`, `user`, `password` | CalDAV access; set the password with `${VAR}` |
| `calendars` | Calendar names; default all. An unknown name fails and lists the available ones |
| `birthday_calendars` | Calendars whose yearly events are birthdays; see below. An unknown name fails like `calendars` |
| `ics` | Links; `webcal://` works |
| `ics_files` | Local iCalendar files |
| `days` | Default 14 |
| `max_events` | Maximum number of events |
| `refresh` | Default `15m` |

`calendars` filters at load time and applies to every display. Each widget can narrow the loaded set further with its own `calendars` argument; an empty list shows all loaded calendars. The editors offer the loaded names as check boxes, and an unknown name is an error that lists the available calendars. Before the first successful load the configured names are offered instead; if those are empty too, the editors keep a comma-separated text field.

For iCloud, create an app-specific password at [account.apple.com](https://account.apple.com) and use your Apple ID as `user`. Nextcloud works with an address like `https://cloud.example.org/remote.php/dav`.

The plugin expands recurring events itself. If an event uses a rule it does not support (hourly, `BYWEEKNO`, `BYYEARDAY`), it skips that event and logs a warning.

## Birthdays

The contacts apps of iCloud and Nextcloud publish a read-only calendar built from the contact birthdays: a yearly all-day event per contact that starts on the date of birth (e.g. `DTSTART;VALUE=DATE:19850412` with `RRULE:FREQ=YEARLY`), so the age can be read from the year of the series. Add the calendar to `calendars` and name it in `birthday_calendars`:

```yaml
plugins:
  calendar:
    config:
      caldav: https://caldav.icloud.com
      user: me@icloud.com
      password: ${PAPERBOY_CALDAV_PASSWORD}
      calendars: [Privat, Geburtstage]
      birthday_calendars: [Geburtstage]
```

iCloud names the calendar after the account language (`Geburtstage`, `Birthdays`); both keys match case-insensitively, like `calendars`. The year counts from the first occurrence of the series, so it also works when the calendar only hands out the current year's instance. A missing or implausible year (before 1900, the day of birth itself, more than 130 years) leaves the title alone.

A birthday on 29 February falls on 28 February in a common year, so it does not vanish three years out of four; in a leap year it stays on the 29th.

## Widgets

- `agenda`: day headings, all-day events first, events running into the range from earlier days count, ended events disappear, „+N weitere“ when there are more events than fit – behind the last event when it fits next to it, otherwise on a line of its own. A heading only takes a line when another one is left for the events it introduces, and a cell with events never claims that there are none. Args: `lines`, `headings`, `location` (multi-line locations such as Apple's name, street and city are joined with commas); `days` limits the range to today plus the following days, 1 to the configured `days` (values outside are clamped), e.g. `days: 2` for today and tomorrow; `empty` (default true when `days` is set) keeps days without events with „Keine Termine“/"No events"; `age` appends the age of birthdays from `birthday_calendars` („Anna wird 34“/"Anna turns 34", as „Anna (34)“ on text displays); `calendars` narrows the events to the named calendars.
- `birthdays`: only the next birthdays from `birthday_calendars`, always with the age, one line per birthday with the day heading. Args: `lines`, `headings`; `days` limits the range to today plus the following days, 1 to the loaded window – the configured `days`, but at least the 42 days the month grid needs – and defaults to that window; `calendars` narrows the birthday calendars further. A cell without a birthday in range says „Keine Geburtstage“/"No birthdays". Without `birthday_calendars` it stays empty.
- `month`: month grid with titles or event bars. Args: `titles: false`, `calendars`.
- `week`: one column per day. Args: `days` 1-14 (default 7, e.g. 5 for a work week), `start` (`week` or `today`, default `week`; `today` on text displays) and `calendars`; the week starts on Monday for German and on Sunday otherwise. Headings show the weekday and date, today inverted. All-day and multi-day events appear as bars across their days, timed events with their start time and an ellipsized title, and „+N weitere“ marks events that did not fit. The text variant lists one line per day starting today.

All widgets have text variants.
