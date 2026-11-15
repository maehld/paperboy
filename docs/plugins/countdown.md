# countdown plugin

Counts the days to a date and, with a reference date, shows how much of the
span has already passed. The dates and the label are widget arguments, so one
page may show several countdowns and the plugin has no configuration of its
own.

## Configuration

None. Declare the plugin with an empty section:

```yaml
plugins:
  countdown: {}
```

## Widget `countdown`

| Arg | Description |
|---|---|
| `date` | Required: the target as `YYYY-MM-DD`, `YYYY-MM-DDTHH:MM`, or `MM-DD` for a date that repeats every year |
| `label` | Text above the number, optional |
| `since` | Reference date for the progress bar: `YYYY-MM-DD`, `YYYY-MM-DDTHH:MM` or `MM-DD` (the occurrence before `date`); without it the widget shows no bar |

```yaml
cells:
  - { at: [0, 0], span: [3, 2], widget: countdown/countdown,
      args: { label: Weihnachten, date: "12-24", since: "01-01" } }
```

Days are calendar days in the configured `timezone`, not 24-hour spans, so a
DST change does not shift a count. On the target day the widget shows
*heute* / *today*; after a date that does not repeat it shows a tick and
*vorbei* / *passed*. A target with a time of day counts hours and minutes once
less than a day is left (`6 Std 30 Min`).

`MM-DD` repeats every year: `12-24` is the next 24 December, today included.
A 29 February falls on 1 March in the other years, because the date is
normalized. `MM-DD` in `since` is the occurrence before `date`, so
`since: "01-01"` with a yearly target measures the current year.

## Layout

The label and the passed percentage share the first line, the number fills the
middle, the bar and the target date follow. Sizes come from the page's text
size (`Request.TextPx`, `MMPx`), so the widget scales with the cell. A cell
too small for every row drops the target date first, then the label, then the
bar; a one-row cell puts the label and the value side by side. The target date
shows no year when `date` repeats every year.

## Values

A text page reads these values ([ADR 0111](../adr/0111-text-pages-from-plugin-values.md), [0120](../adr/0120-arguments-for-values.md)). They read the value arguments `date` (required) and, for `percent`, `since`. Without `date` the value fails, because a countdown has no target.

| Value | Type | Meaning |
|---|---|---|
| `value` | text | the main text: `34 Tage`, `6 Std 30 Min`, `heute` or `vorbei`, in the request's language |
| `target` | timestamp | the target date |
| `days` | number | calendar days to a date-only target, negative after it; 0 when the target is a time less than a day away |
| `percent` | number | the passed share of `since` to `date`, in percent; a dash without `since` |

The `value` text follows the widget's wording, so a page that wants the number alone uses `days`. The label is the cell's, not a value argument: `label: Weihnachten` in front of `countdown/value` writes the same line as the widget.

## Refresh

The plugin asks Paperboy to keep its results until the next local midnight,
when a day count changes. A target with a time of day that is less than a day
away requests a new result after 30 seconds, so the minutes stay current.
