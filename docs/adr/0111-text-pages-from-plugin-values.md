# 0111: Text pages from plugin values

- Status: Proposed
- Date: 2026-11-15
- Related: ADR 0033 (language and timezone), ADR 0044 (text displays), ADR 0045 (poll interval), ADR 0049 (plugin widgets), ADR 0051 (power source), ADR 0066 (page API), ADR 0068 (page editor), ADR 0092 (scrolling lines), ADR 0110 (pages name their display types)

## Context

A text page uses the widget pipeline of ADR 0049. Paperboy sends `text` with character width and height, and every widget answers lines. All ten shipped plugins carry text variants of their widgets.

For an image display drawing belongs to the plugin. The widget owns the pixels of its cell, and the page is a 6 × 8 reference grid with physical sizes (ADR 0050).

A text display is a table. The grid is the character grid of the panel: 20 × 4 on the LCD2004, at most 80 × 24 (ADR 0044). A cell is a run of characters in a row. The choices that matter belong to the page: column widths, a label column, a unit column, alignment at the decimal separator, truncation.

The widget model cannot express those choices. Every plugin lays out its own rows, pads its own labels and formats its own numbers. The same reading looks different in two widgets. The language rules of ADR 0033 reach a text display only as far as each plugin applies them.

Paperboy v1 formatted values itself (ADR 0024, ADR 0046). ADR 0049 moved drawing to the plugins. For text that also moved layout and number formatting there, which is one step too far. The `values` widgets of evcc and dexcom already draw a list of label/value rows: the data model exists, buried in a drawing widget.

## Decision

1. **A text page is a layout of rows and columns.** It carries `kind: text` and `display_types` (ADR 0110). It has no pixel grid, because the character grid is the display's. The parser rejects `grid` and `cells` on a text page with a hint.
2. **Plugins declare values.** `describe` gains a `values` list per plugin: key, label, type, unit, precision and a flag for a list. A list value holds records with named fields. The type is `number`, `text`, `duration`, `timestamp` or `direction`. A value is data, not text.
3. **A new action `values` reads them.** The request carries the page, the device, `language`, `timezone` and `now`, like `render`. One request per plugin and page covers all keys of that page. The answer holds numbers, strings and records. Cache, `max_age` and the `changed` event work as for widgets (ADR 0049).
4. **Paperboy formats.** Numbers follow the global language (decimal separator, grouping), and durations and dates use the existing formatters. Characters map to the charset of the display. A stale value marks `!` in the last column of its row, as today.
5. **Columns carry the layout.** A column has a width, an alignment (`left`, `right`, `decimal`) and an optional label and unit. Decimal alignment lines up the separators of a column, whatever the values are.
6. **Lists render with `for-each`.** A row or block repeats once per record, up to a limit. Scrolling through a long list moves rows; ADR 0092 scrolls characters inside a row.
7. **The old path survives the migration.** A text page may hold a widget row until the last plugin provides values. A later protocol version removes the `text` action and the widget row; until then the protocol grows additively (ADR 0049).
8. **Text pages get their own editor.** It edits rows and columns, picks values from `/api/plugins` and previews one target type. It validates against the width and the charset of the display. Image pages keep the board editor (ADR 0068).
9. **The page stays the unit of a plan.** Text pages keep their names, their place in plans and their notices.

```yaml
# pages/werte.yaml, sketch: the design document owns the keys.
title: Werte
kind: text
display_types: [lcd2004]
rows:
  - columns:
      - {width: 8, align: left, label: true}
      - {width: 6, align: decimal}
      - {width: 6, align: right, unit: true}
    cells:
      - {value: weather/temperature}
      - {value: evcc/pv}
      - {value: evcc/home}
```

## Alternatives

- **Keep text widgets and improve the plugins.** Alignment across cells, units and number formatting stay duplicated. A page cannot align the numbers of two widgets.
- **A value that carries preformatted lines.** Cheap for Paperboy, but the layout and the formatting stay in the plugin. That is exactly the state this ADR replaces.
- **Paperboy fetches the data itself.** That is the data document of ADR 0024, which ADR 0049 removed.
- **Render text pages as images and send them to the panel.** A text panel paints characters from its own charset. An image needs a font grid and loses the LCD character set.
- **Add format arguments to every text widget.** Each widget duplicates the alignment, and a cell still cannot align with its neighbour.
- **Derive the layout from the value type.** A table needs page-wide columns, and a value cannot know its neighbours.

## Consequences

- All ten plugins migrate their text side and raise their version (ADR 0075). `pkg/plugin` grows, so every plugin binary changes (ADR 0107).
- Paperboy owns number and date formatting for text pages again, which is where the language rules live (ADR 0033).
- The value schema is public protocol and must be fixed early, above all the records for lists.
- A second editor shares the page list, the API shape (ADR 0066) and the backup rules (ADR 0073).
- A plugin author loses line layout, padding, truncation and charset work, and gains a declared value list.
- The vertical scroll composes the same page again for every step. Check the power budget (ADR 0051) and the poll interval (ADR 0045) before it ships.
- Text widgets stay as a fallback until the last plugin migrates. Two paths coexist, and the editor marks a widget row as deprecated.
- Open: the exact keys of rows and columns and the schema of a record. `docs/design/02-content.md` and `docs/plugins/protocol.md` own them.
