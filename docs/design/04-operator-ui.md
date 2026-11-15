[← Design documents](README.md) · [Paperboy](../../README.md)

# Operator UI

The operator UI runs on the same listener as the device API. It shows what the instance does, and it edits the files that describe it. The header links the pages: Dashboard, Playground, Configuration, Downloads and Stats.

## Which route needs what

The server keeps two route tables ([ADR 0096](../adr/0096-a-password-for-the-operator-ui.md)):

- `deviceRoutes` is what a display touches: the device API, the composed pages, the notice pages and the firmware. These stay open, because a device has no password.
- `uiRoutes` is everything else: the dashboard, the editors, their API, the previews. Every entry sits behind HTTP Basic auth with `ui.password`, because these pages carry the instance's content: calendars, departures, the energy of the home.

An empty password keeps the UI open and logs a warning at startup when the listener is not loopback. A test pins the prefix of the device routes, so nothing joins them by accident.

## Dashboard

`GET /ui` is the operator's first page; `/` redirects to it ([ADR 0040](../adr/0040-operator-dashboard.md)). One card per device shows:

- the last page as an image, taken from `GET /api/devices/{id}/image` ([ADR 0097](../adr/0097-page-downloads-take-the-device-key.md))
- a *Power* row with the charge state (charging, connected, discharging), the charge or the voltage
- a *Details* block with the display type, the raw battery reading, the last charge and the runtime estimate
- refresh, address and firmware
- a line for a newer Paperboy release, with the version as its link ([ADR 0101](../adr/0101-a-hint-about-a-newer-paperboy.md)), or a warning when the last release check failed

A device without a file appears as unclaimed hardware with *Claim* and *Forget* ([ADR 0080](../adr/0080-device-state-per-mac.md)). A denied device shows as denied.

Rendered pages live in memory only. After a restart the card falls back to the page preview or a placeholder.

## Self-updating pages

The dashboard, the stats page and the downloads page poll themselves ([ADR 0106](../adr/0106-self-updating-pages-with-htmx.md)). Each page wraps its changing part in a region `id="live"` and asks for itself again. htmx morphs the answer into the existing DOM, so a node with the same id keeps its focus, its selection and its place.

- The dashboard polls every 30 seconds, the stats page every 10 seconds, the downloads page every 5 seconds. A hidden tab does not poll, and it refreshes once when it becomes visible again.
- The device cards, their *Details* block and their preview carry stable ids, so an opened *Details* block survives a tick.
- A served picture names its pixel hash: `GET /api/devices/{id}/image?h=<hash>` answers `Cache-Control: private, max-age=31536000, immutable` for the current hash and `no-store` otherwise. A tick therefore downloads no image.
- The downloads poll keeps the query string of the page. The cleanup preview does not poll at all, so its list cannot change under the button next to it.
- Without JavaScript the pages stop updating. They stay readable, and every link works.
- htmx 4.0.1 is vendored in `internal/paperboy/ui/vendor/` and served as `GET /ui/htmx.js` behind the password. It is the only foreign JavaScript in the service; `ui/vendor/` is the only place for it, and a new file there needs an ADR.

## Runtime stats

`GET /ui/stats` is read-only and polls itself ([ADR 0104](../adr/0104-runtime-stats-in-the-web-ui.md), [0106](../adr/0106-self-updating-pages-with-htmx.md)). It shows Paperboy's heap, goroutines, GC, resident memory and CPU. One row per plugin carries state, program, PID, memory, CPU, requests, errors and timings. The widget cache shows entries, images, bytes and age buckets; the rendered pages and the notice pages follow. Named timings carry count, average, p50, p95 and maximum. Per device it shows the syncs with their duration, and the text polls separately. CPU shares compare with the previous view of the page, because they come from a difference of two readings.

## Downloads

