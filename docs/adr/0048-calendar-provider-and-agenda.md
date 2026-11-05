# 0048: Calendar provider over CalDAV and the agenda block

- Status: Accepted
- Date: 2026-11-05
- Builds on: ADR 0032 (providers), ADR 0034 (layout primitives), ADR 0036 (exec providers)

## Context

Displays should show upcoming appointments. The calendars live with iCloud and similar services. A public iCalendar link is the simplest source, but iCloud only publishes calendars that the user shares publicly, so the link is readable by anyone who has it, and private calendars need CalDAV with an app-specific password. Paperboy itself stays free of source-specific code (ADR 0032), and the page blocks so far can only show a fixed number of values, not a list of variable length.

## Decision

1. **Provider `cmd/calendar`.** An exec provider (ADR 0036) writes the `calendar` branch. It reads one CalDAV account and any number of iCalendar links (`-ics`, `-ics-env`, `webcal://` becomes `https://`):
   - CalDAV discovery follows `/.well-known/caldav`, `current-user-principal`, `calendar-home-set` and the calendar collections that support `VEVENT` (RFC 4791, RFC 5397). `-calendar` selects calendars by display name; an unknown name is an error that lists the available ones.
   - Events come from a `calendar-query` REPORT with a time range from local midnight today over `-days` days (default 14). The provider expands recurrences itself, because server-side expansion is optional and differs between servers.
   - Redirects are followed by hand with the same method, body and credentials: Go's client turns PROPFIND into GET on 301/302 and drops credentials on a host change, while iCloud moves every account from `caldav.icloud.com` to its own partition host.
   - The password comes from an environment variable (`-password-env`, default `PAPERBOY_CALDAV_PASSWORD`), never from an argument; error messages strip credentials and queries from URLs.
   - Any failing source makes the run fail, so Paperboy keeps the previous data and shows its age instead of a calendar with appointments missing.
2. **Own iCalendar parser, no new dependency.** Unfolding, parameters, text escapes, `DTSTART`/`DTEND`/`DURATION`, all-day dates, `TZID` (IANA names plus a table of common Windows names, otherwise the configured timezone with a warning), `STATUS:CANCELLED`, `EXDATE`, `RDATE` and overrides by `RECURRENCE-ID`. `RRULE` supports `FREQ` `DAILY` to `YEARLY` with `INTERVAL`, `COUNT`, `UNTIL`, `BYDAY` (with ordinals), `BYMONTHDAY`, `BYMONTH`, `BYSETPOS` and `WKST`. An event with an unsupported rule is skipped with a warning on standard error; the rest of the calendar stays.
3. **Branch layout.** `updated` (run time), `from`, `to`, `calendars` (names), `events` and `days`. An event has `start` and `end` as RFC 3339 in UTC, `all_day`, `date` and `end_date` (local days, `end_date` inclusive), `title`, `location` and `calendar`. All-day events start at local midnight of the configured timezone. `days` lists every day of the window with its weekday `label` and the events touching it, for templates that want a fixed day grid.
4. **Block `agenda`.** `agenda: { events: calendar/events, lines: 6, headings: true, location: false, empty: ..., size, rule, padding }`, shorthand `agenda: calendar/events`. It draws a fixed number of lines: a bold heading per day („Heute“, „Morgen“, „Mo, 9.11.“ / "Today", "Tomorrow", "Mon, Nov 9" from the global language), then time and title per event. All-day events come first in a day, events that ended are left out, an event that started on an earlier day appears under today as „bis 18:00“ or „ganztägig“. When the lines run out, the last line says „+N weitere“ and a heading never stays alone at the end. Titles are shortened with an ellipsis instead of shrinking the font, so lines keep one size. Without headings the time column carries the weekday for later days.

## Consequences

- iCloud, Nextcloud, Fastmail, Radicale and other CalDAV servers work with the same provider; one provider owns one branch, so a second account is a second provider on another path.
- The recurrence subset covers what calendar apps create. Rare rules (hourly, `BYWEEKNO`, `BYYEARDAY`) are visible as warnings and do not break the page. Custom `VTIMEZONE` definitions are not evaluated; unknown zone names fall back to the configured timezone.
- The day labels of the agenda are built-in texts in German and English like the stale notice; other languages need the same extension as the rest of the built-in texts.
- The provider was tested against a fake iCloud server with a partition redirect; the first run against the real iCloud is still open.
