[← Design documents](README.md) · [Paperboy](../../README.md)

# Operator UI

The operator UI runs on the same listener as the device API. It shows what the instance does, and it edits the files that describe it.

## The shell

One layout renders the head, the top bar, the rail, the title line and the theme toggle for every page ([ADR 0152](../adr/0152-a-design-system-and-a-rail-for-the-operator-ui.md), amended by [ADR 0156](../adr/0156-a-title-line-for-the-operator-shell.md)). A page parses the layout with its own template and keeps its title and its styles.

- The rail groups its entries. Overview, Pages and Plans stand without a head. *Configuration* holds Devices, Display types and Plugins, *System* holds Playground, Plugins, Firmware, Maintenance, Stats and the Styleguide ([ADR 0153](../adr/0153-an-overview-page-and-a-grouped-rail.md)). Below 64rem the rail wraps into a strip under the brand, and its foot becomes a second row.
- The top bar carries the logo lockup, the title line and the page actions. The rail foot carries the search hint, the build label and the theme link.
- The title line replaces the breadcrumb and the editor bar ([ADR 0156](../adr/0156-a-title-line-for-the-operator-shell.md)). It reads `Plans / Plan solar`, and the file follows as a muted hint. Every step but the current one is a link. The current object is the `h1`. Below 40rem the file, the status and the build disappear, and the secondary actions move into a `⋯` menu.
- The shell owns the only `main` element. A page lays out its columns in a `div`, so a page rule never restyles the shell.
- The theme follows `prefers-color-scheme`. The link at the foot of the rail stores `light` or `dark` in the browser, and the page applies it before the first paint.
- The first entry is the overview. It names itself as `Overview` in the title line, and the lists reach it through the rail instead of a sub-navigation.
- Every prompt and confirmation is one native `<dialog>` from `ui/dialog.js`. A rename asks for the name with its pattern, and a delete names the references and marks its button as danger.
- `⌘K` or `Ctrl+K` opens the command palette. It searches the sections, the pages, the plans, the devices, the display types and the widgets. It reads the list APIs in the browser, so the server has no search route. The Search link at the foot of the rail opens the same palette.
- `GET /ui/system/styleguide` renders every token and every component, so a change to the look has one page to check. It links the same stylesheets as the other pages, so it shows the theme in use.

The routes name their object. `/ui/pages/{name}`, `/ui/plans/{name}`, `/ui/devices/{id}`, `/ui/devices/{id}/plan` and `/ui/display-types/{name}` open one object. The lists sit at `/ui/pages`, `/ui/plans`, `/ui/devices` and `/ui/display-types`. Downloads and Stats answer at `/ui/system/downloads` and `/ui/system/stats`. The routes of ADR 0068 and ADR 0076 are gone.

## Look

The UI looks like a well-printed instrument: paper, ink, hairlines, machine data in monospace, and colour only where it means something ([ADR 0152](../adr/0152-a-design-system-and-a-rail-for-the-operator-ui.md)).

`tokens.css` holds every value: colour, type scale, spacing, radius, shadow and motion. `ui.css` holds the components that every page shares. A page links both and keeps layout rules only, and it names no colour of its own.

Below 40rem a list table becomes a card list: `ui.css` lays the cells out one under another and shows the column name from `data-label`. A page that adds a table marks its cells, so no column is cut off. A dense table keeps its columns and scrolls sideways instead.

Light is the default. A dark system switches the palette through `prefers-color-scheme`, because the tokens use `light-dark()` with `color-scheme`. The LCD green belongs to the text preview and never becomes an interface colour.

## Which route needs what

The server keeps two route tables ([ADR 0096](../adr/0096-a-password-for-the-operator-ui.md)):

- `deviceRoutes` is what a display touches: the device API, the composed pages, the notice pages and the firmware. These stay open, because a device has no password.
- `uiRoutes` is everything else: the overview, the editors, their API, the previews. Every entry sits behind HTTP Basic auth with `ui.password`, because these pages carry the instance's content: calendars, departures, the energy of the home.

An empty password keeps the UI open and logs a warning at startup when the listener is not loopback. A test pins the prefix of the device routes, so nothing joins them by accident.