`GET /ui/downloads` shows the release side of the instance ([ADR 0060](../adr/0060-download-from-releases.md), [0062](../adr/0062-plugin-updates-and-download-control.md), [0090](../adr/0090-download-cache-and-inventory.md)):

- source, interval, the last check, the running check with its button (`POST /ui/download`)
- a `Paperboy` row with the newer version or the state of the comparison ([ADR 0101](../adr/0101-a-hint-about-a-newer-paperboy.md))
- every plugin with its resolved program and version, `(pinned)` behind a version that a pin selects, and the time each program and image arrived
- the images in `firmware/`, including the old `<hardware>/` layout and the files that are ignored
- the release channel of each train, with a note when the running version is not one the channel would deliver
- the size of both inventories and *Preview cleanup*, which lists what it would delete and then deletes exactly that (`POST /ui/downloads/prune`, [ADR 0090](../adr/0090-download-cache-and-inventory.md))

## Plan preview

`GET /ui/plan?device=<id>[&at=…]` answers what a device would show, without touching it ([ADR 0054](../adr/0054-plan-preview-in-the-web-ui.md)). It lists the rules and marks the active rule or the fallback. It names the next change. Each rule shows its conditions with their live state, evaluated now even when `at` shows another time ([ADR 0103](../adr/0103-plugin-conditions-in-plans.md)). Each page shows a preview image, inline text lines, a skip reason or a page error. The view takes no state lock and never moves a cursor.

## Virtual display

`GET /ui/virtual?plan=<plan>&display=<type>[&refresh=<s>&menu=<s>&page_timeout=<s>&power=mains|battery]` plays the firmware in the browser ([ADR 0091](../adr/0091-virtual-display.md)). It shares `serveSync` with the device API, so plan selection, rotation, `page_timeout`, notices, menu and confirmation behave as on a device.

- `POST /ui/virtual/sync` takes the device sync body; `POST /ui/virtual/reset` drops the session.
- The session is a `deviceState` in memory, keyed by plan and display type. It has no device file, no MAC and no key. It has no entry under `state/devices/` and is absent from `/api/devices`.
- The URL is the only configuration; an invalid value answers 400.
- Actions resolve, select and confirm, but stop before the plugin call, with a `Simulated` banner that names the action.
- The answer adds a `debug` object with rule, page, error and menu for the line below the display.
- The images come from `/ui/virtual/pages/…` and `/ui/virtual/hints/…`, which serve what the device routes serve, because a browser has no device key ([ADR 0097](../adr/0097-page-downloads-take-the-device-key.md)).
- A text display paints one character per slot in the same LCD module as the editor board, with the vendored pixel font ([ADR 0115](../adr/0115-a-pixel-font-for-text-previews.md)). The 20 × 4 and 16 × 2 grids do not reflow.

## Playground

`GET /ui/plugins` is the page editor without a page ([ADR 0055](../adr/0055-allowed-arg-values-and-gallery-form.md), [0077](../adr/0077-the-widgets-page-has-no-gallery.md)). The link carries the widget, its span and its args. The page shows the palette and one widget rendered in its cell size from `/api/widget`. The inspector holds its size, args and style, and the snippet panel holds the cell. The display type switch keeps the widget, its span and its args. Without a widget in the link it starts with the first fitting widget of the palette. An old `?tab=gallery` link still opens the page.

`GET /api/widget/{plugin}/{widget}` renders a single widget: `display`, `span` or `width`/`height`, and `arg.<name>` per argument. The answer is a PNG or text lines, and a failure is the cell notice with an `X-Widget-Error` header.

## Configuration

`GET /ui/editor` is one overview with a sub-tab per object type: `?tab=pages` (default), `plans`, `devices`, `displays`. Each tab has one card with its count, a filter field and its new-object button in the header. The button asks for a name in a dialog and opens the editor of the new object. The pages tab also asks for the kind, and for a text page its character grid. The stale switch is *Stale values* on a text page and *Stale widgets* on an image page. A row opens its object: the name is a link and a click on the row follows it. *Rename* and *Delete* live in the editor and name the references ([ADR 0073](../adr/0073-edit-creates-and-deletes.md), [0079](../adr/0079-rename-config-items.md), [0084](../adr/0084-the-overview-links.md)).

