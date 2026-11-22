# 0154: Split the release trains and add a maintenance page

- Status: Accepted
- Date: 2026-11-22

## Context

The *Downloads* page carried four things at once: the release check, the installed plugin programs, the firmware images, and the cleanup of both directories. The plugin and firmware releases became separate trains in [ADR-0147](0147-separate-server-plugin-and-firmware-releases.md), with a channel of their own each, but nothing in the operator UI said so.

The cleanup sat among them, one *Preview cleanup* for `plugins/` and `firmware/`. Paperboy deletes more than those files: `state/devices/<MAC>.json` stays when a device file goes, and `state/editor/` keeps up to ten copies of every object, including objects that are deleted. Neither had a place in the UI, so both grew until an operator removed the files by hand.

## Decision

We give each release train a page and the cleanup a page of its own.

- `GET /ui/system/plugins` shows the plugins, `GET /ui/system/firmware` the images. Both show the whole check, because one check installs both trains; a page does not pretend to own a check of its own.
- `GET /ui/system/maintenance` is the only page that deletes files. It cleans three things, each behind a preview: the inventory of `plugins/` and `firmware/` ([ADR-0090](0090-download-cache-and-inventory.md)), the state of a MAC without a device file that was not seen for 30 days ([ADR-0080](0080-device-state-per-mac.md)), and the editor backups of objects that are gone ([ADR-0066](0066-page-and-plan-api.md)).
- Recent unclaimed state stays. A device that registers waits for a claim, and its cursor and battery level must survive that wait.
- The maintenance page does not poll. A preview list cannot change under the button that deletes it.
- `GET /ui/system/downloads` and its prune route go away without a redirect.

## Consequences

An operator sees one train per page and cleans up in one place. The rail gains four entries under *System*: Playground, Plugins, Firmware and Maintenance.

The split separates the check from the install only in the UI. One check still downloads and installs both trains in one run; a two-phase flow that checks first and installs later needs its own ADR.

The cleanup reads the inventories and `state/editor/` on every visit. That is cheap, because it lists files and does not parse them, but the page grows with the number of old versions an instance keeps.