Every answer carries `X-Frame-Options: DENY`, `Content-Security-Policy: frame-ancestors 'none'` and `X-Content-Type-Options: nosniff` ([ADR 0136](../adr/0136-harden-the-operator-ui.md)). A framed page would send same-origin writes and pass the CSRF check.

`ui.hosts` names the host names the UI answers to. With a list, a request whose `Host` header names another site gets `403`. This stops DNS rebinding, where `Host` and `Origin` both point at the attacker. Without a list the UI accepts every host and logs a startup warning when the listener is not loopback. The comparison ignores the port and the case. The device routes are not checked, because a display keeps the address it was configured with.

## Overview

`GET /ui` is the operator's first page; `/` redirects to it ([ADR 0040](../adr/0040-operator-dashboard.md)). It heads the device cards with *Devices*. Below them, *Everything else* holds one card per list of the instance: Pages, Plans and Display types, each with its count and a link ([ADR 0153](../adr/0153-an-overview-page-and-a-grouped-rail.md)).

A card names the device as a link to its device file. The card itself shows:

- the last page as an image, taken from `GET /api/devices/{id}/image` ([ADR 0097](../adr/0097-page-downloads-take-the-device-key.md))
- a *Power* row with the charge state (charging, connected, discharging), the charge or the voltage
- a *Details* block with the display type, the raw battery reading, the last charge and the runtime estimate
- a *Readings* row with the room's measured values; the catalog names are its tooltip ([ADR 0134](../adr/0134-readings-from-the-device.md))
- the plan and the shown page as links to their editors
- the refresh interval and the estimated time to the next sync, the address and the firmware
- a line for a newer Paperboy release, with the version as its link ([ADR 0101](../adr/0101-a-hint-about-a-newer-paperboy.md)), or a warning when the last release check failed
- a *Signal* button that queues a beep or blink for the next sync ([ADR 0142](../adr/0142-a-manual-alert-waits-for-the-next-sync.md))

A device page links its plan, its plan preview and its display type. A page names the plans that use it. The *Signal* button posts a form and comes back to the cards. A queued request shows in *Details* with its expiry until the next sync. The button is disabled when the request cannot work. Its tooltip names why: no `alert_methods`, a problem device or no registration.

A device without a file appears as unclaimed hardware with *Claim* and *Forget* ([ADR 0080](../adr/0080-device-state-per-mac.md)). A denied device shows as denied.

Rendered pages live in memory only. After a restart the card falls back to the page preview, with its own device, or a placeholder ([ADR 0123](../adr/0123-a-sample-device-in-previews.md)).

A click on the preview opens it in a modal at the largest size the viewport holds, with `image-rendering: pixelated`. The modal names the device, the page and the age of the served page, and links the page editor. The image keeps the `src` of the card, so the browser serves it from its cache. A text display's LCD pane opens the same way, larger in the same monospace pane. The dialog sits outside `#live`, so the 30-second poll does not close it. A placeholder or a card without a page image opens nothing (issue #398).

## Self-updating pages

The overview, the stats page and the two release trains poll themselves ([ADR 0106](../adr/0106-self-updating-pages-with-htmx.md)). Each page wraps its changing part in a region `id="live"` and asks for itself again. htmx morphs the answer into the existing DOM, so a node with the same id keeps its focus, its selection and its place.

- The overview polls every 30 seconds, the stats page every 10 seconds, the release trains every 5 seconds. A hidden tab does not poll, and it refreshes once when it becomes visible again.
- The device cards, their *Details* block and their preview carry stable ids, so an opened *Details* block survives a tick.
- A served picture names its pixel hash: `GET /api/devices/{id}/image?h=<hash>` answers `Cache-Control: private, max-age=31536000, immutable` for the current hash and `no-store` otherwise. A tick therefore downloads no image.
- The downloads poll keeps the query string of the page. The cleanup preview does not poll at all, so its list cannot change under the button next to it.
- Without JavaScript the pages stop updating. They stay readable, and every link works.
- htmx 4.0.1 is vendored in `server/internal/paperboy/ui/vendor/` and served as `GET /ui/htmx.js` behind the password. It is the only foreign JavaScript in the service; `ui/vendor/` is the only place for it, and a new file there needs an ADR.