The pages tab lists one row per page ([ADR 0110](../adr/0110-pages-serve-their-kind-and-name-variants.md)). The row shows the page file when it exists, else its first variant, with kind, grid, title and parse error. The variants column lists the other files as chips with their label. A chip tooltip names the path, the display types the page sends to it and any error. The row and the chips open exactly that file. A variant that no display type names is italic and stays a candidate in the *Variants* select. A page without a page file shows its name in red; the operator creates the file with *New page*. Its variants stay read-only until then.

**Page editor** `GET /ui/editor/page?name=<name>[&file=<label>][&display=<type>][&kind=<kind>][&new=1]` edits exactly one page file ([ADR 0068](../adr/0068-page-editor-ui.md), [0085](../adr/0085-moving-and-resizing-widgets.md), [0110](../adr/0110-pages-serve-their-kind-and-name-variants.md), [0111](../adr/0111-text-pages-from-plugin-values.md)). Without `file` it opens the page file, else the variant with that label. `display` selects the preview type only; the bar names the open file. `new=1` starts a page file. `kind` decides image or text. For a text page, `display` names the display type whose character grid the page starts with.

The page file shows one *Variants* table: a row per configured display type with its kind and a select. The select offers the files of the page that fit the type: `page file`, the fitting variants and `— none —`. It writes the page's `variants` entry ([ADR 0110](../adr/0110-pages-serve-their-kind-and-name-variants.md)). Its tooltip says why a type is not served: another kind, another character grid or a file that does not parse. The display type is a link: the open file previews it, or it opens the file that serves it. The current preview type is marked. The editor never renders another file in place of the open one. A link to a type the open file does not serve keeps a short notice. A `variants` entry whose display type is no longer configured keeps its row in the table; choosing `— none —` drops the entry and leaves the file. The select offers `— none —` wherever the page file cannot serve the type. One *Create variant…* button opens a dialog for a display type and a file label. It starts with the first type that no file serves, else with the first type. The new variant starts from the type, so its kind and character grid never come from the open file (issue #238). Every type is a candidate, and the page sends it to the new file.

A variant is a child page: it shows the title of its page file read-only and replaces the table with a *Variant* block. The block names the page as a link and lists the display types the page sends to the file, one per line, or `none`. Two buttons act on the file. *Rename…* asks for a new label; the file moves, and the page rewrites every entry that names the old label. *Reassign…* offers the other pages with a page file; the file moves, and its entries go to that page's map. A variant that no entry names moves without touching a map. A variant that no display type names shows a yellow warning. The editor opens the file at its new place after either action. The page file owns the variants, and the bar carries no page rename on a variant. A variant whose page file is missing stays read-only and offers no action. The pages list links none of its files and names the missing page file.

Every editor has the same action bar: `← <list>` · object · status · Rename · Delete · Reload · Save ([ADR 0076](../adr/0076-one-stylesheet-and-one-navigation.md)). Every page links `ui/ui.css`, shows the logo lockup in its header, links the favicon, and runs one embedded script with no build step. The logo and the favicon are copies of `docs/logo/`, and a test keeps them identical to the design files.

On a text page the right inspector shows only the selected cell. It holds `at`, `span`, a *Content* switch between value and literal text, `label`, `unit`, `align` and `overflow`. A full-width *Delete cell* button sits at the bottom. A literal text cell has a textarea and no label or unit. With no selection it shows a short hint. A text page hides the style in points and has no rows panel: boxes are placed in two dimensions directly on the board. The page's `overflow` is a field next to *Title*; the stale switch is *Stale values*. The palette lists the declared values and a *Text* entry. Click a value or *Text*, then a free slot; drag a value onto the board. Drag a box to move it and an edge or corner to resize it. A box that does not fit or would overlap is refused. Scrolling boxes stay static on the board, and its note names `scrolls, step N ms`. Without JavaScript the page renders its boxes as read-only fields and saves nothing.

