# 0045: Text display poll interval from the plan

- Status: Accepted
- Date: 2026-10-31
- Amends: ADR 0044 (text displays on the device API), ADR 0025 (plans)

## Context

ADR 0044 fixed the text poll at one second in the firmware. First use showed three problems:

- How fast a text display follows the data is a matter of the page, not of the firmware: live power values want seconds, a weather page does not. Plans already decide the refresh, but rejected anything below one minute.
- Before the first sync (Wi-Fi up, key missing, MAC unknown) the firmware retried registration every second, so both the device log and Paperboy's log repeated the same failure every second.
- ESPHome's `http_request` logs every answer outside 2xx as a failed request, so each unchanged `304` showed up as an error in the device log.

Separately, the dashboard showed what the last sync rendered, while the display showed what the polls rendered since; with a page timeout or new data the two differ.

## Decision

1. **Plan refresh down to one second.** A plan rule's `refresh` may be as short as `1s`. Paperboy applies the floor of the display type: image displays never go below one minute (and their `min_refresh`), text displays go down to their `min_refresh`, which defaults to `1s` for `kind: text`.
2. **`refresh_s` paces the text poll.** For a text display the sync's `refresh_s` is the poll interval. The firmware syncs every `refresh_s` but at most once a minute, polls every `refresh_s`, and uses one minute for everything before the first sync (registration retries, polls). Notices on text displays report a one-minute refresh instead of 15 minutes, so a fixed plan shows up soon.
3. **204 instead of 304.** The device names the hash it shows in `GET /api/device/text?shown=<hash>`; unchanged text is answered with `204 No Content`. `ETag`/`If-None-Match` are dropped.
4. **The poll updates the visible state in memory.** When the polled content differs from the recorded one, the poll updates `last_text`, `last_page`, `last_page_kind`, `last_page_hash`, `last_page_time` and `last_error` in memory, so `/api/devices` and the dashboard show what the display shows. `state/devices.json` is still written by the sync only.

## Consequences

- A rule like `refresh: 2s` on a plan shared by image and text displays keeps image displays at their one-minute floor.
- Registration and other failures before the first sync appear once a minute in both logs.
- Clients that relied on `ETag`/`304` from ADR 0044 must send `?shown=`; only our firmware used the endpoint.