## Runtime stats

`GET /ui/system/stats` is read-only and polls itself ([ADR 0104](../adr/0104-runtime-stats-in-the-web-ui.md), [0106](../adr/0106-self-updating-pages-with-htmx.md)). It shows Paperboy's heap, goroutines, GC, resident memory and CPU. One row per plugin carries state, program, PID, memory, CPU, requests, errors and timings. A second block names what the operating system sees of each plugin. It shows disk bytes, open files and connections with their peer. It names the last exit with its code or signal and the last 50 stderr lines. A signal such as `SIGSYS` explains a plugin that a sandbox or seccomp filter killed. The lists are cut after a few entries and the card says how many follow. Windows reports no observation yet. The widget cache shows entries, images, bytes and age buckets; the rendered pages and the notice pages follow. Named timings carry count, average, p50, p95 and maximum. Per device it shows the syncs with their duration, and the text polls separately. CPU shares compare with the previous view of the page, because they come from a difference of two readings.

## Release trains

Each train has a page of its own ([ADR 0154](../adr/0154-split-the-release-trains-and-add-maintenance.md)): `GET /ui/system/plugins` and `GET /ui/system/firmware` ([ADR 0060](../adr/0060-download-from-releases.md), [0062](../adr/0062-plugin-updates-and-download-control.md)). Both show the whole check, because one check installs both trains:

- source, interval, the last check and the running check with its button (`POST /ui/download`)
- the release channel of each train, with a note when the running version is not one the channel would deliver
- the plugin and firmware bundle that the last check used (`plugins-YYYY.MM.DD`, `firmware-YYYY.MM.DD`), a dash before the first use
- a `Paperboy` row with the newer version or the state of the comparison ([ADR 0101](../adr/0101-a-hint-about-a-newer-paperboy.md))

The *Plugins* page adds every configured plugin with its resolved program and version. A version a pin selects carries `(pinned)`, and the row names when the program arrived. The *Firmware* page adds the images in `firmware/`, including the old `<hardware>/` layout and the files that are ignored. Each page names the size of its directory and links *Maintenance* for the cleanup.

## Maintenance

`GET /ui/system/maintenance` is the only page that deletes files ([ADR 0090](../adr/0090-download-cache-and-inventory.md), [0154](../adr/0154-split-the-release-trains-and-add-maintenance.md)). It offers three cleanups, each with a preview that lists what would go and a button that deletes exactly that (`POST /ui/system/maintenance/delete`):

- *Inventory*: the files of `plugins/` and `firmware/` that no program, pin, device or recent version needs.
- *Device state*: the state of a MAC without a device file that was not seen for 30 days. Recent entries stay, because a device that registers waits for a claim ([ADR 0080](../adr/0080-device-state-per-mac.md)).
- *Editor backups*: the copies under `state/editor/` whose page, plan, device or display type is gone ([ADR 0066](../adr/0066-page-and-plan-api.md)). A save and a delete keep up to ten copies per object.

The page does not poll, so a preview list cannot change under the button that deletes it.

## Plan preview

`GET /ui/devices/<id>/plan[?at=…]` answers what a device would show, without touching it ([ADR 0054](../adr/0054-plan-preview-in-the-web-ui.md)). It lists the rules and marks the active rule or the fallback. It names the next change and the rule's alert mode ([ADR 0131](../adr/0131-a-plan-rule-alerts-the-device.md)). Each rule shows its value conditions with the key each value answers now, evaluated now even when `at` shows another time ([ADR 0127](../adr/0127-plan-conditions-are-values.md)). A plan condition has no label, so the view names the key. Each page shows a preview image, inline text lines, a skip reason or a page error. The previews name the device, so its status widgets show its values ([ADR 0123](../adr/0123-a-sample-device-in-previews.md)). The view takes no state lock and never moves a cursor.

## Virtual display