The board is the rendered draft with the cell boxes on top ([ADR 0074](../adr/0074-editor-board-shows-the-preview.md)). An image is the background. Text appears one character per slot in a dark green LCD module with light green pixels and the vendored pixel font ([ADR 0115](../adr/0115-a-pixel-font-for-text-previews.md)). The cells have a one-pixel gap, and the glyph size follows the slot height. The 20 × 4 and 16 × 2 grids stay exact. A *Cells* switch, which the browser remembers, hides the boxes, and a "rendering…" hint covers the debounce. A cell comes from the palette by click or drag. It moves with the pointer, so the grabbed point stays under the pointer, or by its position. An edge or corner handle resizes it.

**Plan editor** `GET /ui/editor/plan?name=<name>[&new=1]` edits one plan file ([ADR 0069](../adr/0069-plan-editor-ui.md)). A missing file opens as a new plan and is written by the first save. The editor lists the rules in order with name, `from`/`to`, days, page order, `rotate` and `refresh`. Each page row names the display types the page serves. The page picker lists the page names. The conditions of the plugins' `describe` answers are check boxes. A condition that no plugin declares stays visible and warns ([ADR 0103](../adr/0103-plugin-conditions-in-plans.md)). Then the fallback and the actions, with the plan-local `id` rule of [ADR 0057](../adr/0057-plugin-actions-from-the-plan.md). A side panel names the devices of the plan and the pages with their skip reason per display type. It links to `/ui/plan?device=` and to the device pages.

**Device and display type editors** `GET /ui/editor/device?id=<id>[&new=1]` and `GET /ui/editor/display?name=<name>[&new=1]` edit the fields of their file next to the raw text ([ADR 0070](../adr/0070-devices-in-their-own-files.md), [0071](../adr/0071-display-types-in-files-of-their-own.md)). The device editor offers the display types, the plans, refresh, menu settings, power, the firmware hold and the battery thresholds. The display type editor shows the fields of its kind and computes the physical size from `ppi`. Both list the references that keep the old name.

## Page and plan API

The editors speak the JSON model of `page.Template` and `plan.Plan`, so the same parser validates a write and the runtime ([ADR 0066](../adr/0066-page-and-plan-api.md)).

| Route | Content |
|---|---|
| `GET /api/pages` | One entry per page file with name, label, title, kind, grid or row count, hash, error and `orphan` |
| `GET /api/pages/{name}?file=<label>` | Read that file and the page's coverage of every configured display type |
| `PUT /api/pages/{name}?file=<label>` | Write that file, validated like the loader; a variant carries no title and no variants |
| `POST /api/pages/{name}/variant?display=<type>[&file=<label>]` | `{"hash": "...", "template": …}`, writes the variant under the label and adds its entry |
| `POST /api/pages/{name}/variant/rename?from=<label>` | `{"name": "<new label>", "hash": "..."}`, moves the variant file and rewrites every entry that names the old label |
| `POST /api/pages/{name}/variant/move?from=<label>` | `{"page": "<target>", "hash": "..."}`, moves the variant file to another page and moves its entries to that page's map |
| `POST /api/pages/{name}/serves?display=<type>` | `{"hash": "...", "file": "<label>"}`, points the type at that file; an empty label removes the entry, so the page file serves it |
| `DELETE /api/pages/{name}?file=<label>` | Delete one file with a backup first |
| `POST /api/pages/{name}/rename` | `{"name": "<new>"}`, moves the page file and every variant and names every moved file |
| `GET /api/plans`, `GET /api/plans/{name}`, `PUT /api/plans/{name}`, `DELETE`, `POST /api/plans/{name}/rename` | The same for plans |
| `GET/PUT/DELETE /api/devices/{id}`, `POST /api/devices/{id}/rename` | Device files and their raw YAML |
| `GET/PUT/DELETE /api/displays/{name}`, `POST /api/displays/{name}/rename` | Display type files |
| `GET /api/profiles`, `GET /api/displays` | Display types with kind, size, ppi or charset, gray levels, hash and error |
| `GET /api/plugins` | Built-in and configured plugins with their `describe` answer, resolved program and version |
| `GET /api/devices` | Devices with the address of the last contact and the fuel gauge reading |
| `GET /api/preview/{display}/{page}` | Page preview, optional `at=YYYY-MM-DDTHH:MM` |
| `POST /api/preview/{display}` | Draft preview of a page that was not saved |

