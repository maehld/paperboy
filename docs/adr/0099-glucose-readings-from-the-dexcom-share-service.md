# 0099: Glucose readings from the Dexcom Share service

- Status: Accepted, implemented
- Date: 2026-11-13
- Builds on: ADR 0049 (plugins), 0055 (arg enums), 0081 (plugin state)
- Answers: issue #225

## Context

A Dexcom G7 sensor measures glucose every five minutes. Its readings appear on
a phone and, with Share switched on, in Dexcom's cloud. Three ways to get them
onto a display were on the table:

1. **Read the sensor over Bluetooth.** The G7 speaks a proprietary, encrypted
   protocol. Implementations exist, but only inside phone apps (Juggluco,
   xDrip+ with a bridge), and Paperboy would need a BLE stack and a reverse
   engineering effort of its own.
2. **Dexcom's developer API.** Officially documented, but only for approved
   partners; a private installation does not get access.
3. **The Share service.** Undocumented for third parties, but the path xDrip+,
   Nightscout's bridge and pydexcom have used for years, for the G6 and the G7
   alike: log in with the account, then read the values of the last day.

The third one is a plugin in the sense of ADR 0049: it talks to an HTTP service
and knows nothing about displays.

## Decision

### 1. The Share service, as an ordinary plugin

`cmd/dexcom` talks to `share2.dexcom.com` (Europe) or `shareous1.dexcom.com`
(US). The endpoints are used as they answer today: the account name is
exchanged for an account ID, the account ID and the password for a session, and
the readings are read with `sessionId`, `minutes` and `maxCount` in the query
and an empty body. A failed call is reported with the service's own code
(`AccountPasswordInvalid`, `SessionIdNotFound`), not as a bare HTTP status.

The plugin is shipped with the others, has its own `VERSION` (ADR 0075) and its
own page in `docs/plugins/`.

### 2. The region is a required configuration value

Which host answers does not follow the country the sensor sits in: a German
account was found on the US host. Guessing would mean a failing login and an
unclear error, so `region` (`eu` or `us`) is required and the error names the
mistake. The plugin never tries both.

### 3. The account ID and the session live in the state

Exchanging the account name for the account ID costs a request and the ID never
changes, so the plugin keeps it in its state (ADR 0081). The same goes for the
session: it is reused after a restart and only renewed when the service forgot
it (`SessionIdNotFound`), not on every poll. The account name is the state's
identity: a stored state of another account or region is ignored.

### 4. One day at first, then only the gap

The service keeps 24 hours. The first read takes the whole day (288 values);
each poll afterwards asks only for what arrived since the last read
(`maxCount` = the missed five-minute values plus one for context). The fresh
values are merged into the stored series, deduplicated by minute and pruned to
a day, so a poll moves a handful of values instead of a day, and the series is
complete again after a downtime of any length.

### 5. The numbers of the app, in the unit of the account

`urgent` (55), `low` (80), `high` (280) and the scale (`min` 40, `max` 300) are
the thresholds of the Dexcom app for mg/dL, configurable and with the mmol/L
equivalents (3.1, 4.4, 15.5, 2.2, 16.7) as their defaults. The unit comes from
the account and is detected by magnitude; `unit` overrides it.

The chart draws the band as two labelled lines and fills the area the curve
spends outside it. A tinted band (the first draft) hides the target zone and
says nothing about how far a reading went; the filled excursion does. The fill
is the mid gray of the palette, the strongest level that still leaves the black
curve readable, and it stays a row pattern for QOI (ADR 0088).

`glucose` inverts a reading outside the band and rings it below `urgent`, with
the word „Dringend!"/“Urgent!” — the one state a wall display has to show
without anyone opening the app.

## Consequences

- The plugin brakes the service: one read per interval, two values per poll in
  normal operation, and no login per restart.
- The data is only as fresh as the upload of the G7 app. A phone that is off or
  offline leaves the readings old, which the widget draws as a gray value with
  its age instead of pretending it is current.
- There is no week or month view. One day is what the service answers, and the
  plugin deliberately keeps no history of its own; a user who wants more needs
  Nightscout or Juggluco as the source, which a later plugin can read.
- The API is undocumented and can change or be closed. A broken answer becomes
  the cell's error, not a silently empty page.
