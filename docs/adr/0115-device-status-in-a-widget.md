# 0115: Device status in a widget

- Status: Accepted; point 5 superseded by [ADR-0121](0121-segmented-battery-gauge.md)
- Date: 2026-11-15
- Related: ADR 0020 (battery warning), ADR 0039 (device status), ADR 0049 (plugins), ADR 0051 (power source), ADR 0080 (state per MAC), ADR 0107 (plugin builds), issue #210

## Context

The dashboard shows the last contact, the battery reading and the estimated runtime of every device (ADR 0039, 0080). A display in the hallway should show the same values for the other devices in the house: is the display in the kitchen still alive, and does its battery last?

A page is a grid of plugin widgets (ADR 0049), and a plugin receives the page, the cell, its arguments, the language and the timezone. It does not receive any device state. A plugin must not read the instance files, the device state included. Paperboy therefore has to put the values into the request. `plugin.Request` already carries a `device` object for actions, but that object has only the id, name and hardware of the action's device.

The built-in plugin is the right owner of such a widget: it runs in the Paperboy process, needs no data source and is never cached. An external plugin would cache the rows until its `max_age` runs out and would show an old battery reading.

## Decision

1. **The request gets a separate `server_status` object.** It is part of `plugin.Request` for every plugin, but Paperboy fills it only for the built-in plugin for now. An external plugin receives `null`, because its cached widgets must not show stale rows. The object carries no key, no MAC and no address.
2. **`server_status` has two parts.** `current_device_status` is the device the page is rendered for, or `null` in a preview without a device. `selected_devices_status` is a map from id to status with only the devices the page names in its widget arguments; it is not a full inventory of the instance. Unknown ids appear with status `unknown`.
3. **A status has what the dashboard knows.** Id, display type, plan, hardware, firmware, `ok`/`late`/`never`/`unregistered`, last contact, seconds since it, power source, signal, battery (volts, percent, capacity, current, charging, warning level), the estimated runtime and its data span, the last page and the last error.
4. **Paperboy takes the snapshot outside the state lock.** It locks only while it reads the state, then composes and draws outside it, as ADR 0049 requires.
5. **The built-in plugin gets two widgets and a header switch.** `paperboy/battery` draws a battery icon with an optional percentage or voltage. `paperboy/device-status` shows the current device by default, or the devices in `devices`. Each row shows battery, estimated runtime and the last contact; the rows are sorted by staleness and a `!` marks an overdue device. Text displays get one row per device. `paperboy/header` gains `battery: true` for an icon and percentage at the right.
6. **A page that shows its own device is cached per device.** The render cache is keyed by display type and page, which would let two devices of the same type overwrite each other's status. Such a page gets the device id in its cache key, and rendered pages are served by their content hash. Pages that name only other devices stay shared.
7. **The protocol stays at version 6.** The new field is optional and additive (ADR 0055, 0081). The change to `pkg/plugin` raises every plugin version, because every binary contains the package (ADR 0107).

## Alternatives

- **Let the built-in plugin read `state/devices/`:** no protocol change, but it breaks the rule that a plugin does not read the instance's files. A later external plugin would need the same hole.
- **Send all configured devices to every plugin:** simpler for a widget that lists everything, but every plugin process would see the whole installation's state, and its cached widgets would go stale. The filtered map keeps the data small and the cache safe.
- **A separate `status` plugin:** it would need a data API or the state files, and Paperboy has no data API by design.
- **Only the current device:** no selected-device mode. The issue names a hallway display for the other devices, so the filtered map stays.

## Consequences

- A page with `device-status` and no `devices` renders once per device; the memory cost is one page per device that shows it.
- An external plugin cannot show these values until a later ADR defines a cache rule for device-aware widgets.
- The runtime is only as good as the battery history; a new device shows no estimate until its series has a measurable drop.
- Every plugin gets a new version because `pkg/plugin` changed, although only the built-in one uses the field.