Two rules guard every write. A write carries the hash of the file it read and is refused with 409 when the file changed in between ([ADR 0066](../adr/0066-page-and-plan-api.md)). Every write and every delete keeps a backup, the last ten versions, under `state/editor/` ([ADR 0073](../adr/0073-edit-creates-and-deletes.md)). A rename moves files only and refuses a name that already exists with 409. It never rewrites a reference ([ADR 0114](../adr/0114-a-rename-does-not-ask.md)). A page rename moves the page file and every variant as one set, keeps the `variants` map and names each moved file ([ADR 0113](../adr/0113-a-page-rename-moves-its-variants.md)). A device rename moves only the file, so the state follows the MAC ([ADR 0080](../adr/0080-device-state-per-mac.md)). Before the first rename, each move is preflighted: every source must exist and be readable, every target must not exist. Each target directory must be writable, and a failed check leaves every file in place. A variant rename or move backs up every page file it rewrites and undoes the file move when a write fails.

## Decisions

[ADR 0040](../adr/0040-operator-dashboard.md) (dashboard) · [0054](../adr/0054-plan-preview-in-the-web-ui.md) (plan preview) · [0055](../adr/0055-allowed-arg-values-and-gallery-form.md) (argument values) · [0066](../adr/0066-page-and-plan-api.md) (page and plan API) · [0068](../adr/0068-page-editor-ui.md) (page editor) · [0069](../adr/0069-plan-editor-ui.md) (plan editor) · [0070](../adr/0070-devices-in-their-own-files.md), [0071](../adr/0071-display-types-in-files-of-their-own.md) (device and display type files) · [0073](../adr/0073-edit-creates-and-deletes.md) (create and delete) · [0074](../adr/0074-editor-board-shows-the-preview.md) (the board) · [0076](../adr/0076-one-stylesheet-and-one-navigation.md) (one stylesheet, one navigation) · [0077](../adr/0077-the-widgets-page-has-no-gallery.md) (no gallery) · [0079](../adr/0079-rename-config-items.md) (renaming) · [0080](../adr/0080-device-state-per-mac.md) (unclaimed hardware) · [0084](../adr/0084-the-overview-links.md) (overview links) · [0085](../adr/0085-moving-and-resizing-widgets.md) (moving and resizing) · [0091](../adr/0091-virtual-display.md) (virtual display) · [0094](../adr/0094-the-pages-list-is-a-list.md) (the pages list) · [0096](../adr/0096-a-password-for-the-operator-ui.md) (password) · [0097](../adr/0097-page-downloads-take-the-device-key.md) (images behind the UI) · [0101](../adr/0101-a-hint-about-a-newer-paperboy.md) (newer Paperboy) · [0104](../adr/0104-runtime-stats-in-the-web-ui.md) (stats) · [0106](../adr/0106-self-updating-pages-with-htmx.md) (self-updating pages) · [0110](../adr/0110-pages-serve-their-kind-and-name-variants.md) (page file and variants) · [0113](../adr/0113-a-page-rename-moves-its-variants.md) (a page rename moves its variants) · [0115](../adr/0115-a-pixel-font-for-text-previews.md) (the pixel font of text previews)