`GET /ui/virtual?plan=<plan>&display=<type>[&refresh=<s>&menu=<s>&power=mains|battery]` plays the firmware in the browser ([ADR 0091](../adr/0091-virtual-display.md)). It shares `serveSync` with the device API, so plan selection, paging, notices, menu and confirmation behave as on a device.

- `POST /ui/virtual/sync` takes the device sync body; `POST /ui/virtual/reset` drops the session.
- The session is a `deviceState` in memory, keyed by plan and display type. It has no device file, no MAC and no key. It has no entry under `state/devices/` and is absent from `/api/devices`.
- The URL is the only configuration; an invalid value answers 400.
- Actions resolve, select and confirm, but stop before the plugin call, with a `Simulated` banner that names the action.
- The answer adds a `debug` object with rule, page, error and menu for the line below the display.
- The images come from `/ui/virtual/pages/…` and `/ui/virtual/hints/…`, which serve what the device routes serve, because a browser has no device key ([ADR 0097](../adr/0097-page-downloads-take-the-device-key.md)).
- A text display paints one character per slot in the same LCD module as the editor board, with the vendored pixel font ([ADR 0116](../adr/0116-a-pixel-font-for-text-previews.md)). The 20 × 4 and 16 × 2 grids do not reflow.

## Playground

`GET /ui/playground` is the page editor without a page ([ADR 0055](../adr/0055-allowed-arg-values-and-gallery-form.md), [0077](../adr/0077-the-widgets-page-has-no-gallery.md)). It offers image display types only, because a text page is a character grid of boxes ([ADR 0111](../adr/0111-text-pages-from-plugin-values.md), [0124](../adr/0124-the-playground-shows-image-display-types.md)). The link carries the widget, its span, the page's grid and its args. The page shows the palette and one widget rendered in its cell size from `/api/widget`. The inspector holds its size, args and style, and the snippet panel holds the cell. A link from a page previews the cell the page has, not a reference cell ([ADR 0128](../adr/0128-a-default-grid-per-display-type.md)). The display type switch keeps the widget, its span and its args. A link with a text display type opens the first image type, and a hint names *Pages*. With text display types only, the page names that page instead of failing. Without a widget in the link it starts with the first fitting widget of the palette. An old `?tab=gallery` link still opens the page.

