# 0103: Show a page while a plugin condition is active

- Status: Accepted
- Date: 2026-11-13
- Related: ADR 0025 (plans), ADR 0049 (plugins instead of a data document), ADR 0057 (actions from the plan), ADR 0081 (plugin state), ADR 0099 (dexcom)

## Context

ADR 0025 planned rules that react to data, not only to the clock: an open door at 22:00 should show a warning page. The condition was written as `when.data` with paths into the shared data document of ADR 0024. ADR 0049 replaced that document with plugins, so there are no paths any more; Paperboy has rejected `when.data` since then, and `docs/content.md` says so.

The use case remains: when the glucose reading falls below a threshold, the device should show the glucose page instead of its normal page, and back again when it recovers. The data now lives in the plugin that owns it, which also knows the thresholds (the dexcom plugin colours the widget with `urgent`, `low` and `high`).

## Decision

1. **Plans depend on named conditions, not on values.** A plugin declares conditions in `describe` (`conditions` with `name`, `title` and a description) and answers a `conditions` request with every declared condition and whether it is active now. Paperboy plans name them as `plugin/condition`.
2. **A rule combines conditions with its time window as AND**:

   ```yaml
   rules:
     - name: unterzucker
       when: { conditions: [dexcom/low] }
       pages: [zucker]
     - name: tag
       when: { from: "07:00", to: "22:00" }
       pages: [uebersicht]
   ```

   The first matching rule wins as before, and `fallback` works unchanged. A rule with conditions and no time window matches whenever they are active.
3. **Missing data never raises an alarm.** A plugin that is not configured, a condition it does not declare, a failing answer and a condition the plugin itself calls stale all count as inactive. The plan then falls through to its next rule. The plugin decides what "stale" means, because it knows its data; dexcom reports every condition as inactive while the newest reading is older than 15 minutes.
4. **Thresholds and units stay in the plugin's configuration.** The plan names a state, never a number, so `low: 100` in the plugin configuration moves both the widget's colouring and the page switch, and a plan is not tied to mg/dL or mmol/L.
5. **The state is asked outside the state lock.** Device handlers take a condition snapshot before they lock, keyed by `plugin/condition`, and pass it to the plan selection (sync, text poll, virtual display, plan view). The widget host caches a plugin's answer for ten seconds and drops it on a `changed` event, so a sync never waits for a plugin's network work and a text display polling every second does not ask every time.
6. **Reaction time is the next wake.** Conditions change the page at the next sync; a sleeping device learns about them no earlier than its `refresh`. The rule that shows the normal page sets how quickly that is. Conditions do not wake a device.
7. **The protocol grows additively** to version 6: `describe.conditions`, the `conditions` request and `message.conditions` (a map of name to active, a missing name is inactive). Version 5 plugins stay usable; their conditions are simply never active.
8. **The editors and the plan view show conditions.** The plan editor offers the declared conditions as check boxes per rule and warns about names no configured plugin declares. The plan view writes `if dexcom/low` into the rule and shows each condition as active or inactive; conditions are evaluated at the current moment even when the view shows another time, because only time windows can be moved.
9. **Dexcom is the first plugin with conditions:** `urgent`, `low` (including the urgent range) and `high`, following its configured thresholds, inactive while the reading is stale.

## Alternatives

- **Values with comparisons** (`when.values`, `below: 100`): more general, but it puts units and thresholds into plans, duplicates them against the widget, and needs type handling in the plan file. Rejected for now; a later ADR can add it on top of conditions.
- **Conditions on widgets** (`when: { widget: dexcom/glucose, state: low }`): the same plugin work with less clarity, and it ties the plan to a specific widget.
- **A push route the plugin calls** to override the plan: nearer to instant, but a sleeping device only sees it at its next wake anyway, and it adds priority, expiry and conflict rules that the plan model does not have.
- **Only an always-present glucose page in the rotation:** no code, but the page shows whether or not the reading is low.

## Consequences

- A plan stays readable and display-independent: rules say "when the glucose is low, show this page", not how low is defined.
- Every data source that wants to steer pages needs a small plugin change: declare the conditions and answer the request. The threshold semantics stay where the operator already configures them.
- Plans cannot compare numbers, so a new threshold means a change in `paperboy.yaml`, not in the plan. That is the price of keeping units and colouring consistent.
- A plugin that is down silently keeps its alert page away. The widget host logs the failed request once, and the page's stale bar still shows that data is old.
- The plan view can no longer promise which rule is active at a future time when conditions are involved; it says so and shows the live state.
- Old plans with `when.data` are refused with a hint that names `when.conditions`.
