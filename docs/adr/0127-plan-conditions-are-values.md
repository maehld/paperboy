# 0127: Plan conditions are values

- Status: Accepted
- Date: 2026-11-16
- Related: ADR 0025 (plans), ADR 0055 (argument values), ADR 0075 (plugin versions), ADR 0107 (plugin builds), ADR 0111 (text pages from plugin values), ADR 0120 (arguments for values), ADR 0122 (unit progression), ADR 0126 (schedule event), issue #286
- Supersedes: ADR 0103 (plugin conditions in plans)
- Extends: ADR 0111, ADR 0120, ADR 0122 (the value model)

## Context

ADR 0103 gave plans a second data model. A plugin declared named booleans in
`describe.conditions`, answered them on a `conditions` request, and Paperboy
cached the answer for ten seconds. Values already had declarations, arguments,
a cache and demand. A condition also could not appear on a page, and it could
not carry the keys of a state such as `normal | low | high | urgent`.

ADR 0126 makes Paperboy compute the demand per plugin. A plan condition is a
second reason to read a plugin, so the special case remains until conditions
become values.

## Decision

1. **A plan condition names a value and its matching keys.** `when.conditions`
   stays a list, but every entry is an object:

   ```yaml
   when:
     conditions:
       - { value: dexcom/state, is: [low, urgent] }
   ```

   `is` accepts one key or a list. The keys within one entry are alternatives
   (OR), and several entries must all match (AND). A missing value, a failing
   plugin and an unknown key mean no match, so a rule never shows an alert page
   for data that is gone. The old string form is rejected with a hint; there is
   no migration code.

2. **A value type `enum` carries the keys.** `describe.values` gains `enum`,
   the allowed keys. The answer is one key: `{"enum":"low"}`. Paperboy rejects
   a key outside the declaration and shows `?` for an empty answer. The plugin
   carries no labels; a key is stable data.

3. **A text cell carries the labels.** A value cell gets `labels`, a map from
   an enum key to display text. Paperboy shows the label when it is not empty
   and the key otherwise. Only an enum value cell may carry labels. A plan
   condition and the plan view show the key, because a condition is not a page.

4. **The second model is gone.** `describe.conditions`, the `conditions`
   request, `plugin.Conditioner`, `Message.conditions` and the ten-second
   condition cache are removed. Protocol 9 has not shipped, so it absorbs the
   change: it adds the schedule event, the enum value type and the cell labels,
   and removes plan conditions. The version stays 9 and the minimum stays 5.

5. **Paperboy reads plan values before page selection.** It collects the
   distinct values of all rules, fetches them through the normal value cache
   with the device and a minimal page, and passes the answered key of each
   `plugin/value` to `Plan.Select`. The call runs outside the state lock.
   Missing and failing values are omitted. The demand takes the plugin from
   each condition value and needs no special case.

6. **The editors work with keys.** The plan editor offers the enum values of the
   configured plugins and one check box per key. It warns about a value or key
   that no plugin declares, about duplicates and about an empty key list. A
   stored unknown value or key stays visible. The page editor shows one label
   input per declared key of a cell's enum value.

7. **Dexcom migrates.** Its three conditions become one value `state` with the
   keys `normal`, `low`, `high` and `urgent`. The configured thresholds and
   their precedence stay in the plugin: urgent wins over low and high. A missing
   or stale reading answers no key. The example page labels the keys; the tour
   plan shows the glucose page while the state is out of its band.

## Alternatives

- **Labels in the plugin declaration.** The plugin follows `language` and the
  editor reads the labels directly. But a plan condition would then show the
  plugin's sentence, which can be long and changes with the language. The label
  also cannot differ per page. A label per use keeps the file stable and the
  text short.
- **Keep conditions next to values.** Two declarations, requests and caches
  remain, and a condition still cannot appear on a page. Rejected by this ADR.
- **Numeric comparisons in the plan** (`below: 100`). More general, but it puts
  units and thresholds into plans and duplicates them against the widget. The
  enum solves the Dexcom case; a later ADR can add comparisons.
- **A compatibility path for `when.conditions: [dexcom/low]`.** Paperboy has no
  migration code for v1 content and the shipped examples change in the same
  commit. A parser hint is enough.

## Consequences

- One declaration, request, cache and demand path replaces two.
- A condition is visible on a text page and in the plan editor.
- A page keeps its readable labels while the file keeps the key, so a change of
  wording does not touch a plan.
- `pkg/plugin` changes, so every plugin binary changes and raises its version
  (ADR 0075, 0107). Dexcom also changes its declaration.
- A plugin that wants to steer a page declares an enum value instead of a
  condition. A non-enum value cannot take part in a plan condition.
- A plan condition cannot use a label. The plan view names the key and its
  current value.
- Old plans with condition strings are refused with a hint. The operator edits
  them by hand.
- The page editor can label only the keys that the plugin declares now. An
  unknown key keeps its key on the display.
