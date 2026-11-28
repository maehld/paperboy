# 0181: A World of Warcraft character from two APIs

- Status: Accepted, implemented
- Date: 2026-11-28
- Builds on: ADR 0049 (plugin widgets), 0126 (schedule event), 0151 (design language)
- Answers: issue #505

## Context

An operator wants a World of Warcraft character on the display: the render,
level, specialization, item level, the Mythic+ score, the raid progress and the
equipped items.

Blizzard's profile API is the official source. It answers the character
profile, the render pictures and every equipped item with its rarity, its item
level and the content it comes from. It takes an application token from the
client-credentials flow; the operator creates one pair of client id and secret
at `develop.battle.net`. It has no Mythic+ score and no raid progress.

Raider.IO answers the score, the best runs and the raid progress per
difficulty, with the tier's name. It needs no key and allows a rate of 300
requests per minute. Its data is only as fresh as its last scan of the
character, and it has no render picture and no per-item rarity.

## Decision

The plugin `armory` reads both sources and merges them. Blizzard answers the
profile, the render and the equipment; Raider.IO answers the score, the best
run and the raid progress. Either source may fail without losing the other.

Without credentials the plugin runs on Raider.IO alone. The `character` and
`mythic` and `raid` widgets then work, and the `character` widget draws its
text without a render. The `gear` widget draws a notice, because Raider.IO has
no rarity and no item origin.

The widgets are `character`, `mythic`, `raid` and `gear`; the values are
`name`, `level`, `class`, `spec`, `guild`, `item-level`, `achievements`,
`mythic-score`, `raid-progress` and `last-login`. A character is a named entry
in the plugin's configuration (`Name@realm`), and a widget or a value names it
with the argument `character`. This lets one page show several characters.

The equipment shows the slot, the rarity and the origin of each item, beside
its name and item level. The upgrade step ("Myth 4/6") stays out: it is not a
field of the API. It only exists inside the bonus-list ids of the item, which
Wowhead and SimulationCraft decode with a table that changes with every patch.

## Consequences

The plugin keeps no state: one fetch rebuilds everything, and the data of a
character changes when that character logs out. It caches for `refresh`
(30 minutes by default) and asks Raider.IO on every refresh, so the operator's
data source sees a steady but small load.

The render picture is downloaded on each refresh and dithered onto the gray
levels of the display. On a two-level panel it stays readable, on sixteen
levels it looks like a photograph. The picture belongs to Blizzard
Entertainment; the plugin fetches it at runtime and ships nothing.

The plugin needs Paperboy protocol 10 for its widgets and values; it uses the
page's text scale and the shared hero component of `sdk/design`.

Attribution: World of Warcraft and the character renders belong to Blizzard
Entertainment. Raider.IO asks for a link back from integrations. The plugin
document names both sources with their terms.
