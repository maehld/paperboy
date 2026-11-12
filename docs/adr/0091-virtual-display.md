# 0091: A virtual display in the web UI

- Status: Accepted, implemented
- Date: 2026-11-12
- Builds on: ADR 0042 (device API), 0049 (pages from plugin widgets), 0054 (plan preview), 0057/0067/0082 (device menu), 0064 (one-shot wake), 0078 (menu deadline), 0080 (device state per MAC)
- Answers: issue #169

## Context

The web UI can show a plan read-only (`/ui/plan?device=`, ADR 0054) and single pages (`/api/preview/{display}/{page}`), but nothing behaves like a display: no paging, no rotation, no menu, no actions, no notices, no `refresh_s` and no `wake`. Debugging a plan therefore means waiting for a device or checking pages one by one, and showing a plan to somebody without hardware is not possible.

Everything a display *is* already lies in the device API: `POST /api/device/sync` answers the image or the text lines, the refresh, the listen window and the one-shot wake, and the server keeps the cursor, the menu and the action phases. A display is that contract plus a client that plays the firmware loop.

Three shapes were considered:

- **The browser as the firmware of a real device.** A device file with a synthetic MAC, `register` for the key, then real syncs. The most faithful, and no server code. But the virtual display *is* a device: it appears on the dashboard, writes `state/devices/<MAC>.json`, and shares its cursor and menu with the panel. Pointing it at a real MAC would also hand out that device's key, because `register` trusts whoever knows the MAC.
- **A second sync handler for virtual displays.** Cheap at first, but it would drift from the device path and show green where a real panel fails, which is exactly what a debug tool must not do.
- **One sync path, two sources of a device.** The shared path plus an in-memory session is the only shape that is both faithful and does not touch a device.

## Decision

### 1. The URL is the configuration

`GET /ui/virtual?plan=<plan>&display=<type>` renders a plan as a display of that type. Both parameters are required: the plan supplies the rules, the page order, the rotation and the actions; the display type supplies pixels, ppi, grid, kind and the refresh floor. A missing parameter answers 400 with a hint, an unknown display type 404. There are no controls on the page; the URL is the only place a setting lives, so it can be shared and bookmarked.

Optional parameters replace what a device file would decide: `refresh` (the device refresh, which a plan rule may override), `menu` (5–60 s), `page_timeout` and `power=mains|battery`, all in seconds except `power`. Out-of-range values answer 400 with the expected range, like the configuration does. The defaults are the instance defaults; `power` defaults to mains, because the browser never sleeps and a battery floor of one minute would only hide the plan's refresh.

### 2. A session, not a device

The state of a virtual display is a `deviceState` in memory, keyed by plan and display type. It has no device file, no MAC, no API key, no entry under `state/devices/`, no row in the device editor's unclaimed hardware and no card on the dashboard; nothing is written. `POST /ui/virtual/reset` drops the cursor and the menu. A change to `refresh`, `menu`, `page_timeout` or `power` applies to the running session from the next sync on; another plan or display type is another session.

Two browsers with the same URL therefore operate the same display, like two hands on one panel. That is intended: the URL describes the display, and a shared link shows the same page.

### 3. The browser plays the firmware

The page syncs on load, loads the returned image (or shows the text lines), reports the hash only once the panel has it, and comes back after `refresh_s` or for the one-shot `wake` with the reason the answer names (`menu`, `result`). Input becomes `events` with monotonically increasing ids, exactly like the firmware's RTC queue; input during a sync becomes one follow-up sync after 150 ms. `listen_s` is meaningless in a browser and always 0. The buttons are `←`, `→` and Enter with press-and-hold for a long press; the menu is drawn without its tap strip, because the virtual display has no digitiser.

### 4. One sync path

`handleSync` keeps the bearer-key lookup and the device problem; `serveSync` applies the wake. The virtual display calls the same function with `syncOptions{virtual: true}`, which skips the firmware offer, the battery input and the listen window and writes no state file. The dry-run lives at the one point where a device invokes an action: the menu resolves its actions, opens, selects and confirms exactly as on a device, then stops before the plugin call and shows a result banner with the title "Simulated" and the action's name. That guards real side effects (evcc charging mode, Škoda climate) until a real action trigger exists; a fast action looks the same on a device, because its sync returns the result.

The virtual sync answer carries the device fields plus a `debug` object with the rule, the page, the last error and the open menu, which the page shows below the display. Those fields are not added to the device answer: a device reports them in its log and on the dashboard.

### 5. Reached from where the plan is

The plan view and the device cards link to the virtual display with the device's plan and display type, so nobody types names. The link carries no other parameter; the defaults apply.

## Consequences

- A plan, its pages, its notices and its menu can be debugged in a browser without a device, and a plan can be shown without hardware.
- The virtual display is not the device: it cannot reproduce anything that depends on a real cursor, a MAC, a battery reading, a firmware version or the digitiser. Those stay on the device's own state and its API.
- Actions are visible but never run. Plugins see the device id `virtual` and no hardware, which no shipped action uses; a future per-device action would need a real device.
- Sessions are lost on restart and live in memory; there is no eviction beyond the number of plan and display type combinations.
- The shared sync path is the risk of the change: every branch that the virtual display skips is a branch a device still uses, so the existing device tests stay the guard.
- The page is another operator page without a token; it fits the security model of the UI (keep Paperboy in a trusted network).
