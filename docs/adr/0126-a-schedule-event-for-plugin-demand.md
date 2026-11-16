# 0126: A schedule event for plugin demand

- Status: Accepted; protocol 9, the helper, the host demand and the cache gate are implemented, the plugin migrations follow
- Date: 2026-11-16
- Related: ADR 0045 (the text poll), ADR 0049 (plugins), ADR 0057 (actions), ADR 0058 (the protocol version check), ADR 0080 (device state), ADR 0103 (plan conditions), ADR 0107 (plugin builds), issue #276
- Amends: ADR 0058 (the protocol can add an inbound event; Paperboy gates it on the version)

## Context

A plugin polls its source in its own `Start` loop: weather every 15 minutes, evcc every 30 seconds, MyŠkoda every hour. Paperboy cannot influence the timer. Its widget cache keeps every entry alive in a second loop: `maintain` re-renders an entry before `max_age` while it was used within an hour, and every render sets `lastUsed = now`, so the one-hour idle eviction never fires.

The result is data and requests for pages that no display shows. A device that syncs every 30 minutes costs weather four API reads per hour instead of two. A page removed from every plan still keeps its plugin polling. The principle is "no data that no display shows", and today a plugin cannot know when its data is needed.

## Decision

1. **Paperboy computes the demand per plugin.** For every configured device with a plan and a known contact, the next expectation is `last_seen + refresh` from `deviceState` (ADR 0080). At that contact the device needs the pages of the rule it is on (`st.Rule`) that fit its display type, plus the plugins of the plan's `when.conditions`. The earliest expectation over all devices is the demand.
2. **The demand has limits.** Actions do not create demand: Paperboy resolves them when the menu opens (ADR 0057), so prefetching for a menu that may never open is waste. A late or never-seen device creates no demand until it contacts again; its first or late sync is served from memory and refreshes on demand. Virtual displays, editor previews and the built-in plugin add nothing. A plugin that no device uses has no demand.
3. **A schedule event carries the moment.** Protocol 9 adds an inbound line `{"event":"schedule","next_refresh":"..."}`. A missing or null `next_refresh` means no display needs the plugin now. Paperboy sends the event when a plugin starts and when the moment changes by more than about a minute. It sends it only to a plugin that reports version 9 or newer, so an older plugin never sees an unknown line.
4. **The plugin decides.** `refresh` stays the plugin's own minimum interval and rate limit. `pkg/plugin.Demand` implements the event: `Wait(ctx, floor)` sleeps until `next_refresh` minus 30 seconds, wakes on a new schedule, and never returns earlier than `floor` after the call. A plugin calls it after each fetch, so the floor counts from the end of the fetch. With no demand it blocks. A plugin may wait longer, refuse a fetch and keep the last data, or idle; it still answers a request from memory.
5. **The cache follows the demand.** `maintain` refreshes an expired entry only when the plugin's demand is within 30 seconds. Otherwise the entry stays expired until a device asks; the plugin answers from memory, so no device waits. This ends the re-render chain, and idle eviction works again.
6. **Text displays keep their demand.** A text display polls every second (ADR 0045), so its demand is "now" and the plugin's `refresh` floor decides. The event still carries the next moment.

## Alternatives

- **Let the server force a fetch.** Rejected: the plugin owns its rate limit and its data source, and a forced fetch can fail a page that would have answered from memory.
- **Pull on every request.** The device would wait for the network; plugins answer from memory today.
- **Compute the demand per widget or data source.** More precise, but departures (one stop) and calendar (one calendar) would need a second dimension. A follow-up.
- **Keep polling on the plugin's timer.** That is the current cost. The event is a hint, not a command.
- **Raise the minimum protocol version to 9.** Every plugin ships with Paperboy, but an older value-only plugin still works. Gating the event on the reported version is enough.

## Consequences

- The protocol gains one inbound event. Older plugins are not sent it; newer plugins ignore an event they do not know.
- A protocol 9 plugin can run on an older Paperboy, which sends no schedule. It must keep its own timer then; the request's `version` says which host runs it. Myskoda is the reference: it uses `Demand` on version 9 and polls on its interval below that.
- Every plugin binary changes and raises its version (ADR 0075, 0107) because `pkg/plugin` changed.
- The prediction can be wrong: a late device, a button press or a rule switch by a condition. The effect is at most one sync with older data, never a missing page or a timeout, because the plugin answers from memory.
- A plugin used only for a condition cannot be skipped, but it idles between contacts; otherwise the alert page never appears.
- The host demand computation and the cache gate follow in separate commits. This ADR records the target for both.
- The host demand and the cache gate are implemented; the plugin migrations follow.
