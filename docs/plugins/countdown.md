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

## Image variant

The label and the passed percentage share the first line, the number fills the
middle, the bar and the target date follow. Sizes come from the page's text
size (`Request.TextPx`, `MMPx`), so the widget scales with the cell. A cell
too small for every row drops the target date first, then the label, then the
bar; a one-row cell puts the label and the value side by side. The target date
shows no year when `date` repeats every year.

## Text variant

The first line carries the value, right-aligned next to the label (`34 Tage`,
`vorbei`, `6 Std 30 Min`); a narrower cell shortens it (`34 d`). With `since`
the second line is a bar of `#` and `-`, because the `hd44780` charset has no
block characters, and the third line is the target date. The value follows the
request's `language`.

## Refresh

The plugin asks Paperboy to keep its results until the next local midnight,
when a day count changes. A target with a time of day that is less than a day
away requests a new result after 30 seconds, so the minutes stay current.
