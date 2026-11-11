# rss plugin

RSS 2.0, RSS 1.0 (RDF) and Atom feeds as widgets. Every configured feed is fetched in the background, so the page never waits for a slow server. No API key, no account.

## Configuration

| Key | Description |
|---|---|
| `feeds` | Required: a list of `{name, url}` entries. The name is what the widgets select; a plain URL uses its host as the name |
| `refresh` | How often every feed is fetched, default `15m`, at least `5m` |

```yaml
plugins:
  rss:
    command: [rss]
    config:
      refresh: 15m
      feeds:
        - { name: Tagesschau, url: "https://www.tagesschau.de/xml/rss2/" }
        - { name: heise, url: "https://www.heise.de/rss/heise-atom.xml" }
        - "https://netzpolitik.org/feed/"          # name: netzpolitik.org
```

Every feed is requested with a conditional GET (`ETag`, `If-Modified-Since`), a 20 s timeout and a 2 MB cap. A feed that fails keeps its last entries and does not affect the others; a feed that never loaded shows its error in the cell. One feed keeps at most 60 entries in memory.

Titles and summaries are plain text: HTML is stripped and entities decoded, and a summary is cut at 400 characters. Entries are sorted by date (newest first), and duplicates are dropped by GUID or link. Entries without a date keep the feed's order and come last.

## Widget `feed`

A list of one feed, or of all feeds mixed.

| Arg | Description |
|---|---|
| `feed` | A configured name, or `all` (default) for the newest entries of every feed |
| `items` | Most entries, `0` (default) fills the cell |
| `summary` | One line of the entry's summary beneath its title |
| `header` | The feed's name above the list |
| `source` | The feed's name on every entry; on by default for `all` when more than one feed is configured, `source: false` hides it |
| `time` | `relative` (default), `clock` or `none` |

All titles of a cell share one size: the largest that fits every visible title down to 0.7 of the page's base text; longer ones end with an ellipsis. The list fills the cell, so a taller cell shows more entries (or the same entries with their summaries).

The text variant puts one entry per line, the compact age (`12m`, `5h`, `2d`, then a date) in front and the title behind it.

## Widget `headline`

The newest entry of one feed, large: the feed's name and the age on top, the title over up to four lines, the summary beneath.

| Arg | Description |
|---|---|
| `feed` | A configured name, or `all` (default) for the newest entry of every feed |
| `summary` | The entry's summary, default `true` |
| `time` | `relative` (default), `clock` or `none` |

The text variant shows the source and age in the first line, then the wrapped title and, when there is room, one line of the summary.

## Times

`relative` says how old an entry is: `vor 12 Min`, `vor 5 Std`, `gestern`, the weekday within the last week, otherwise the date. The text follows the global `language`, and all times use the configured `timezone`. `clock` shows a time of day, `none` hides it.

See `examples/pages/nachrichten.yaml` for a page with two feeds and a mixed list.
