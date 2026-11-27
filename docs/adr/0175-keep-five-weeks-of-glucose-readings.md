# 0175: Keep five weeks of glucose readings

- Status: Accepted, implemented
- Date: 2026-11-27
- Builds on: ADR 0099 (glucose readings from the Dexcom Share service), ADR 0081 (plugin state), ADR 0169 (the glucose chart covers 24 hours)

## Context

The Share service answers at most the last 24 hours. The plugin keeps those readings for the chart and the values. A glucose profile folds several days onto one 24-hour grid, and a report needs 14 or 30 days. The plugin cannot ask for them.

The state is limited to 256 KiB (ADR 0081). The state stores 24 hours in the service's verbose form, about 25 KiB. The same form for 35 days would need about 900 KiB. The state must survive a restart, because the service cannot backfill.

## Decision

- **The plugin keeps 35 days of readings.** A rolling window of 30 days stays complete. The extra days cover the current day and the edge of a timezone.
- **The state stores the last 24 hours in the verbose form and the older readings compactly.** The verbose readings (`WT`, `ST`, `Value`, `Trend`) keep the trend and the exact second of the newest value. The history stores two parallel arrays per UTC hour: the minute of the hour and the value in tenths of the account unit. The plugin rebuilds `[]reading` on load.
- **The merge keeps the window.** A poll asks for the gap, merges the values and drops duplicates and anything older than 35 days.
- **The day widgets and values keep 24 hours.** They filter the longer series. A widget that needs several days filters the series with its own window.
- **Values stay raw.** The state does not keep precomputed shares. A changed target band therefore applies to the whole history.
- **The state stays below 256 KiB.** 35 days at five-minute values take about 128 KiB.
- **A version 1 state migrates.** Its readings of 24 hours start the history; the state version becomes 2.
- **The history starts with the first run.** A gap longer than 24 hours is lost. The service does not backfill.

## Alternatives

- **Keep only hourly or daily aggregates.** Small, but a profile needs the time of day and percentiles. Aggregates also fix the target band or lose the distribution.
- **Store the verbose readings for 35 days.** About 900 KiB, more than the state limit.
- **Use a blob.** A blob holds 1 MiB. The compact state fits, and a second file with base64 adds work for no gain today. Revisit if the window grows.
- **Keep 30 days exactly.** A rolling 30-day window and a timezone edge need a buffer. 35 days give it.
- **Read Nightscout or Juggluco.** Another service, another account, and the plugin already receives the readings.

## Consequences

- The state grows from about 25 KiB to about 128 KiB. The plugin sends it only after a change, not on every render. The host writes the whole state when a reading arrives, about 130 KiB every five minutes.
- A restart restores five weeks, so the first page has data and a profile starts with history.
- The values of the day stay on 24 hours. A comparison between a 24-hour value and a 14-day widget needs a clear label.
- The plugin version rises and the state version becomes 2. A version 1 state loses nothing; its readings start the history.
- A profile and a range report over several days become possible without another source.
