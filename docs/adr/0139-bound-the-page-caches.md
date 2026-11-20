# 0139: Bound the in-memory page caches

- Status: Accepted
- Datum: 2026-11-20

## Kontext

Paperboy keeps two page caches in memory. `rendered` holds the last render per
profile and page, and `hints` holds every notice page, battery warning and menu
screen. Neither had a limit. `rendered` also kept the key of a page that the
operator deleted, and `hints` grew with every distinct notice. The note map that
suppresses repeated refusal logs in the journal grew with every path.

## Entscheidung

The rendered cache holds at most 256 entries. An entry older than 24 hours is
dropped when the next page is stored, and above the cap the oldest goes. The
hash index follows the dropped entry.

The notice cache holds at most 500 entries and drops the least recently used.
A download, a menu overlay and a page check count as use.

The log notes for refused page downloads hold at most 256 paths. An entry older
than an hour is dropped first; above the cap Paperboy keeps the new note to
itself instead of growing the map.

Both caches are memory only. A dropped entry is rebuilt on the next request.

## Konsequenzen

Memory stays bounded on a long-running instance with many pages and notices. A
device that still names a dropped notice in its URL gets `404` and keeps its
screen until the next sync, as after a restart. The stats page shows the cache
size and the oldest entry, so the operator sees the cap at work.
