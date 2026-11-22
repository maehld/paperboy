# 0153: An overview page and a grouped rail

- Status: Accepted
- Date: 2026-11-22

## Context

[ADR-0152](0152-a-design-system-and-a-rail-for-the-operator-ui.md) gave the operator UI one shell with a rail of five entries: Devices, Pages, Plans, Plugins and System. The first entry, *Devices*, opened the dashboard, which shows one card per device and nothing else.

The device list and the display type list are pages of their own, but nothing in the rail names them. Only a sub-navigation on those two lists reached them, and that sub-navigation lived on pages the operator could not find. The search on `⌘K` was the only way in.

The rail also mixed two kinds of entry. Pages, Plans and Plugins are lists the operator edits; Downloads, Stats and the Styleguide are tools around the service. *System* named the second group, the first had no name.

## Decision

We make `/ui` the overview and group the rail.

- The first entry is *Overview*, the dashboard under a new name. It heads the device cards with *Devices*. Below them, *Everything else* holds one card per list that carries the instance: Pages, Plans and Display types, each with its count and a link.
- A device card names the device as a link to its device file.
- The sub-navigation of the device section goes away. The rail names the objects, so no list hides behind another.
- The rail groups its entries: Overview, Pages and Plans stand without a head, *Configuration* holds Devices, Display types and Plugins, and *System* holds Playground, Downloads, Stats and the Styleguide.
- The Playground loses its name *Plugins*. It is no list of plugins, and the page already calls itself Playground. Its route becomes `GET /ui/playground`; the old route goes away without a redirect.
- The overview has no breadcrumb. It is the root of every path.

## Consequences

Every list is one click away from every page, and the rail says which entries are content and which are tools. The overview answers what the instance holds without opening four lists.

The counts come from where the data already lives. Pages and plans are files that no configuration snapshot holds, so a directory listing counts them without reading them; a parse per poll would cost the whole pages and plans directory every 30 seconds. Display types and devices come from the configuration. A list that does not read stays out of the overview instead of showing a wrong count.

The name *Dashboard* leaves the operator pages and the documents. Code identifiers keep the name `dashboard` where the overview renders devices; renaming them would not help the operator.

A new list needs an entry in the rail, a card on the overview and a section name in `navFor`.
