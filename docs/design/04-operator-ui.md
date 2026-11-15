[← Design documents](README.md) · [Paperboy](../../README.md)

# Operator UI

The operator UI runs on the same listener as the device API. It shows what the instance does, and it edits the files that describe it.

## Which route needs what

The server keeps two route tables (ADR 0096):

- `deviceRoutes` is what a display touches: the device API, the composed pages, the notice pages and the firmware. These stay open, because a device has no password.
- `uiRoutes` is everything else: the dashboard, the editors, their API, the previews. Every entry sits behind HTTP Basic auth with `ui.password`, because these pages carry the instance's content: calendars, departures, the energy of the home.

An empty password keeps the UI open and logs a warning at startup when the listener is not loopback. A test pins the prefix of the device routes, so nothing joins them by accident.

## Dashboard

`GET /ui` is the operator's first page; `/` redirects to it (ADR 0040). One card per device shows:

- the last page as an image, taken from `GET /api/devices/{id}/image` (ADR 0097)
- a *Power* row with the charge state (charging, connected, discharging), the charge or the voltage
- a *Details* block with the display type, the raw battery reading, the last charge and the runtime estimate
- refresh, address and firmware
- a line for a newer Paperboy release, with the version as its link (ADR 0101), or a warning when the last release check failed

A device without a file appears as unclaimed hardware with *Claim* and *Forget* (ADR 0080). A denied device shows as denied.

Rendered pages live in memory only. After a restart the card falls back to the page preview or a placeholder.

## Runtime stats

`GET /ui/stats` is read-only (ADR 0104). It shows Paperboy's heap, goroutines, GC, resident memory and CPU. One row per plugin carries state, program, PID, memory, CPU, requests, errors and timings. The widget cache shows entries, images, bytes and age buckets; the rendered pages and the notice pages follow. Named timings carry count, average, p50, p95 and maximum. Per device it shows the syncs with their duration, and the text polls separately. CPU shares compare with the previous view of the page, because they come from a difference of two readings.

## Downloads

`GET /ui/downloads` shows the release side of the instance (ADR 0060, 0062, 0090):

- source, interval, the last check, the running check with its button (`POST /ui/download`)
- a `Paperboy` row with the newer version or the state of the comparison (ADR 0101)
- every plugin with its resolved program and version, `(pinned)` behind a version that a pin selects, and the time each program and image arrived
- the images in `firmware/`, including the old `<hardware>/` layout and the files that are ignored
- the release channel of each train, with a note when the running version is not one the channel would deliver
- the size of both inventories and *Preview cleanup*, which lists what it would delete and then deletes exactly that (`POST /ui/downloads/prune`, ADR 0090)

## Plan preview

`GET /ui/plan?device=<id>[&at=…]` answers what a device would show, without touching it (ADR 0054). It lists the rules and marks the active rule or the fallback. It names the next change. Each rule shows its conditions with their live state, evaluated now even when `at` shows another time (ADR 0103). Each page shows a preview image, inline text lines, a skip reason or a page error. The view takes no state lock and never moves a cursor.

## Virtual display

`GET /ui/virtual?plan=<plan>&display=<type>[&refresh=<s>&menu=<s>&page_timeout=<s>&power=mains|battery]` plays the firmware in the browser (ADR 0091). It shares `serveSync` with the device API, so plan selection, rotation, `page_timeout`, notices, menu and confirmation behave as on a device.

- `POST /ui/virtual/sync` takes the device sync body; `POST /ui/virtual/reset` drops the session.
- The session is a `deviceState` in memory, keyed by plan and display type. It has no device file, no MAC and no key. It has no entry under `state/devices/` and is absent from `/api/devices`.
- The URL is the only configuration; an invalid value answers 400.
- Actions resolve, select and confirm, but stop before the plugin call, with a `Simulated` banner that names the action.
- The answer adds a `debug` object with rule, page, error and menu for the line below the display.
- The images come from `/ui/virtual/pages/…` and `/ui/virtual/hints/…`, which serve what the device routes serve, because a browser has no device key (ADR 0097).

## Widgets page

`GET /ui/plugins` is the page editor without a page (ADR 0055, 0077). The link carries the widget, its span and its args. The page shows the palette and one widget rendered in its cell size from `/api/widget`. The inspector holds its size, args and style, and the snippet panel holds the cell. The display type switch keeps the widget, its span and its args. Without a widget in the link it starts with the first fitting widget of the palette. An old `?tab=gallery` link still opens the page.

`GET /api/widget/{plugin}/{widget}` renders a single widget: `display`, `span` or `width`/`height`, and `arg.<name>` per argument. The answer is a PNG or text lines, and a failure is the cell notice with an `X-Widget-Error` header.

## Editors

`GET /ui/editor` is one overview with a sub-tab per object type: `?tab=pages` (default), `plans`, `devices`, `displays`. Each tab has one card with its count, a filter field and its new-object button in the header. The button asks for a name in a dialog and opens the editor of the new object. The pages tab also asks for the display type. A row opens its object: the name is a link and a click on the row follows it. *Rename* and *Delete* live in the editor and name the references (ADR 0073, 0079, 0084).

