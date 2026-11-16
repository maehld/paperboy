# 0122: Hide and progress a text value's unit

- Status: Accepted
- Date: 2026-11-16
- Related: ADR 0111 (text pages from plugin values), ADR 0120 (arguments for values), ADR 0107 (plugin builds reach installations), issue #269

## Context

ADR 0111 gives the formatting of a value to Paperboy. A value declaration carries a unit such as `kW`, and a page may override that text. Two gaps remain.

1. A page cannot hide the unit. An empty `unit` falls back to the declared unit, so a page cannot ask for no unit.
2. The plugin picks the magnitude. evcc divides watts by 1000 and declares `kW`. A large PV day never becomes `MW`, and every plugin repeats that choice. Two pages can show the same reading in different units.

## Decision

We reserve the unit text `none` and let a value declare the steps of its unit.

1. **`unit: none` hides the unit.** A cell's `unit` stays the override text. Absent shows the declared unit, `none` shows no unit, and other text replaces the declared unit.
2. **A value declares its base unit and its steps.** `describe.values` gains `progression`, a list of `{unit, factor, precision}`. `unit` is the base unit the plugin answers in, e.g. `W`, and `precision` its decimals. Protocol 7 grows additively, so older Paperboy ignores the fields.
3. **Paperboy chooses the step.** It rounds the value with a step's precision and takes the largest step whose rounded absolute value is at least 1. Zero stays in the base unit, and a negative value chooses by its absolute value. The chosen step's precision applies; the language still decides the decimal separator.
4. **A cell may force a step.** The new cell key `scale` is `auto` (default) or the unit of a declared step. The base unit itself is a valid scale. An unknown step marks the cell with an error, like an unknown argument.
5. **The unit text and the magnitude stay apart.** `unit` overrides or hides the unit text, and `scale` picks the magnitude.
6. **The editor offers choices.** The unit field is a choice of declared, none and custom, because an empty text field cannot tell declared from none. A value with a progression shows its steps in a scale select.
7. **evcc migrates first.** Powers arrive in W and energies in Wh, with progressions to kW/MW and kWh/MWh. Other plugins with scalable units follow.

## Alternatives

- **Let every plugin keep choosing the unit.** The rule stays duplicated, two pages show the same reading in different units, and a page cannot compare neighbours.
- **A format string per value.** It puts layout and formatting back into the plugin and breaks the language rules.
- **A plugin argument for the unit.** A generic concern would need a per-plugin argument everywhere and still no cross-plugin rule.
- **Scale only in the page.** The plugin must answer in a base unit anyway, so the declaration has to carry it.
- **A second cell key for the unit.** `none` is one reserved word and keeps the existing key.

## Consequences

- `pkg/plugin` grows, so every plugin binary changes and raises its version (ADR 0075, 0107).
- A page shows a reading in the unit that fits its box. The plugin no longer decides the magnitude.
- A value without a progression behaves exactly as before.
- A step below the base unit (mW, Wh) stays out of scope. A page-wide default for `scale` is possible later.
- The rounding rule steps at 1 of the chosen unit, so 0.5 kW stays 500 W. A later ADR may change that threshold.
