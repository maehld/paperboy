[← Paperboy](../README.md)

# Runtime

## Web UI

Paperboy serves its operator pages on the same address as the device API. They need no token (see [Security](#security)).

### Dashboard

`GET /` redirects to `/ui`, a read-only overview of all devices (ADR 0040). Each card shows the status, the plan and its active rule, a preview of the last served page (the lines for text displays), last contact and a *Power* row with the charge state — `Charging`, `Connected` (on mains without a charge current) or `Discharging` — followed by the state of charge or, when the hardware has no percentage, the voltage; the battery warning adds `low` or `critical`. The raw battery reading (voltage, charge and current), the display type with the hardware, refresh, address and firmware sit in a collapsed *Details* block, a *Last charge* line names when the last charging episode ended and what it did (`2 h ago · 41 → 50 %`, the voltage pair on hardware without a percentage), and a *Runtime* line estimates how long the battery lasts (`~12 d`, below two days in hours), marked as an estimate; the last render or plan error is shown below the card as text. The page is a server-rendered Go template without JavaScript, refreshes itself every 30 seconds (which closes an opened *Details* block) and needs no token. Rendered PNGs live in memory only, so after a restart a card omits `last_page_url` and uses the page preview or a placeholder. The header shows the running build: version (or `dev`), short commit with `+dirty` for local changes, and commit date from the VCS stamp; the Go toolchain is in the tooltip. Every page links the editor.

The *Last charge* line comes from the fuel gauge current, with the threshold of the *Power* row: an episode starts with the first sync above 20 mA and ends with the first one below it. Both ends keep the charge and voltage the hardware reported, and the state file keeps the episode, so a charge that ends while Paperboy is down closes at the next sync — with that sync's time and charge, not the real ones.

The *Runtime* estimate learns the discharge rate from the device instead of a model of the cell: Paperboy keeps one battery sample per day (at most a week) in the state file and divides the charge of the last sample by the drop between the first and the last one. It needs two samples over more than a day, says nothing while the series rises and nothing beyond a year. A charge clears the samples, so it does not look like a slow discharge; hardware without a fuel gauge uses the voltage down to an empty cell at 3.0 V.

### Stats

`/ui/stats` (ADR 0104) shows what Paperboy costs while it runs. It is server-rendered, refreshes itself every ten seconds and writes nothing.

The first block is Paperboy itself: start time and uptime, goroutines, `GOMAXPROCS`, heap in use against the memory the runtime reserved, the live heap with its object count, the stack, the garbage collector (cycles, next target, last pause), the resident memory and the CPU time with the share of one core used since the previous view. CPU percentages compare with the previous view of the page, because a process only exposes its cumulative CPU time; the first view names the total and shows no percentage.

The plugin table has one row per plugin — the built-in one included. It names the state (running since, failing with the retry time, not running, built-in), program and version, PID, resident memory and CPU, the requests it answered with the failed ones and the last error, and two timings: *Answer* is how long the plugin took, *Wait* is how long a request waited for another request of the same plugin. The last columns are the widgets it keeps in the cache with their bytes. Memory and CPU come from the operating system and stay empty for the in-process plugin and on platforms that do not report them.

The widget cache follows: entries, images, bytes, stale entries, entries past their expiry, evictions and the age of the oldest entry, then one row per age range (under a minute, five minutes, an hour, a day, older) with entries, images and bytes. Below it Paperboy counts its own caches: rendered pages and notice pages with count, bytes and age, and the parsed page and plan files.

*Timings* lists named operations with count, last, average, p50, p95 and maximum: the sync as a whole, the configuration snapshot, the wait for the state lock, the plugin conditions, plan selection, the page render split into waiting for plugins and PNG encoding, running a menu action, writing the state file, every page render and the text poll. Percentiles come from the last 256 samples of an operation. *Syncs and polls* finally has one row per device: its syncs with last, average, p95 and maximum duration, and the text polls separately, because a text display asks every second and would otherwise dominate the numbers.

### Plan preview

The **Plan preview** button at the foot of a device card opens `/ui/plan?device=<id>` (ADR 0054), a read-only view of the plan of the device. It shows every rule with its time window, `rotate` and `refresh`, the fallback, the rule that applies now (or at the time picked with `&at=YYYY-MM-DDTHH:MM`) and the next rule change. Each page is previewed for the display type of the device; pages the device would skip show the reason, broken pages their error. The view does not change the page cursor or the state of the device.

### Virtual display

The **Virtual display** button at the foot of a device card and the *virtual display* link in the plan view open `/ui/virtual?plan=<plan>&display=<type>` (ADR 0091). The page acts as a display of that type: it syncs like the firmware, shows the page the plan picks, pages with the buttons of the plan and opens the device menu. Optional URL parameters replace what a device file would decide: `refresh`, `menu` (5–60 s), `page_timeout` (all seconds) and `power=mains|battery`. The URL is the only configuration, so a link is a display. The cursor and the menu live in memory and not in `state/devices/`, so no device is touched. *Reset* starts again at the first page. The picture comes from `/ui/virtual/pages/…` and `/ui/virtual/hints/…`, which serve the same PNG and QOI as the device routes but follow the UI password (ADR 0096): the browser has no device key (ADR 0097).

Paperboy resolves, selects and confirms actions like on a device, but never runs them: the result banner says *Simulated* and names the action. `←`/`→`/Enter and press-and-hold work, *Full screen* hides the chrome, and a debug line below the display names the active rule, the page, the refresh, the wake and the last error.

### Widgets page

`/ui/plugins` (linked from the dashboard) opens the page editor without a page. Pick a widget in the palette, or open the page without a widget and it starts with the built-in header. Paperboy draws the widget in its cell size on the chosen display type. The inspector shows its size, arguments and style. The snippet panel shows the finished cell, ready to copy into a page. Sizes are spans on a reference grid of 6 × 8 cells (`?grid=` changes it), and plugins declare their sizes as spans of that grid.

`GET /api/plugins` lists every plugin with its widgets, the sizes they are designed for, their arguments and the config keys of the plugin. A single widget is available as `GET /api/widget/<plugin>/<widget>?display=<type>` with `span=3x2` (and `grid=6x8`) or `width`/`height` in pixels, and arguments as `arg.<name>=<value>` (JSON values keep their type, e.g. `arg.days=3`). It answers a PNG or `text/plain`, and for a failing widget the notice a page would show, with the reason in `X-Widget-Error`.

### Editors

`/ui/editor` is one overview with a sub-tab per object type (`?tab=pages|plans|devices|displays`, default pages). Each tab has one card with its count, a filter field and its new-object button in the header. The button asks for the name in a dialog (a page also chooses the display type) and opens the editor of the new object. A row opens its object: the name is a link, and a click on the row follows it. Rename and Delete live in the editor and name the references (ADR 0073, 0079, 0084). Every editor shows the same action bar `← <list>` · object · status · Rename · Delete · Reload · Save (ADR 0076). A save sends the model with the hash it read, so a file changed on disk answers `409` and asks for a reload. Paperboy keeps the last ten old versions under `state/editor/`.

#### Page editor

The pages tab lists every page once, without a display type switch (ADR 0094). A row shows the shared page when it exists, else the first variant, with kind, grid, title and parse error, and opens exactly that file; the variants column only names the files (`shared · small`, path in the tooltip, a broken file in red). A file that no editor can open is removed on disk. `/ui/editor/page?name=<page>&display=<type>` builds one page for a display type (ADR 0068); `&shared=1` names the shared page and `&file=<type>` a variant. The editor names the open file behind the page name (`page countdown - shared`, `page countdown - small`) and toggles between *Edit … variant* and *Edit shared* when both files exist. It edits the title, the grid and the style. On text displays the grid is fixed to the character grid. The palette from `/api/plugins` is filtered by the kind of the display. For the selected cell the editor sets the span, the position, the arguments and the style.

Click a widget and then a free cell, or drag it from the palette. A placed box is moved with the pointer (it stays where it was grabbed) or by its position. Resize it by the handles on its right edge, bottom edge and corner; a size or position that would overlap another box is not applied (ADR 0085). A live draft preview shows the result.

The board is the rendered draft with the cell boxes on top (ADR 0074): an image as its background, the text lines one character per slot. A *Cells* switch hides the overlay (remembered per browser, forced on while placing), and a "rendering…" hint covers the debounce.

Nothing is written before *Save*. A shared page stays the shared file until *Save as … variant*. A file that does not parse is listed with its error and opens read-only, because the API takes a model, not text. *Delete* removes the file the editor shows after naming the plans that use the page (ADR 0073). *Rename* moves the shared page and every variant as one set and names the plans that keep the old name (ADR 0079). A variant of a display type that is no longer configured opens through a configured type of the same kind and is saved as that type's variant.

#### Plan editor

`/ui/editor/plan?name=<plan>` edits a plan (ADR 0069). A missing file opens as a new plan that the first save writes. The editor edits rules in their order: name, time window (`from`/`to` as times of day, days as checkboxes), pages in their order with `rotate` and `refresh`, and the fallback. It also edits the actions the device menu offers, built from the actions of the plugins with their arguments and confirmation.

The editor marks rules that an earlier rule always overrides. It names pages that the own devices of the plan would skip, and links each device to its `/ui/plan` preview and to its device page. *Delete* removes the plan after naming the devices that use it. *Rename* moves the file and names the devices that keep the old name (ADR 0079).

#### Device editor

The devices tab lists the files under `devices/` with display type, MAC, address and plan, and creates new ones in a dialog. Hardware that registered with an unknown MAC appears below them as unclaimed, with its hardware, firmware, address and first and last contact. *Claim* asks first, then opens a new device file with the MAC prefilled; *Forget* deletes its state file (ADR 0080).

`/ui/editor/device?id=<id>` edits one (ADR 0070): display type, MAC address, plan chosen from the plan files, `refresh`, the device menu (`actions`, `menu_show_duration`, `touch`), the power source, the rotation (a device override, ADR 0102), the firmware hold and the battery thresholds. It links the plan preview of the device and the current file text. The editor writes only the device file, so a plan assignment never touches `paperboy.yaml`. A new device file needs a display type and a MAC address.

*Delete* removes the file; the device stops syncing until a file with its MAC exists again (ADR 0073), and its state waits under the MAC. *Rename* moves only the file; the state stays with the MAC, so the page cursor, battery reading and menu survive (ADR 0079, 0080).

#### Display type editor

The displays tab lists the files under `displays/` with kind, size, `ppi` or `charset`, gray levels and power, and creates new types in a dialog. `/ui/editor/display?name=<name>` edits one (ADR 0071) with the fields of its kind, the rotation of an image display (ADR 0102), the physical size computed from `ppi`, the page variants and devices that reference it, and the current file text.

The `ppi` field has a helper. It converts the diagonal of the panel in inches or one side of its active area in millimetres into `ppi` from the pixel size in the form. It says so when the two disagree, because an inch diagonal is usually rounded. *Rename* moves only the file and names the page variants and devices that keep the old name (ADR 0079). *Delete* removes the file after naming the page variants and devices that reference it (ADR 0073).

## Device API

`POST /api/device/register` with `{"mac", "hardware", "firmware"}` (optionally `power` and `touch`) returns `{"key", "device"}` for a configured MAC (an existing key is kept) and `403` otherwise (ADR 0042, ADR 0064). Every wake sends one `POST /api/device/sync` with `Authorization: Bearer <key>`:

- `reason`: `timer`, `button`, `touch`, `powercycle`, `menu` or `result`.
- `events`: buttons by role `next`, `previous`, `home`, optionally with `hold_ms`; touches with `x`/`y`. Each event has a monotonically increasing `id`, and Paperboy ignores a repeated ID.
- `shown`: the hash on the panel.
- `battery`: `volts` is required, `percent`, `mah` and `ma` are optional. `ma` is the current from the fuel gauge, positive while charging. `charging` is the charger's own state (the Sticky reads the BQ25616's `STAT` pin) and cross-checks the gauge.
- `rssi`, `power` and `log` entries.

The answer names the image (`image.path` relative to the Paperboy URL, `image.hash`) and the deep-sleep time `refresh_s`. It also names `listen_s` (0 after a timer wake, 20 s after input) and `sleep` (`false` for mains devices, ADR 0051). It names the server `time` in Unix seconds and `firmware` (an update offer, otherwise `null`). `rotation` is the effective page rotation in degrees clockwise (0, 90, 180 or 270), omitted when it is 0; the firmware applies it before painting and to touches (ADR 0102). `wake` (`{reason, after_s}`) is a one-shot wake for the menu deadline or an action result; otherwise it is absent. An unknown key gets `401`, and the device registers again. An invalid body gets `400`.

A device whose file has a `Problem` answers with `503` and the reason, never with `401` (ADR 0079).

**Text displays** (ADR 0044): register and sync like ePaper displays, but the sync answers `"image": null` and `"text": {"lines", "hash"}`, and `listen_s` is always 0. They run on mains power and never sleep. Between syncs they poll `GET /api/device/text?shown=<hash>` with their key every `refresh_s`, which the plan decides (ADR 0045; the firmware syncs at most once a minute and uses one minute until its first sync). The answer is `{"lines", "hash"}`, plus `"scroll"` with the full text of every row of a cell with `scroll: true` (ADR 0092); unchanged text gets `204`. The poll renders the page the cursor points to without advancing it and writes nothing to the state file of the device. It updates the shown content in memory for `/api/devices` and the dashboard. An image display gets `409`. Paperboy word-wraps notices (no plan, broken page, critical battery) into the lines.

**Buttons**: events apply in order before rendering. `next` = next page, `previous` = one page back (wrapping), a long `next`/`previous` = first page of the active rule, `home` = the device menu (ADR 0057, ADR 0067, see [Actions](content.md#actions)). A short `home` always opens the menu, also when the plan offers no actions; then it is an empty banner. With the menu open, `next`/`previous` move the selection and `home` confirms; a long `home` closes the menu and keeps the page. After `page_timeout` (5 min) the device shows the first page again. Touches operate the device menu when it is open (the Sticky sends taps, ADR 0064, issue #54); outside a menu, Paperboy only logs them. The server decides at 700 ms whether a press was long; the firmware only measures and reports `hold_ms`.

## Operation

### Intervals

`defaults.refresh` (at least 1 minute) or the `refresh` of the device applies; the `refresh` of a device may go down to 1 s. A plan rule may override it (down to `1s`). The reported interval never goes below the floor of the power source (ADR 0051). Battery devices keep one minute or the `min_refresh` of the display type if that is longer. Mains devices go down to `min_refresh`: by default `1s` for text and `10s` for image displays. When the composed image does not change, the display does not redraw.

### Power source

`power: auto|battery|mains` on the display type, overridable per device; default `auto` (ADR 0051). The firmware reports `power` (`battery` or `mains`) in register and sync: the Sticky measures USB power on GPIO9, the E1001 cannot detect it and reports `battery`, and text displays report `mains`. `auto` uses the last report (`reported_power` in `/api/devices`, next to the effective `power`) and behaves as `battery` until the device reports one. Text displays with older firmware therefore fall back to a one-minute floor until you update them or set `power: mains`. The sync answers `sleep: false` for mains devices: the ePaper firmware then stays awake with Wi-Fi up and syncs again after `refresh_s` instead of deep sleeping. A change of the reported source or of the charger state gets one `power changed` line in the journal (with `power`, `charger`, voltage and current), so the log tells the story instead of repeating the reading on every sync.

### Battery warning

The warning is fed by `battery.volts` of the sync (ADR 0020). With `battery` in `defaults` or on a device, every page below `warn_volts` gets the bar *Battery low – please charge* (*Akku schwach – bitte laden*). Below `critical_volts` a full-screen image *Battery almost empty* (*Akku fast leer*) appears instead, and buttons then do not page. A level ends only `hysteresis_volts` above its threshold. Without `battery` there is no warning. All texts use the configured `language`.

### Firmware updates

Put OTA images into `firmware/<hardware>-v<version>.bin` of the instance (`firmware/esphome/build.ps1 -Hardware sticky -Instance <dir>` does it). Paperboy offers the highest version in the sync as `{"version", "path", "md5"}`. It compares the version part by part as numbers; a `firmware.version` pin narrows it to exactly that image (ADR 0060). It offers an update when all of these conditions hold (ADR 0043):

- `firmware.update` is `auto`.
- The device has no `firmware: hold`.
- The reported version differs (a lower file is a rollback).
- The battery reading is at least `min_battery_volts` and not at warning level (text displays run on mains power and skip the battery condition).
- The version has not failed three times on that device.

A sync that still reports the old version after an offer counts as a failure. A changed image (new MD5) or clearing `firmware_failures` in the state file `state/devices/<MAC>.json` allows new attempts. Paperboy serves images at `GET /firmware/<file>` without a token; it ignores other file names and lists them in the log and on the dashboard. `GET /api/devices` adds `firmware_available` and `firmware_status` (`current`, `offered`, `available`, `failed`, `held`, `none`). Test the rollback with a broken image before you use `update: auto`.

### Device logs

The sync carries firmware log entries (`level`, `message`, optional `source`, `time` and `id`). Paperboy writes one line per entry with device, level, source and age. An entry with a `time` or `id` that a repeated sync delivers again is logged only once.

### Device list

`GET /api/devices` shows every configured device without keys: registration, hardware, firmware, battery voltage (and `battery_percent`, `battery_mah`, `battery_ma` and `battery_charging` from hardware that knows them; `battery_ma` is positive while charging), signal strength, wake reason and last sync. It also shows what the device is showing (ADR 0039). The fields are `last_page`, `last_page_kind` (`page`, `notice` or `battery`), `last_page_hash`, `last_page_url` and `last_refresh` (text displays: `last_text` instead of `last_page_url`). The last render or plan error is in `last_error`, and a derived `status` is `ok`, `late`, `never` or `unregistered`. `late` means the last sync is more than twice the reported refresh old; devices deep-sleep, so an idle device is not an offline one.

## Security

Devices are admitted by allow list and use their own key. The device API answers with plain HTTP status codes (`400`, `401`, `403`). The dashboard, the editor, the preview, the virtual display, `GET /api/devices`, `GET /api/plugins` and the page and plan API need no token, so keep Paperboy in a trusted network. Whoever reaches `/ui` can change the pages and plans. Only plugins named in `paperboy.yaml` run, so the configuration file is executable content.