The pages tab lists every page once without a display type switch (ADR 0094). A row shows the shared page when it exists, else the first variant, with kind, grid, title and parse error. The variants column names the files as `shared · small` and puts the path in the tooltip. A broken file is red, and a file that no editor can open is removed on disk.

Every editor has the same action bar: `← <list>` · object · status · Rename · Delete · Reload · Save (ADR 0076). Every page links `ui/ui.css`, shows the logo lockup in its header, links the favicon, and runs one embedded script with no build step. The logo and the favicon are copies of `docs/logo/`, and a test keeps them identical to the design files.

**Page editor** `GET /ui/editor/page?name=<name>&display=<type>[&new=1][&shared=1][&file=…][&source=…]` edits exactly one page file (ADR 0068, 0085, 0094). It names the open file behind the page name (`page <name> - shared`, `page <name> - <type>`). It offers `Edit <type> variant` or `Edit shared` to switch between the two files of the display type. `new=1` starts exactly the file the editor would write. `file=` opens a variant of a display type that is no longer configured, through a configured type of the same kind. The editor warns and keeps the page unsaved until `Save as <type> variant` writes it under the open type (ADR 0079).

The board is the rendered draft with the cell boxes on top (ADR 0074). An image is the background, and text appears one character per slot. A *Cells* switch, which the browser remembers, hides the boxes, and a "rendering…" hint covers the debounce. A cell comes from the palette by click or drag. It moves with the pointer, so the grabbed point stays under the pointer, or by its position. An edge or corner handle resizes it.

**Plan editor** `GET /ui/editor/plan?name=<name>[&new=1]` edits one plan file (ADR 0069). A missing file opens as a new plan and is written by the first save. The editor lists the rules in order with name, `from`/`to`, days, page order, `rotate` and `refresh`. The conditions of the plugins' `describe` answers are check boxes. A condition that no plugin declares stays visible and warns (ADR 0103). Then the fallback and the actions, with the plan-local `id` rule of ADR 0057. A side panel names the devices of the plan and the pages with their skip reason per display type. It links to `/ui/plan?device=` and to the device pages.

**Device and display type editors** `GET /ui/editor/device?id=<id>[&new=1]` and `GET /ui/editor/display?name=<name>[&new=1]` edit the fields of their file next to the raw text (ADR 0070, 0071). The device editor offers the display types, the plans, refresh, menu settings, power, the firmware hold and the battery thresholds. The display type editor shows the fields of its kind and computes the physical size from `ppi`. Both list the references that keep the old name after a rename.

## Page and plan API

The editors speak the JSON model of `page.Template` and `plan.Plan`, so the same parser validates a write and the runtime (ADR 0066).

| Route | Content |
|---|---|
| `GET /api/pages`, `GET /api/pages/{name}?display=` | List or read one page file |
| `PUT /api/pages/{name}?display=` | Write, validated like the loader |
| `DELETE /api/pages/{name}` | Delete with a backup first |
| `POST /api/pages/{name}/rename` | `{"name": "<new>"}`, moves the file only |
| `GET /api/plans`, `GET /api/plans/{name}`, `PUT /api/plans/{name}`, `DELETE`, `POST /api/plans/{name}/rename` | The same for plans |
| `GET/PUT/DELETE /api/devices/{id}`, `POST /api/devices/{id}/rename` | Device files and their raw YAML |
| `GET/PUT/DELETE /api/displays/{name}`, `POST /api/displays/{name}/rename` | Display type files |
| `GET /api/profiles`, `GET /api/displays` | Display types with kind, size, ppi or charset, gray levels, hash and error |
| `GET /api/plugins` | Built-in and configured plugins with their `describe` answer, resolved program and version |
| `GET /api/devices` | Devices with the address of the last contact and the fuel gauge reading |
| `GET /api/preview/{display}/{page}` | Page preview, optional `at=YYYY-MM-DDTHH:MM` |
| `POST /api/preview/{display}` | Draft preview of a page that was not saved |

Two rules guard every write. A write carries the hash of the file it read and is refused with 409 when the file changed in between (ADR 0066). Every write and every delete keeps a backup, the last ten versions, under `state/editor/` (ADR 0073). A rename moves files only and refuses a name that already exists with 409. It never rewrites a reference, and it names the ones that keep the old name (ADR 0079). A page rename moves its shared file and every variant as one set, with a `set_hash` and a rollback on failure. A device rename moves only the file, so the state follows the MAC (ADR 0080).

## Decisions

ADR 0040 (dashboard) · 0054 (plan preview) · 0055 (argument values) · 0066 (page and plan API) · 0068 (page editor) · 0069 (plan editor) · 0070, 0071 (device and display type files) · 0073 (create and delete) · 0074 (the board) · 0076 (one stylesheet, one navigation) · 0077 (no gallery) · 0079 (renaming) · 0080 (unclaimed hardware) · 0084 (overview links) · 0085 (moving and resizing) · 0091 (virtual display) · 0094 (the pages list) · 0096 (password) · 0097 (images behind the UI) · 0101 (newer Paperboy) · 0104 (stats)
