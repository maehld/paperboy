# 0123: A sample device in previews

- Status: Accepted
- Date: 2026-11-16
- Related: ADR 0040 (dashboard), ADR 0054 (plan preview), ADR 0055 (argument values), ADR 0096 (UI password), ADR 0115 (device status in a widget), issue #274

## Context

`paperboy/battery`, `paperboy/device-status` and the header's `battery` switch read `server_status` from the plugin request ([ADR 0115](0115-device-status-in-a-widget.md)). They need the device the page is rendered for. A preview has no device, so the battery widget draws an empty outline and `-`, and `device-status` prints `no device`. The editor draft preview, the page preview and the widget preview all pass an empty device.

Two previews know their device and drop it. The dashboard card falls back to the page preview after a restart. The plan preview draws the pages of one device.

The operator must also type the device ids by hand. `battery.device` is a text field and `device-status.devices` is a comma-separated text. A typo becomes an `unknown` row on the display. ADR 0115 keeps that row on purpose, so a typo stays visible.

## Decision

1. **A preview without a device shows a sample device.** Paperboy fills `current_device_status` with a fixed status. It names the id `preview`, status `ok`, `seen_s` 0, the display type of the preview and power `battery`. It shows 78 %, 4.05 V, not charging and a runtime of about 12 days. It is deterministic, reads no state file and does not write the render cache, because previews are `no-store`. The row reads `preview 78 % ~12 d now`, so the operator sees that these are sample values.
2. **Only the unnamed device is faked.** A page that names an unknown id keeps its `unknown` row. A preview that names a device shows that device.
3. **The page preview takes an optional `device=<id>`.** `GET /api/preview/{display}/{page}` draws the page for that device, so the status widgets show its real values. The dashboard fallback and the plan preview pass it. `POST /api/preview/{display}` and `GET /api/widget/{plugin}/{widget}` keep the sample device until an editor knows a device.
4. **`GET /api/plugins` offers the configured device ids.** Paperboy fills the `enum` of `paperboy/battery.device` and `paperboy/device-status.devices` with the configured ids. ADR 0055 allows a computed enum. Paperboy labels the empty value in the configured language: *the device that shows the page*. The built-in `Describe` stays enum-free, so a page that names a removed device still renders. The `unknown` row stays reachable. The editor only guides.
5. **The editor keeps a stored id that is no longer configured.** The select and the check boxes show it with a *not configured* mark. The operator sees it and can remove it.

## Alternatives

- **A device switch in the Playground:** it would show real values, but the Playground is the page editor without a page. It previews one widget per display type. The sample covers the draft and the widget preview too.
- **A `device` and a `devices` argument type in the protocol:** the types would be reusable. Every plugin would get a new version for a feature that only the built-in plugin uses today.
- **An id that `validName` rejects, such as `(preview)`:** it would rule out a collision with a device file. It reads oddly in the `device-status` row.
- **All configured devices in the request:** a preview could name any of them, but every preview would carry the whole installation's status. The page's arguments still select the ids it needs.

## Consequences

- A device file may use the id `preview`. Its previews then show the sample values, and only a preview that names the device shows its own.
- The sample values are fake. The id in the row says so, and no real device reports them.
- The computed enum is a hint. A page with an unknown id still renders, as ADR 0115 requires.
- The editor labels now follow `language`; an English instance shows an English empty choice.
