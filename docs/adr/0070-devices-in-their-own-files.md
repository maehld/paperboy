# 0070: Devices in files of their own

- Status: Accepted, implemented
- Date: 2026-11-08
- Builds on: ADR 0037 (instance directory), ADR 0047 (configuration reload), ADR 0066 (page and plan API), ADR 0068 (page editor), ADR 0069 (plan editor)

## Context

The devices are a section of `paperboy.yaml`: one map from a stable ID to
display type, MAC address, plan, refresh and menu settings (ADR 0026). That was
fine while the file was edited by hand only. The editors of ADR 0068 and 0069
work through an API that reads and writes one file per page or plan and never
rewrites the hand-edited main configuration (comments, `${VAR}` placeholders).

Two things follow from that. Assigning a plan to a device (issue #92) is a
two-line YAML edit that nothing in the browser can do, and creating a device
means editing the same large file. Both are the first steps towards the
"everything dynamic" direction: what the UI manages should live in files the UI
can write.

## Decision

- Devices move to `devices/<id>.yaml` in the instance directory, one file per
  device (ADR 0037 layout). The file name is the stable device ID (a-z, 0-9, `-`);
  the file holds the keys the map entry held before, without the map level:
  `display_type`, `protocol`, `mac`, `plan`, `refresh`, `power`, `actions`,
  `menu_show_duration`, `battery` and `firmware`.
- The files are parsed as strictly as pages and plans: an unknown key, a second
  document, an invalid ID or a broken value aborts the load and names the file.
  The checks of a device (MAC, display type, plan name, durations, actions) stay
  the same and live in one place (`Config.validateDevice`), which the API uses
  too.
- `paperboy.yaml` keeps only the global settings (`language`, `timezone`,
  `plugins`, `download`, `defaults`, `firmware`, `display_types`, `battery`
  defaults). A leftover `devices:` section aborts the start with a hint that
  points at `devices/`, like the v1 sections `server` and `providers`.
- A missing `devices/` directory is an instance without devices, not an error, so
  a fresh instance and the examples still start.
- Reload (ADR 0047): the configuration loader watches `paperboy.yaml` and the
  device files. It compares a stamp of the directory (name, size and mod time of
  every `*.yaml`) on each access, so adding, changing or removing a device
  applies without a restart; a broken device file keeps the last valid
  configuration and logs once.
- API in the shape of the page and plan API (ADR 0066): `GET /api/devices/{id}`
  answers the model, the raw YAML, the file hash and a parse error, a broken
  file included. `PUT /api/devices/{id}` takes the model and the hash that was
  read, validates it like the loader (display type must exist, MAC unique, plan
  name, durations), answers `409` when the file changed since it was read,
  creates a missing file, and keeps the old version under
  `state/editor/devices/<id>/`. The dashboard list `GET /api/devices` stays what
  it was: runtime state (ADR 0039).
- The plan stays on the device, so assigning one is a single small write to
  `devices/<id>.yaml` and never touches `paperboy.yaml`. Several devices may
  still share a plan; content never references devices.
- Editor: `/ui/editor` lists the device files next to pages and plans and offers
  a new device; `/ui/editor/device?id=<id>[&new=1]` edits one with display type,
  MAC, plan (chosen from the plan files), `refresh`, `actions`,
  `menu_show_duration`, `power`, `firmware` and the battery thresholds, plus a
  link to the plan preview. The plan editor links its devices here. Save and
  conflict handling work like the page and plan editors.
- `state/devices.json` stays runtime state (API keys, page cursor, battery,
  menu) and is still keyed by the device ID.
- Migration is by hand, like the v1 to v2 migration: split the `devices:` section
  into one file per device and remove the section (README). There is no migration
  code.
- Out of scope: `display_types` in files of their own (a later step; devices
  reference them), automatic migration, authentication of the editor.

## Consequences

- The editor can create devices and assign plans; the device list is still
  configuration and the authority, and a plan is assigned where it belongs.
- A change to one device rewrites one small file and cannot lose the comments of
  `paperboy.yaml`.
- A new device needs two files in the instance directory: the device file and
  what the plan and pages provide. A start with a broken device file reports its
  path instead of starting with a different device list.
- Removing a device file keeps its entry in `state/devices.json`, like removing a
  device from the configuration did before.
- Everything under `/ui` and the API stays unauthenticated (ADR 0040, 0066,
  0068): the device editor is as open as the dashboard and belongs in a trusted
  network.
