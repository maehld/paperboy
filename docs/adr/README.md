# Architecture Decision Records

Every important decision is recorded here as a short ADR (format after Michael Nygard).

- File name: `NNNN-<short-title>.md`, numbered in sequence.
- Status: `Proposed`, `Accepted`, `Superseded by ADR-NNNN` or `Rejected`.
- Do not rewrite an existing ADR. A changed decision gets a new ADR, and the old one is marked as superseded.
- Template: [`0000-vorlage.md`](0000-vorlage.md)

An ADR records **why** a decision was made and what it replaced. It does not describe the system as it is today. For that, read the design documents under [`../design/`](../design/README.md); each section below names the document that carries its decisions.

**Origin:** Paperboy grew out of the display service of [SolarAI](https://github.com/maehld/solarai). ADRs 0012–0031 are copies from SolarAI under their original numbers, so that cross references stay valid; "SolarAI", `displayd` and "display service" in them mean the state of that time. [ADR 0030](0030-englisch-fuer-logs-kommentare-commits.md) and [0031](0031-everything-in-english.md) apply to both repositories. New Paperboy ADRs start at 0032 and are English ([ADR 0031](0031-everything-in-english.md)), while 0012–0030 stay German. The numbers 0021–0023 exist only in SolarAI; 0052 was never written.

## Origin and repository

Decisions about where the project lives and how it is organized. See [`01-system.md`](../design/01-system.md).

| Nr. | Title | Status |
|---|---|---|
| [0019](0019-epaper-aus-solarai-entfernen.md) | ePaper-Sender aus SolarAI entfernen | Akzeptiert |
| [0028](0028-paperboy-eigenes-repository.md) | Display-Service als „Paperboy" in eigenem Repository | Akzeptiert |
| [0029](0029-repository-trennung-umgesetzt.md) | Repository-Trennung SolarAI / Paperboy umgesetzt | Akzeptiert |
| [0030](0030-englisch-fuer-logs-kommentare-commits.md) | Englisch für Lognachrichten, Kommentare und Commits | Abgelöst durch [ADR-0031](0031-everything-in-english.md) |
| [0031](0031-everything-in-english.md) | Everything in the software is English | Accepted |

## Instance, configuration and files

Where the instance lives, how it is configured and how its files relate. See [`01-system.md`](../design/01-system.md).

| Nr. | Title | Status |
|---|---|---|
| [0026](0026-konfiguration-als-yaml.md) | Konfiguration als YAML mit Displayarten und Geräten | Akzeptiert, teilweise umgesetzt (Konfiguration wird noch nicht neu geladen); `directories` abgelöst durch [ADR-0037](0037-instance-directory-layout.md) |
| [0033](0033-global-language-and-timezone.md) | Global language and timezone in the configuration | Accepted |
| [0037](0037-instance-directory-layout.md) | One instance directory with a fixed layout | Accepted, implemented; `providers/` and `icons/` replaced by `plugins/` ([ADR-0049](0049-plugin-widgets.md)), devices moved to `devices/` by [ADR-0070](0070-devices-in-their-own-files.md), device state split per MAC by [ADR-0080](0080-device-state-per-mac.md) |
| [0047](0047-config-reload.md) | Reload the configuration on change | Accepted, implemented |
| [0070](0070-devices-in-their-own-files.md) | Devices in files of their own | Accepted, implemented; a missing display type no longer fails the load ([ADR-0079](0079-rename-config-items.md)) |
| [0071](0071-display-types-in-files-of-their-own.md) | Display types in files of their own | Accepted, implemented; renaming in the editor added by [ADR-0079](0079-rename-config-items.md) |
| [0079](0079-rename-config-items.md) | Renaming pages, plans, devices and display types | Accepted, implemented; supersedes the manual rename of [ADR-0071](0071-display-types-in-files-of-their-own.md) and the load failure of [ADR-0070](0070-devices-in-their-own-files.md); a device rename no longer moves state ([ADR-0080](0080-device-state-per-mac.md)) |
| [0139](0139-bound-the-page-caches.md) | Bound the in-memory page caches | Accepted, implemented; caps the rendered and notice pages and the refusal notes |

## Pages, widgets and plans

The content model: what a page is, what draws in it and when it is shown. See [`02-content.md`](../design/02-content.md).

| Nr. | Title | Status |
|---|---|---|
| [0013](0013-truetype-schrift.md) | TrueType-Schrift über golang.org/x/image | Akzeptiert |
| [0024](0024-layouts-im-display-service.md) | Daten statt Bilder – Datendokument und YAML-Layouts im Display-Service | Abgelöst durch [ADR-0049](0049-plugin-widgets.md) |
| [0025](0025-plaene-je-display.md) | Pläne – regelbasierte Seitenauswahl je Display | Akzeptiert, umgesetzt für TRMNL-Geräte; Zeitzone abgelöst durch [ADR-0033](0033-global-language-and-timezone.md), `rotate` durch [ADR-0130](0130-what-a-plan-refresh-does.md) |
| [0032](0032-data-providers-and-regions.md) | Data providers with named regions instead of image fragments | Superseded by [ADR-0049](0049-plugin-widgets.md) |
| [0034](0034-layout-primitives-and-typography.md) | Layout primitives and typography in page templates | Superseded by [ADR-0049](0049-plugin-widgets.md) |
| [0035](0035-duration-format.md) | Duration format and explicit charge direction for derived values | Superseded by [ADR-0049](0049-plugin-widgets.md) |
| [0036](0036-exec-providers.md) | Exec providers in the data directory | Superseded by [ADR-0049](0049-plugin-widgets.md) |
| [0038](0038-icons.md) | Icons from the instance directory | Superseded by [ADR-0049](0049-plugin-widgets.md) |
| [0046](0046-width-aware-value-formatting.md) | Width-aware value formatting for text displays | Superseded by [ADR-0049](0049-plugin-widgets.md) |
| [0049](0049-plugin-widgets.md) | Paperboy v2 – pages composed from plugin widgets | Accepted, implemented |
| [0050](0050-physical-scaling-by-display-ppi.md) | Scale widget layout by display PPI | Accepted, implemented |
| [0057](0057-plugin-actions-from-the-plan.md) | Plugin actions from the plan and the action menu | Superseded by [ADR-0161](0161-home-syncs-and-the-menu-goes.md) |
| [0067](0067-buttons-and-the-menu.md) | Buttons – home opens the menu, long presses page | Superseded by [ADR-0161](0161-home-syncs-and-the-menu-goes.md) |
| [0078](0078-the-menu-stays-open-while-it-is-used.md) | The menu stays open while it is used | Superseded by [ADR-0161](0161-home-syncs-and-the-menu-goes.md) |
| [0082](0082-menu-tap-controls-in-a-strip.md) | The menu's tap controls are their own strip | Superseded by [ADR-0161](0161-home-syncs-and-the-menu-goes.md) |
| [0092](0092-scrolling-text-lines.md) | Scrolling lines on text displays | Accepted, implemented |
| [0095](0095-the-logo-on-pages-and-devices.md) | The Paperboy logo on pages and on the device | Accepted, implemented |
| [0103](0103-plugin-conditions-in-plans.md) | Show a page while a plugin condition is active | Superseded by [ADR-0127](0127-plan-conditions-are-values.md); replaced the unimplemented `when.data` of [ADR-0025](0025-plaene-je-display.md) with conditions from plugins |
| [0110](0110-pages-serve-their-kind-and-name-variants.md) | Pages serve their kind and name their variants | Accepted, implemented; replaces the implicit variant/shared precedence of [ADR-0049](0049-plugin-widgets.md) and the file handling of [ADR-0068](0068-page-editor-ui.md), [ADR-0073](0073-edit-creates-and-deletes.md), [ADR-0079](0079-rename-config-items.md) (page part), [ADR-0093](0093-every-page-file-is-addressable.md) and [ADR-0094](0094-the-pages-list-is-a-list.md) with an explicit variant list; a page rename moves the variants ([ADR-0113](0113-a-page-rename-moves-its-variants.md)) |
| [0111](0111-text-pages-from-plugin-values.md) | Text pages from plugin values | Accepted, partly implemented (values, text boxes, overflow policies, editor and preview); plugin migrations open, lists (`for-each`) deferred to the first list plugin; builds on [ADR-0110](0110-pages-serve-their-kind-and-name-variants.md), replaces the text widget of [ADR-0049](0049-plugin-widgets.md) in stages; the text action is removed by [ADR-0125](0125-remove-the-text-widget-path.md) |
| [0112](0112-a-missing-page-file.md) | A missing page file is created by hand | Accepted, implemented; removes the assign dialog and `POST /api/pages/{name}/assign` of [ADR-0110](0110-pages-serve-their-kind-and-name-variants.md); a variant without a page file is read-only; a page rename moves its variants ([ADR-0113](0113-a-page-rename-moves-its-variants.md)) |
| [0113](0113-a-page-rename-moves-its-variants.md) | A page rename moves its variants | Accepted, implemented; amends the rename part of [ADR-0110](0110-pages-serve-their-kind-and-name-variants.md) and the orphan case of [ADR-0112](0112-a-missing-page-file.md) |
| [0115](0115-device-status-in-a-widget.md) | Device status in a widget | Accepted, implemented; adds `server_status` to the built-in plugin request and the widgets `battery` and `device-status`; point 5 (a header icon and percentage) superseded by [ADR-0121](0121-segmented-battery-gauge.md) |
| [0117](0117-text-cells-name-their-column.md) | Text cells name their column | Accepted, implemented; amends the row model of [ADR-0111](0111-text-pages-from-plugin-values.md) with a free column per cell, like the `at` of [ADR-0085](0085-moving-and-resizing-widgets.md) |
| [0118](0118-text-boxes-and-overflow.md) | Text boxes and overflow | Accepted, implemented; replaces the row model of [ADR-0117](0117-text-cells-name-their-column.md) with boxes in a character grid, adds strip/wrap/scroll per cell and literal text cells |
| [0120](0120-arguments-for-values.md) | Arguments for values | Accepted, implemented; extends the values of [ADR-0111](0111-text-pages-from-plugin-values.md) with arguments per value, like widgets have them ([ADR-0055](0055-allowed-arg-values-and-gallery-form.md)) |
| [0121](0121-segmented-battery-gauge.md) | A segmented gauge for the battery | Accepted, implemented; replaces the drawing and the sizes of the `battery` widget of [ADR-0115](0115-device-status-in-a-widget.md) and supersedes its point 5 (the header shows an icon and a percentage) |
| [0122](0122-hide-and-progress-a-unit.md) | Hide and progress a text value's unit | Accepted, implemented; extends the units of [ADR-0111](0111-text-pages-from-plugin-values.md) with `unit: none`, a declared progression and the cell key `scale` |
| [0155](0155-cgram-glyphs-on-hd44780-text-displays.md) | CGRAM glyphs on HD44780 text displays | Accepted, implemented; amends the `hd44780` charset of [ADR-0044](0044-text-displays-on-the-device-api.md) with five CGRAM glyphs |
| [0128](0128-a-default-grid-per-display-type.md) | A default grid per display type | Accepted, implemented; keeps the reference grid of [ADR-0049](0049-plugin-widgets.md) for declarations, adds `grid` to a display type as the start for new pages, and leaves the variant rule of [ADR-0110](0110-pages-serve-their-kind-and-name-variants.md) alone |
| [0130](0130-what-a-plan-refresh-does.md) | What a plan refresh does | Accepted, implemented; replaces `rotate` of [ADR-0025](0025-plaene-je-display.md) with `on_refresh`, removes `defaults.page_timeout` and its extra wake; a rule change always starts at the first page |
| [0131](0131-a-plan-rule-alerts-the-device.md) | A plan rule alerts the device | Accepted, implemented; adds `alert: once` and `alert: refresh` to a rule and `beep`/`blink` to the sync answer, with one script per method in each hardware type |
| [0138](0138-resolve-plans-outside-the-state-lock.md) | Resolve plans and pages outside the state lock | Accepted, implemented; moves plan loading, rule selection and page reads before the state lock |

## Devices and the device protocol

Registration, sync, state and what a device receives. See [`03-devices.md`](../design/03-devices.md).

| Nr. | Title | Status |
|---|---|---|
| [0017](0017-display-service-trmnl.md) | Eigener Display-Service mit TRMNL-Protokoll | Akzeptiert; TRMNL-Firmware und -Protokoll abgelöst durch [ADR-0041](0041-esphome-firmware.md) und [ADR-0042](0042-device-protocol.md) |
| [0020](0020-akkuwarnung-trmnl.md) | Akkuwarnung für TRMNL-Geräte | Akzeptiert |
| [0027](0027-textdisplays-und-http-geraete.md) | Textdisplays und einfache HTTP-Geräte | Akzeptiert; Textvorlagen umgesetzt mit [ADR-0044](0044-text-displays-on-the-device-api.md), `text-http` abgelöst durch [ADR-0044](0044-text-displays-on-the-device-api.md), `image-http` abgelöst durch [ADR-0042](0042-device-protocol.md) |
| [0039](0039-device-status.md) | Device status in the devices API | Accepted, implemented |
| [0042](0042-device-protocol.md) | Paperboy device protocol | Accepted, implemented |
| [0043](0043-firmware-distribution.md) | Firmware distribution through Paperboy | Accepted, implemented |
| [0044](0044-text-displays-on-the-device-api.md) | Text displays on the device API | Accepted, implemented (firmware not yet flashed); poll interval and 304 amended by [ADR-0045](0045-text-poll-interval-from-the-plan.md); text templates superseded by [ADR-0049](0049-plugin-widgets.md) |
| [0045](0045-text-poll-interval-from-the-plan.md) | Text display poll interval from the plan | Accepted, implemented |
| [0051](0051-power-source-refresh-policy.md) | Refresh policy by power source | Accepted, implemented (firmware not yet flashed) |
| [0064](0064-device-protocol-v4.md) | Device protocol v4 – event identities and explicit wake reasons | Accepted, implemented |
| [0072](0072-pages-as-qoi.md) | Pages as QOI for the firmware | Accepted, implemented |
| [0080](0080-device-state-per-mac.md) | Device state in a file per MAC | Accepted, implemented; supersedes the `devices.json` layout of [ADR-0037](0037-instance-directory-layout.md) and the state move of [ADR-0079](0079-rename-config-items.md) |
| [0083](0083-device-address.md) | The address of a device in its state | Accepted, implemented |
| [0088](0088-pages-that-do-not-fit-the-buffer.md) | Pages that do not fit the firmware's buffer | Accepted, implemented; amends the size limit of [ADR-0072](0072-pages-as-qoi.md) |
| [0097](0097-page-downloads-take-the-device-key.md) | Page downloads take the device key | Accepted, implemented; the dashboard reads the last image through a UI route ([ADR-0040](0040-operator-dashboard.md), [ADR-0096](0096-a-password-for-the-operator-ui.md)) |
| [0098](0098-the-device-shows-the-lockup-and-its-version.md) | The device shows the lockup and its firmware version | Accepted, implemented |
| [0102](0102-rotate-pages-onto-the-panel.md) | Rotate pages onto the panel | Accepted, implemented (firmware not yet flashed); amends the display types of [ADR-0071](0071-display-types-in-files-of-their-own.md), the device files of [ADR-0070](0070-devices-in-their-own-files.md) and the sync answer of [ADR-0042](0042-device-protocol.md) |
| [0109](0109-pair-a-display.md) | Pair a display instead of trusting its MAC address | Accepted, implemented (firmware not yet flashed); replaces the MAC rule of [ADR-0042](0042-device-protocol.md) and builds on the UI password of [ADR-0096](0096-a-password-for-the-operator-ui.md) |
| [0119](0119-remove-opendisplay.md) | Remove OpenDisplay from Paperboy | Accepted, implemented; removes `internal/opendisplay`, `cmd/odtest` and the device keys `address`/`buttons`; supersedes [ADR-0016](0016-opendisplay-bluetooth.md), the delivery of [ADR-0018](0018-opendisplay-im-display-service.md) and the rest of [ADR-0012](0012-epaper-opendisplay.md) |
| [0129](0129-align-timer-syncs.md) | Align timer syncs to the interval grid | Accepted, implemented; makes `refresh_s` the sleep until the next slot and keeps the caps of [ADR-0051](0051-power-source-refresh-policy.md) and the `wake` of [ADR-0064](0064-device-protocol-v4.md) untouched |
| [0132](0132-alert-methods-per-device.md) | Alert methods per device | Accepted, implemented; adds `alert_methods` to a device file and makes alerting opt-in: an omitted key stays silent, a list sets the `beep`/`blink` flags of [ADR-0131](0131-a-plan-rule-alerts-the-device.md) without a firmware change |
| [0133](0133-the-page-cursor-holds-the-page.md) | The page cursor holds the page | Accepted, implemented; adds `page_name` to the device state, so an edit of a plan keeps a device on its page and keeps the first-page rule of [ADR-0130](0130-what-a-plan-refresh-does.md) for a rule change |
| [0134](0134-readings-from-the-device.md) | Readings from the device | Accepted, implemented; adds the generic `readings` list to the sync body and the reading catalog, so the SHT4x of the Sticky, E1001 and E1003 reports the room on the dashboard; the plugin status and the page values follow with the widget ([ADR-0163](0163-the-readings-in-the-device-status-and-a-reading-widget.md)) |
| [0137](0137-bound-unclaimed-device-registration.md) | Bound unclaimed device registration | Accepted, implemented; caps the unclaimed state of [ADR-0080](0080-device-state-per-mac.md) |
| [0142](0142-a-manual-alert-waits-for-the-next-sync.md) | A manual alert waits for the next sync | Accepted, implemented; adds `pending_alert` to the device state and the `Signal` button on the dashboard, and reuses the `beep`/`blink` answer of [ADR-0131](0131-a-plan-rule-alerts-the-device.md) through the `alert_methods` of [ADR-0132](0132-alert-methods-per-device.md) without a firmware change |
| [0163](0163-the-readings-in-the-device-status-and-a-reading-widget.md) | The readings in the device status and a reading widget | Accepted, implemented; adds `readings` to `server_status` without a protocol version, the widget `paperboy/reading` and the values `paperboy/temperature` and `paperboy/humidity` of [ADR-0134](0134-readings-from-the-device.md), drawn with [ADR-0151](0151-a-shared-design-language-for-plugin-widgets.md) |

## Displays, panels and firmware

The hardware side: what a panel can do and how the firmware talks to it. See [`03-devices.md`](../design/03-devices.md).

| Nr. | Title | Status |
|---|---|---|
| [0012](0012-epaper-opendisplay.md) | ePaper-Anzeige über OpenDisplay im LAN | Teilweise abgelöst durch [ADR-0017](0017-display-service-trmnl.md) und [ADR-0018](0018-opendisplay-im-display-service.md); der Rest entfällt mit [ADR-0119](0119-remove-opendisplay.md) |
| [0014](0014-epaper-tastenbedienung.md) | Tastenbedienung der ePaper-Anzeigen | Teilweise abgelöst durch [ADR-0015](0015-tastenbedienung-per-schalter.md), [ADR-0017](0017-display-service-trmnl.md) und [ADR-0018](0018-opendisplay-im-display-service.md) |
| [0015](0015-tastenbedienung-per-schalter.md) | Tastenbedienung nur per Schalter | Teilweise abgelöst durch [ADR-0017](0017-display-service-trmnl.md) und [ADR-0018](0018-opendisplay-im-display-service.md) |
| [0016](0016-opendisplay-bluetooth.md) | OpenDisplay per Bluetooth über tinygo.org/x/bluetooth | Abgelöst durch [ADR-0119](0119-remove-opendisplay.md) |
| [0018](0018-opendisplay-im-display-service.md) | OpenDisplay im Display-Service | Abgelöst durch [ADR-0119](0119-remove-opendisplay.md) |
| [0041](0041-esphome-firmware.md) | Paperboy firmware built on ESPHome | Accepted, implemented (not yet flashed) |
| [0086](0086-four-gray-levels-on-the-e1001.md) | Four gray levels on the E1001 | Accepted, implemented; supersedes the dithered grays of [ADR-0041](0041-esphome-firmware.md) for that hardware; the panel settings moved to the device in [ADR-0087](0087-panel-settings-live-on-the-device.md) |
| [0087](0087-panel-settings-live-on-the-device.md) | Panel settings live on the device | Accepted, implemented; amends the substitution decision of [ADR-0086](0086-four-gray-levels-on-the-e1001.md) |
| [0105](0105-sixteen-gray-levels.md) | Sixteen gray levels on the panel | Accepted, implemented; replaces the `gray` bool of display types with `levels`, accepts `levels` 16 in the plugin protocol and keeps the 512 KiB QOI limit of [ADR-0088](0088-pages-that-do-not-fit-the-buffer.md), which follows the firmware buffer |
| [0108](0108-verify-the-tls-certificate.md) | Verify the TLS certificate in the firmware | Accepted, implemented (firmware not yet flashed); builds on the page key of [ADR-0097](0097-page-downloads-take-the-device-key.md) |

## Operator UI and editors

The web UI: what the operator sees and how content is edited. See [`04-operator-ui.md`](../design/04-operator-ui.md).

| Nr. | Title | Status |
|---|---|---|
| [0040](0040-operator-dashboard.md) | Operator dashboard as a Go template | Accepted, implemented |
| [0054](0054-plan-preview-in-the-web-ui.md) | Plan preview per device in the web UI | Accepted, implemented |
| [0066](0066-page-and-plan-api.md) | A page and plan API for an editor | Accepted, implemented |
| [0068](0068-page-editor-ui.md) | A page editor in the web UI | Accepted, implemented |
| [0069](0069-plan-editor-ui.md) | A plan editor in the web UI | Accepted, implemented |
| [0073](0073-edit-creates-and-deletes.md) | Creating plans and deleting files in the editor | Accepted, implemented |
| [0074](0074-editor-board-shows-the-preview.md) | The editor board shows the page preview | Accepted, implemented |
| [0076](0076-one-stylesheet-and-one-navigation.md) | One stylesheet and one navigation | Accepted, implemented |
| [0077](0077-the-widgets-page-has-no-gallery.md) | The widgets page has no gallery | Accepted, implemented |
| [0084](0084-the-overview-links.md) | The overview links, the editors act | Accepted, implemented |
| [0085](0085-moving-and-resizing-widgets.md) | Moving and resizing widgets with the pointer | Accepted, implemented |
| [0091](0091-virtual-display.md) | A virtual display in the web UI | Accepted, implemented |
| [0093](0093-every-page-file-is-addressable.md) | Every page file is addressable | Accepted, implemented; amends the Delete rule of [ADR-0084](0084-the-overview-links.md) and the overview of [ADR-0068](0068-page-editor-ui.md); the chip links and the Delete in the list superseded by [ADR-0094](0094-the-pages-list-is-a-list.md) |
| [0094](0094-the-pages-list-is-a-list.md) | The pages list is a list | Accepted, implemented |
| [0096](0096-a-password-for-the-operator-ui.md) | A password for the operator UI | Accepted, implemented; supersedes the "no token" decision of [ADR-0040](0040-operator-dashboard.md) and [ADR-0049](0049-plugin-widgets.md) for the operator UI; the device API keeps its key ([ADR-0042](0042-device-protocol.md)) |
| [0104](0104-runtime-stats-in-the-web-ui.md) | Runtime stats in the web UI | Accepted, implemented |
| [0106](0106-self-updating-pages-with-htmx.md) | Self-updating operator pages with htmx | Accepted, implemented; replaces the `<meta http-equiv="refresh">` of [ADR-0040](0040-operator-dashboard.md) for the dashboard, the stats page and the downloads page |
| [0116](0116-a-pixel-font-for-text-previews.md) | A pixel font for text previews | Accepted, implemented; builds on the board of [ADR-0074](0074-editor-board-shows-the-preview.md), the one stylesheet of [ADR-0076](0076-one-stylesheet-and-one-navigation.md), the virtual display of [ADR-0091](0091-virtual-display.md) and the text pages of [ADR-0111](0111-text-pages-from-plugin-values.md) |
| [0123](0123-a-sample-device-in-previews.md) | A sample device in previews | Accepted, implemented; builds on the device status of [ADR-0115](0115-device-status-in-a-widget.md) and adds a computed `enum` to the argument values of [ADR-0055](0055-allowed-arg-values-and-gallery-form.md) |
| [0124](0124-the-playground-shows-image-display-types.md) | The Playground shows image display types only | Accepted, implemented; supersedes the sentence about the LCD block in [ADR-0074](0074-editor-board-shows-the-preview.md); the `text` action it left in the protocol is removed by [ADR-0125](0125-remove-the-text-widget-path.md) |
| [0125](0125-remove-the-text-widget-path.md) | Remove the text widget path | Accepted, implemented; removes the text action, `Texter`, `Result.lines`, `WidgetRequest.scroll` and the `text` widget kind in protocol 8, and the built-in `Text` method; supersedes the "the text action stays" rule of [ADR-0111](0111-text-pages-from-plugin-values.md) and the widget part of [ADR-0092](0092-scrolling-text-lines.md) |
| [0136](0136-harden-the-operator-ui.md) | Harden the operator UI against framing and DNS rebinding | Accepted, implemented; amends the same-origin check of [ADR-0096](0096-a-password-for-the-operator-ui.md) with a host allowlist and response headers |
| [0140](0140-an-explicit-devices-api-answer.md) | An explicit answer for the devices API | Accepted, implemented; replaces the embedded device state with a fixed field set |
| [0152](0152-a-design-system-and-a-rail-for-the-operator-ui.md) | A design system and a rail for the operator UI | Accepted; amends the one-file rule and the flat navigation of [ADR-0076](0076-one-stylesheet-and-one-navigation.md), the variant flow follows in issue #384, the shell amended by [ADR-0156](0156-a-title-line-for-the-operator-shell.md), the recent widgets dropped by [ADR-0157](0157-drop-the-recent-widgets.md) |
| [0153](0153-an-overview-page-and-a-grouped-rail.md) | An overview page and a grouped rail | Accepted, implemented; replaces the first rail entry and the sub-navigation of [ADR-0152](0152-a-design-system-and-a-rail-for-the-operator-ui.md) |
| [0154](0154-split-the-release-trains-and-add-maintenance.md) | Split the release trains and add a maintenance page | Accepted, implemented; moves the cleanup of [ADR-0090](0090-download-cache-and-inventory.md) to its own page and adds the state and backup cleanup |
| [0156](0156-a-title-line-for-the-operator-shell.md) | A title line for the operator shell | Accepted, implemented; amends the top bar, the editor action bars and the list heads of [ADR-0152](0152-a-design-system-and-a-rail-for-the-operator-ui.md) |
| [0157](0157-drop-the-recent-widgets.md) | Drop the recent widgets from the palette | Accepted, implemented; keeps the pinned widgets of [ADR-0152](0152-a-design-system-and-a-rail-for-the-operator-ui.md) and removes the recent ones |
| [0158](0158-plans-are-pages-refresh-windows-and-triggers.md) | A plan is pages, refresh windows and triggers | Accepted, implemented; supersedes [ADR-0025](0025-plaene-je-display.md) and amends the conditions of [ADR-0127](0127-plan-conditions-are-values.md), the refresh of [ADR-0130](0130-what-a-plan-refresh-does.md), the alert of [ADR-0131](0131-a-plan-rule-alerts-the-device.md) and the cursor of [ADR-0133](0133-the-page-cursor-holds-the-page.md) |
| [0161](0161-home-syncs-and-the-menu-goes.md) | Home syncs the page and the device menu goes | Accepted, implemented; supersedes [ADR-0057](0057-plugin-actions-from-the-plan.md), [ADR-0067](0067-buttons-and-the-menu.md), [ADR-0078](0078-the-menu-stays-open-while-it-is-used.md) and [ADR-0082](0082-menu-tap-controls-in-a-strip.md) |
| [0162](0162-widget-actions-and-the-plugins-choices.md) | A widget action and the plugin's choices | Accepted, implemented; arguments, title and inheritance amended by [ADR-0164](0164-a-widget-action-carries-its-own-arguments.md), point 8 and the option request amended by [ADR-0165](0165-a-plugin-describes-its-action-options.md) |
| [0163](0163-the-action-mode-works-with-the-keys.md) | The action mode works with the keys | Accepted, implemented; amends the long `home` of [ADR-0161](0161-home-syncs-and-the-menu-goes.md) and the touch requirement and key map of [ADR-0162](0162-widget-actions-and-the-plugins-choices.md) |
| [0164](0164-a-widget-action-carries-its-own-arguments.md) | A widget action carries its own arguments | Accepted, implemented; amends the arguments, title and inheritance of [ADR-0162](0162-widget-actions-and-the-plugins-choices.md) |
| [0165](0165-a-plugin-describes-its-action-options.md) | A plugin describes its action options | Accepted, implemented; protocol 10 adds `action_options` and the `active` flag, amends [ADR-0162](0162-widget-actions-and-the-plugins-choices.md) and builds on [ADR-0164](0164-a-widget-action-carries-its-own-arguments.md) |
| [0167](0167-a-header-band-outside-the-page-grid.md) | A header band outside the page grid | Accepted, implemented; amends the grid of [ADR-0049](0049-plugin-widgets.md), adds `paperboy/title` and `format: auto` to `paperboy/clock`, and keeps `paperboy/header` for existing pages; its three zones are superseded by [ADR-0168](0168-a-header-band-per-display-type.md) |
| [0168](0168-a-header-band-per-display-type.md) | A header band per display type | Accepted, implemented; replaces the zones and the free part list of [ADR-0167](0167-a-header-band-outside-the-page-grid.md) with one fixed band in the display type, switches for the page indicator, readings, clock and battery, `header: false` on a page, and removes the `paperboy/header` and `paperboy/title` widgets |

## Plugins

How plugins are written, hosted and versioned. See [`05-plugins.md`](../design/05-plugins.md).

| Nr. | Title | Status |
|---|---|---|
| [0048](0048-calendar-provider-and-agenda.md) | Calendar provider over CalDAV and the agenda block | Accepted; provider and agenda block replaced by the calendar plugin ([ADR-0049](0049-plugin-widgets.md)); not yet run against iCloud |
| [0053](0053-departures-from-efa.md) | Real-time departures from an EFA installation | Accepted, implemented |
| [0055](0055-allowed-arg-values-and-gallery-form.md) | Allowed argument values and the gallery form | Accepted, implemented |
| [0058](0058-plugin-protocol-version-check.md) | Check the plugin protocol version at runtime | Accepted, implemented; the exact match became a floor in [ADR-0081](0081-plugin-state-over-the-protocol.md) |
| [0065](0065-plugin-state-directory.md) | A state directory per plugin | Superseded by [ADR-0081](0081-plugin-state-over-the-protocol.md) |
| [0075](0075-plugin-versions.md) | A version per plugin | Accepted, implemented |
| [0081](0081-plugin-state-over-the-protocol.md) | Plugin state and blobs over the protocol | Accepted, implemented; supersedes [ADR-0065](0065-plugin-state-directory.md), amends the exact version match of [ADR-0058](0058-plugin-protocol-version-check.md) (newer versions are accepted) |
| [0099](0099-glucose-readings-from-the-dexcom-share-service.md) | Glucose readings from the Dexcom Share service | Accepted, implemented |
| [0100](0100-one-stop-per-departures-widget.md) | One stop per departures widget | Accepted, implemented; replaces the single `stop` of [ADR-0053](0053-departures-from-efa.md) |
| [0126](0126-a-schedule-event-for-plugin-demand.md) | A schedule event for plugin demand | Accepted; protocol 9, the `Demand` helper, the host demand and the cache gate implemented; the plugin migrations follow |
| [0127](0127-plan-conditions-are-values.md) | Plan conditions are values | Accepted, implemented; supersedes the conditions of [ADR-0103](0103-plugin-conditions-in-plans.md), adds the `enum` value type to [ADR-0111](0111-text-pages-from-plugin-values.md) and the per-cell labels to those values; amends protocol 9 before it ships |
| [0135](0135-a-minimal-environment-for-plugins.md) | A minimal environment for plugins | Accepted, implemented |
| [0141](0141-keep-caldav-credentials-on-their-hosts.md) | Keep CalDAV credentials on their hosts | Accepted, implemented; limits the redirect handling of [ADR-0048](0048-calendar-provider-and-agenda.md) |
| [0143](0143-a-fuel-price-plugin-with-its-own-history.md) | A fuel price plugin with its own history | Accepted, implemented |
| [0144](0144-a-github-plugin-for-repository-status.md) | A GitHub plugin for repository status | Accepted, implemented |
| [0145](0145-a-period-and-a-balance-in-the-evcc-plugin.md) | A period and a balance in the evcc plugin | Accepted, implemented; adds `period` to the flow and the `balance` widget, and draws a link only while power or energy flows |
| [0149](0149-a-prusalink-plugin-for-printer-status.md) | A PrusaLink plugin for 3D printer status | Accepted, implemented |
| [0151](0151-a-shared-design-language-for-plugin-widgets.md) | A shared design language for plugin widgets | Accepted, implemented; adds `sdk/design` with the page-wide type scale and the tile, hero, trend, sparkline, meter, arrow, muted and notice components |
| [0152](0152-a-tiny-role-for-dense-lists.md) | A tiny role for dense lists | Accepted, implemented; adds `Tiny` at 0.55 of `text_px` to the scale of [ADR-0151](0151-a-shared-design-language-for-plugin-widgets.md) |
| [0156](0156-mark-the-extrema-in-a-trend-chart.md) | Mark the extrema in a trend chart | Accepted, implemented; adds labelled marks and the extrema helper to the trend of [ADR-0151](0151-a-shared-design-language-for-plugin-widgets.md), the label placement amended by [ADR-0160](0160-a-trend-chart-reads-in-a-short-cell.md) |
| [0159](0159-show-an-offline-printer-in-the-prusalink-widgets.md) | Show an offline printer in the prusalink widgets | Accepted, implemented; adds the `offline` state key and amends the failure handling of [ADR-0149](0149-a-prusalink-plugin-for-printer-status.md) |
| [0160](0160-a-trend-chart-reads-in-a-short-cell.md) | A trend chart reads in a short cell | Accepted, implemented; caps the title and axis of the trend of [ADR-0151](0151-a-shared-design-language-for-plugin-widgets.md) by the cell height and amends the mark labels of [ADR-0156](0156-mark-the-extrema-in-a-trend-chart.md) |
| [0166](0166-a-shared-battery-gauge.md) | A shared battery gauge | Accepted, implemented; adds `DrawBattery` to [ADR-0151](0151-a-shared-design-language-for-plugin-widgets.md) and uses it for the gauge of [ADR-0121](0121-segmented-battery-gauge.md) and the evcc flow |

## Delivery and releases

Release trains, downloads and what an instance installs. See [`06-delivery.md`](../design/06-delivery.md).

| Nr. | Title | Status |
|---|---|---|
| [0056](0056-repository-layout-and-release-trains.md) | One repository with separate release trains | Accepted, implemented; firmware assets amended by [ADR-0059](0059-firmware-bundle.md), the one-module layout replaced by [ADR-0146](0146-server-plugins-and-firmware-in-their-own-directories.md), the tag table replaced by [ADR-0147](0147-separate-server-plugin-and-firmware-releases.md) |
| [0146](0146-server-plugins-and-firmware-in-their-own-directories.md) | Server, plugins and firmware in their own directories | Accepted, implemented; replaces the one-module layout of [ADR-0056](0056-repository-layout-and-release-trains.md) |
| [0147](0147-separate-server-plugin-and-firmware-releases.md) | Separate server, plugin and firmware releases | Accepted, implemented; replaces the tag table of [ADR-0056](0056-repository-layout-and-release-trains.md), the Docker part amended by [ADR-0148](0148-the-container-image-is-the-primary-server-artifact.md) |
| [0148](0148-the-container-image-is-the-primary-server-artifact.md) | The container image is the primary server artifact | Accepted, implemented; amends the Docker part of [ADR-0147](0147-separate-server-plugin-and-firmware-releases.md), the badge rule amended by [ADR-0149](0149-the-newest-final-server-release-is-githubs-latest.md) |
| [0149](0149-the-newest-final-server-release-is-githubs-latest.md) | The newest final server release is GitHub's latest | Accepted, implemented; amends the badge rule of [ADR-0148](0148-the-container-image-is-the-primary-server-artifact.md) |
| [0150](0150-start-a-release-from-the-actions-ui.md) | Start a release from the Actions UI | Accepted, implemented; starts a release from the release train of [ADR-0147](0147-separate-server-plugin-and-firmware-releases.md) |
| [0059](0059-firmware-bundle.md) | One firmware bundle per release | Accepted, implemented; bundle name amended by [ADR-0063](0063-constant-firmware-bundle-name.md) |
| [0060](0060-download-from-releases.md) | Downloading plugins and firmware from releases | Accepted, implemented; which firmware images are installed amended by [ADR-0061](0061-install-every-bundle-image.md), plugin updates, interval and on-demand check by [ADR-0062](0062-plugin-updates-and-download-control.md), bundle asset name by [ADR-0063](0063-constant-firmware-bundle-name.md) |
| [0061](0061-install-every-bundle-image.md) | Install every image of a firmware bundle | Accepted, implemented |
| [0062](0062-plugin-updates-and-download-control.md) | Newer plugin downloads, a configurable interval and a check on demand | Accepted, implemented |
| [0063](0063-constant-firmware-bundle-name.md) | A constant name for the firmware bundle | Accepted, implemented |
| [0089](0089-release-channels.md) | Release channels and prerelease versions | Accepted, implemented; the cache layout and the ownership of `state/download` follow [ADR-0090](0090-download-cache-and-inventory.md) |
| [0090](0090-download-cache-and-inventory.md) | The download cache keeps one file, the inventory is pruned by hand | Accepted, implemented |
| [0101](0101-a-hint-about-a-newer-paperboy.md) | A hint about a newer Paperboy | Accepted, implemented; amends the release check of [ADR-0060](0060-download-from-releases.md) and [ADR-0062](0062-plugin-updates-and-download-control.md) (it names a release, it installs nothing) |
| [0107](0107-plugin-builds-reach-installations.md) | Plugin builds reach installations | Accepted, implemented; amends the version rule of [ADR-0075](0075-plugin-versions.md) (shared `pkg/plugin` and `pkg/render` raise every plugin version) and makes the `levels` value of [ADR-0105](0105-sixteen-gray-levels.md) tolerant |
