# 0143: A fuel price plugin with its own history

- Status: Accepted, implemented
- Date: 2026-11-20
- Builds on: ADR 0049 (plugin widgets), 0081 (plugin state), 0111 (values for text pages), 0120 (arguments for values), 0126 (schedule event)
- Answers: issue #353

## Context

Paperboy has no fuel prices. A display at the door should show the cheapest
stations nearby and how the price moved, so a household knows when to fill up.

[Tankerkönig](https://creativecommons.tankerkoenig.de) publishes the prices of
the German MTS-K under CC BY 4.0. The API needs a personal key. Its terms limit
the radius to 25 km, ask for at most one request per minute, and advise five
minutes for home automation. They also require attribution to Tankerkönig.

The radius search returns every station with the prices of E5, E10 and Diesel
in one answer. The API has no history endpoint. Tankerkönig sells an archive of
CSV files, but a reader needs an account and a repository clone.

A price moves a few times a day. A chart over a week needs samples, and a
restart must not erase them. Paperboy's state fits that role: the plugin sends
its state with an answer, and Paperboy writes it to
`state/plugins/<id>/state.json` (ADR 0081).

## Decision

1. One plugin `fuel` covers the German prices from Tankerkönig. Another country
   needs another plugin and another ADR, because the price models differ.
2. One radius request per fetch carries all three fuels. The page picks the
   fuel with the argument `fuel` of the widget or the value, so one fetch
   serves every cell.
3. The plugin fetches on demand through the schedule event (ADR 0126) and
   never more often than `refresh`. The default is 10m, the minimum 5m, the
   default radius 5 km and the maximum 25 km.
4. The cheapest station has the lowest price of the fuel. An open station wins
   over a closed one, and the nearer station wins a tie. The widgets and the
   history use the same rule, so the chart ends at the price of `cheapest`.
5. The plugin stores one series per fuel with the moments the cheapest price
   changed, not every sample. A series holds at most 1500 points and one point
   before `span` as the start of the line. The default span is one week.
6. The state carries a fingerprint of the key, the position and the radius. A
   change of one of them discards the series, because the old prices describe
   another place. A change of `refresh` or `span` keeps it.
7. The plugin declares values for text pages: the cheapest price, station,
   distance and open state, a list of the nearest stations, and the time of the
   last fetch (ADR 0111, 0120).
8. The plugin names Tankerkönig in its title and the documentation links the
   licence. That is the attribution the licence asks for.

## Consequences

- The chart shows the best price available, not the price of one station. A
  station that closes or undercuts another switches the series.
- The sampling interval sets the resolution. A price that changes and returns
  between two fetches is invisible. Finer samples would break the API terms.
- A closed station counts only while no open station offers the fuel. That
  keeps the page useful at night and in regions with fixed opening hours.
- A restart keeps the chart. A long outage shows as a flat line, because the
  last price holds until the next point.
- The state stays small: a few points per day per fuel. The cap of 1500 points
  keeps it far below the 256 KiB limit of ADR 0081.
- The operator needs a Tankerkönig key and puts it into `paperboy.yaml`. The
  plugin never logs or stores the key, the fingerprint is a hash.
- Without a display that needs the page the plugin costs no request. With a
  device that polls every 10 minutes the plugin fetches every 10 minutes.
- A broken answer keeps the last prices and logs the error once per change,
  like the other data plugins.

## Alternatives

- **The Tankerkönig archive.** An account and a git clone deliver the official
  history, but they add a second data path for a chart that the plugin can
  sample itself.
- **`prices.php` per station.** The radius search already carries every price
  in one answer. A second request would only add load.
- **A history of one station.** The operator would name a UUID, and the chart
  would show a station that may be closed or expensive. A per-station series
  also grows with every station on the page.
- **A sample every minute.** The API allows one request per minute, but its
  terms ask home automation for five. A minute would multiply the load for a
  chart that E-Paper cannot show.
- **A blob for the series.** The series is small JSON and fits the state. A
  blob is for data that is not JSON, such as an image (ADR 0081).
- **Storing every fetch.** Five to ten samples per hour would multiply the
  state without adding visible steps.
- **An Austrian source (E-Control).** Its API has another answer shape and no
  key but a referer. A second country is a separate decision.