`GET /api/widget/{plugin}/{widget}` renders a single widget: `display`, `span` on `grid` (the display type's grid unless the request names one), `width`/`height`, and `arg.<name>` per argument. The answer is a PNG, and a failure is the cell notice with an `X-Widget-Error` header. A text display type answers `400` with a hint that its pages are built from values ([ADR 0125](../adr/0125-remove-the-text-widget-path.md)).

## Pages, plans, devices and display types

Each list is a page of its own: `/ui/pages`, `/ui/plans`, `/ui/devices` and `/ui/display-types`. Its count sits in the title line, next to the list name. Its new-object button sits at the right edge of the top bar ([ADR 0156](../adr/0156-a-title-line-for-the-operator-shell.md)). The Playground is a tool under System (ADR 0153). The button asks for a name in a dialog and opens the editor of the new object. The pages tab also asks for the kind, and for a text page its character grid. A new image page starts at the grid of the chosen display type ([ADR 0128](../adr/0128-a-default-grid-per-display-type.md)). The stale switch is *Stale values* on a text page and *Stale widgets* on an image page. A row opens its object: the name is a bold link and a click on the row follows it. *Rename* and *Delete* live in the editor and name the references ([ADR 0073](../adr/0073-edit-creates-and-deletes.md), [0079](../adr/0079-rename-config-items.md), [0084](../adr/0084-the-overview-links.md)).

The pages tab lists one row per page ([ADR 0110](../adr/0110-pages-serve-their-kind-and-name-variants.md)). The row shows the page file when it exists, else its first variant, with kind, grid, title and parse error. The variants column lists the other files as chips with their label. A chip tooltip names the path, the display types the page sends to it and any error. The row and the chips open exactly that file. A variant that no display type names is italic and stays a candidate in the *Variants* select. A page without a page file shows its name in red; the operator creates the file with *New page*. Its variants stay read-only until then.

**Page editor** `GET /ui/pages/<name>[?file=<label>][&display=<type>][&kind=<kind>][&new=1]` edits exactly one page file ([ADR 0068](../adr/0068-page-editor-ui.md), [0085](../adr/0085-moving-and-resizing-widgets.md), [0110](../adr/0110-pages-serve-their-kind-and-name-variants.md), [0111](../adr/0111-text-pages-from-plugin-values.md)). Without `file` it opens the page file, else the variant with that label. `display` selects the preview type only; the bar names the open file. `new=1` starts a page file. `kind` decides image or text. For a text page, `display` names the display type whose character grid the page starts with.

The page file shows one *Variants* table: a row per configured display type with its kind and a select. The select offers the files of the page that fit the type: `page file`, the fitting variants and `— none —`. It writes the page's `variants` entry ([ADR 0110](../adr/0110-pages-serve-their-kind-and-name-variants.md)). Its tooltip says why a type is not served: another kind, another character grid or a file that does not parse. The display type is a link: the open file previews it, or it opens the file that serves it. The current preview type is marked. The editor never renders another file in place of the open one. A link to a type the open file does not serve keeps a short notice. A `variants` entry whose display type is no longer configured keeps its row in the table; choosing `— none —` drops the entry and leaves the file. The select offers `— none —` wherever the page file cannot serve the type. One *Create variant…* button opens a dialog for a display type and a file label. It starts with the first type that no file serves, else with the first type. The new variant starts from the type, so its kind and grid never come from the open file (issue #238). An image variant starts at the display type's default grid ([ADR 0128](../adr/0128-a-default-grid-per-display-type.md)). Every type is a candidate, and the page sends it to the new file.

A variant is a child page. The *Parent* row names its page file as a link. The Title field shows the title of that file read-only; the page file owns the title. The block lists the display types the page sends to the file, one per line, or `none`. Each type previews the open file, and the current preview type is marked. Two buttons act on the file. *Rename…* asks for a new label; the file moves, and the page rewrites every entry that names the old label. *Reassign…* offers the other pages with a page file; the file moves, and its entries go to that page's map. A variant that no entry names moves without touching a map. A variant that no display type names shows a yellow warning. The editor opens the file at its new place after either action. The page file owns the variants, and the bar carries no page rename on a variant. A variant whose page file is missing stays read-only and offers no action. The pages list links none of its files and names the missing page file.

Every editor carries its object in the title line. Its status and actions sit at the right edge: status · Rename · Delete · Reload · Save ([ADR 0076](../adr/0076-one-stylesheet-and-one-navigation.md), [0150](../adr/0152-a-design-system-and-a-rail-for-the-operator-ui.md)). Every page links `tokens.css` and `ui.css`, shows the logo lockup in its header, links the favicon, and runs one embedded script with no build step. The logo and the favicon are copies of `docs/logo/`, and a test keeps them identical to the design files.

On a text page the right inspector shows only the selected cell. It holds `at`, `span`, a *Content* switch between value and literal text, `label`, `labels`, `align` and `overflow`. An enum value shows one label input per declared key, empty means the key itself. The unit is a choice of declared, none and custom; a scale select forces a step. A value with a unit progression offers `auto`, its base unit and every declared step in the scale select. A full-width *Delete cell* button sits at the bottom. A literal text cell has a textarea and no label, labels or unit. With no selection it shows a short hint. A text page hides the style in points and has no rows panel: boxes are placed in two dimensions directly on the board. The page's `overflow` is a field next to *Title*; the stale switch is *Stale values*. The palette lists the declared values and a *Text* entry, grouped by plugin with a filter. Click a value or *Text*, then a free slot; drag a value onto the board. Drag a box to move it and an edge or corner to resize it. A box that does not fit or would overlap is refused. Scrolling boxes stay static on the board, and its note names `scrolls, step N ms`. Without JavaScript the page renders its boxes as read-only fields and saves nothing.

An image page's *Grid* holds `cols` and `rows`. A lock next to them keeps the ratio of the grid. A change to one axis derives the other from the ratio it was switched on with, rounded up. 6 × 8 becomes 3 × 4, and 2 × 3 ([ADR 0128](../adr/0128-a-default-grid-per-display-type.md)). The lock changes nothing by itself; it applies to the next change. The note under the board names the cell size and its deviation, because the ratio says nothing about the panel. The lock is a browser preference, not part of the file. The board is the rendered draft with the cell boxes on top ([ADR 0074](../adr/0074-editor-board-shows-the-preview.md)). A variant previews with the title of its page file, as it does on the device. An image is the background. Text appears one character per slot in a dark green LCD module with light green pixels and the vendored pixel font ([ADR 0116](../adr/0116-a-pixel-font-for-text-previews.md)). The cells have a one-pixel gap, and the glyph size follows the slot height. The 20 × 4 and 16 × 2 grids stay exact. A *Cells* switch, which the browser remembers, hides the boxes, and a "rendering…" hint covers the debounce. A cell comes from the palette by click or drag. The palette of an image page lists one entry per widget ([ADR 0152](../adr/0152-a-design-system-and-a-rail-for-the-operator-ui.md)). An entry carries a small picture from `/api/widget`, the widget's title and the sizes it declares. Pinned widgets sit on top in their own group ([ADR 0157](../adr/0157-drop-the-recent-widgets.md)). It starts open, folds on its own, and the browser remembers the pins. One plugin group is open. A click on a head opens it and folds the others. A filter shows every group with a match and suspends the fold. The selected cell opens the group of its widget and moves the widgets that fit it to the top. A folded group and an entry out of view ask for no picture. The Playground ranks nothing, because a pick replaces its one cell. `/` focuses the filter, the arrow keys walk the entries and Enter places the focused one. It moves with the pointer, so the grabbed point stays under the pointer, or by its position. An edge or corner handle resizes it.

The inspector draws the arguments from the plugin's `describe` answer. A `string` with an `enum` is a select, and a `list` with an `enum` is one check box per value. The device arguments of the built-in plugin offer the configured device ids ([ADR 0123](../adr/0123-a-sample-device-in-previews.md)). Their empty value reads *the device that shows the page* in the configured language. A stored id that no device file has keeps its option, marked *not configured*.

**Plan editor** `GET /ui/plans/<name>[&new=1]` edits one plan file ([ADR 0069](../adr/0069-plan-editor-ui.md)). A missing file opens as a new plan and is written by the first save. The editor lists the rules in order with name, `from`/`to`, days, page order, `on_refresh`, `refresh` and `alert`. Each page row names the page and the display types it serves. The name keeps its room on a narrow screen: the types wrap below it. The page picker lists the page names. A condition is a choice of the enum values in the plugins' `describe` answers and one check box per key. A value or key that no plugin declares stays visible and warns ([ADR 0127](../adr/0127-plan-conditions-are-values.md)). Then the fallback and the actions, with the plan-local `id` rule of [ADR 0057](../adr/0057-plugin-actions-from-the-plan.md). A side panel names the devices of the plan and the pages with their skip reason per display type. It links to `/ui/devices/<id>/plan` and to the device pages.

**Device and display type editors** `GET /ui/devices/<id>[?new=1]` and `GET /ui/display-types/<name>[?new=1]` edit the fields of their file next to the raw text ([ADR 0070](../adr/0070-devices-in-their-own-files.md), [0071](../adr/0071-display-types-in-files-of-their-own.md)). The device editor offers the display types, the plans, refresh, menu settings, the alert methods, power, the firmware hold and the battery thresholds. The display type editor shows the fields of its kind and computes the physical size from `ppi`. An image type has a `grid` field: the default grid of a new page. The editor suggests the coarsest grid with cells within 3 % of square ([ADR 0128](../adr/0128-a-default-grid-per-display-type.md)). It names the cell size of the entered grid and the deviation from square. Both list the references that keep the old name.

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
| `GET /api/devices` | Devices with the address of the last contact and the fuel gauge reading; an explicit field set without the device key or the pairing code ([ADR 0140](../adr/0140-an-explicit-devices-api-answer.md)) |
| `POST /api/devices/{id}/alert` | Queues a manual alert for the next sync and redirects to the cards ([ADR 0142](../adr/0142-a-manual-alert-waits-for-the-next-sync.md)) |
| `GET /api/preview/{display}/{page}` | Page preview, optional `at=YYYY-MM-DDTHH:MM` and `device=<id>` |
| `POST /api/preview/{display}` | Draft preview of a page that was not saved |

Two rules guard every write. A write carries the hash of the file it read and is refused with 409 when the file changed in between ([ADR 0066](../adr/0066-page-and-plan-api.md)). Every write and every delete keeps a backup, the last ten versions, under `state/editor/` ([ADR 0073](../adr/0073-edit-creates-and-deletes.md)). A backup that cannot be written leaves the file unchanged. A rename moves files only and refuses a name that already exists with 409. It never rewrites a reference ([ADR 0114](../adr/0114-a-rename-does-not-ask.md)). A page rename moves the page file and every variant as one set, keeps the `variants` map and names each moved file ([ADR 0113](../adr/0113-a-page-rename-moves-its-variants.md)). A device rename moves only the file, so the state follows the MAC ([ADR 0080](../adr/0080-device-state-per-mac.md)). Before the first rename, each move is preflighted: every source must exist and be readable, every target must not exist. Each target directory must be writable, and a failed check leaves every file in place. A variant rename or move backs up every page file it rewrites and undoes the file move when a write fails.

## Decisions

[ADR 0040](../adr/0040-operator-dashboard.md) (dashboard) · [0054](../adr/0054-plan-preview-in-the-web-ui.md) (plan preview) · [0055](../adr/0055-allowed-arg-values-and-gallery-form.md) (argument values) · [0066](../adr/0066-page-and-plan-api.md) (page and plan API) · [0068](../adr/0068-page-editor-ui.md) (page editor) · [0069](../adr/0069-plan-editor-ui.md) (plan editor) · [0070](../adr/0070-devices-in-their-own-files.md), [0071](../adr/0071-display-types-in-files-of-their-own.md) (device and display type files) · [0073](../adr/0073-edit-creates-and-deletes.md) (create and delete) · [0074](../adr/0074-editor-board-shows-the-preview.md) (the board) · [0076](../adr/0076-one-stylesheet-and-one-navigation.md) (one stylesheet, one navigation) · [0077](../adr/0077-the-widgets-page-has-no-gallery.md) (no gallery) · [0079](../adr/0079-rename-config-items.md) (renaming) · [0080](../adr/0080-device-state-per-mac.md) (unclaimed hardware) · [0084](../adr/0084-the-overview-links.md) (overview links) · [0085](../adr/0085-moving-and-resizing-widgets.md) (moving and resizing) · [0091](../adr/0091-virtual-display.md) (virtual display) · [0094](../adr/0094-the-pages-list-is-a-list.md) (the pages list) · [0096](../adr/0096-a-password-for-the-operator-ui.md) (password) · [0097](../adr/0097-page-downloads-take-the-device-key.md) (images behind the UI) · [0101](../adr/0101-a-hint-about-a-newer-paperboy.md) (newer Paperboy) · [0104](../adr/0104-runtime-stats-in-the-web-ui.md) (stats) · [0106](../adr/0106-self-updating-pages-with-htmx.md) (self-updating pages) · [0110](../adr/0110-pages-serve-their-kind-and-name-variants.md) (page file and variants) · [0113](../adr/0113-a-page-rename-moves-its-variants.md) (a page rename moves its variants) · [0116](../adr/0116-a-pixel-font-for-text-previews.md) (the pixel font of text previews) · [0123](../adr/0123-a-sample-device-in-previews.md) (the sample device in previews) · [0131](../adr/0131-a-plan-rule-alerts-the-device.md) (alert in the editor and preview) · [0132](../adr/0132-alert-methods-per-device.md) (alert methods in the device editor) · [0136](../adr/0136-harden-the-operator-ui.md) (framing and DNS rebinding) · [0139](../adr/0139-bound-the-page-caches.md) (bounded page caches) · [0140](../adr/0140-an-explicit-devices-api-answer.md) (the devices API field set) · [0142](../adr/0142-a-manual-alert-waits-for-the-next-sync.md) (the signal button)
