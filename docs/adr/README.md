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

## Pages, widgets and plans

The content model: what a page is, what draws in it and when it is shown. See [`02-content.md`](../design/02-content.md).

| Nr. | Title | Status |
|---|---|---|
| [0013](0013-truetype-schrift.md) | TrueType-Schrift über golang.org/x/image | Akzeptiert |
| [0024](0024-layouts-im-display-service.md) | Daten statt Bilder – Datendokument und YAML-Layouts im Display-Service | Abgelöst durch [ADR-0049](0049-plugin-widgets.md) |
| [0025](0025-plaene-je-display.md) | Pläne – regelbasierte Seitenauswahl je Display | Akzeptiert, umgesetzt für TRMNL-Geräte; Zeitzone abgelöst durch [ADR-0033](0033-global-language-and-timezone.md) |
| [0032](0032-data-providers-and-regions.md) | Data providers with named regions instead of image fragments | Superseded by [ADR-0049](0049-plugin-widgets.md) |
| [0034](0034-layout-primitives-and-typography.md) | Layout primitives and typography in page templates | Superseded by [ADR-0049](0049-plugin-widgets.md) |
| [0035](0035-duration-format.md) | Duration format and explicit charge direction for derived values | Superseded by [ADR-0049](0049-plugin-widgets.md) |
| [0036](0036-exec-providers.md) | Exec providers in the data directory | Superseded by [ADR-0049](0049-plugin-widgets.md) |
| [0038](0038-icons.md) | Icons from the instance directory | Superseded by [ADR-0049](0049-plugin-widgets.md) |
| [0046](0046-width-aware-value-formatting.md) | Width-aware value formatting for text displays | Superseded by [ADR-0049](0049-plugin-widgets.md) |
| [0049](0049-plugin-widgets.md) | Paperboy v2 – pages composed from plugin widgets | Accepted, implemented |
| [0050](0050-physical-scaling-by-display-ppi.md) | Scale widget layout by display PPI | Accepted, implemented |
| [0057](0057-plugin-actions-from-the-plan.md) | Plugin actions from the plan and the action menu | Accepted, implemented |
| [0067](0067-buttons-and-the-menu.md) | Buttons – home opens the menu, long presses page | Accepted, implemented |
| [0078](0078-the-menu-stays-open-while-it-is-used.md) | The menu stays open while it is used | Accepted, implemented |
| [0082](0082-menu-tap-controls-in-a-strip.md) | The menu's tap controls are their own strip | Accepted, implemented |
| [0092](0092-scrolling-text-lines.md) | Scrolling lines on text displays | Accepted, implemented |
| [0095](0095-the-logo-on-pages-and-devices.md) | The Paperboy logo on pages and on the device | Accepted, implemented |
| [0103](0103-plugin-conditions-in-plans.md) | Show a page while a plugin condition is active | Accepted, implemented; replaces the unimplemented `when.data` of [ADR-0025](0025-plaene-je-display.md) with conditions from plugins |
| [0110](0110-pages-name-their-display-types.md) | Pages name their display types | Proposed; on acceptance supersedes the file resolution of [ADR-0049](0049-plugin-widgets.md), the editor addressing and the set of files of [ADR-0068](0068-page-editor-ui.md), [ADR-0073](0073-edit-creates-and-deletes.md), [ADR-0079](0079-rename-config-items.md), [ADR-0093](0093-every-page-file-is-addressable.md) and [ADR-0094](0094-the-pages-list-is-a-list.md) |
| [0111](0111-text-pages-from-plugin-values.md) | Text pages from plugin values | Proposed; builds on [ADR-0110](0110-pages-name-their-display-types.md), replaces the text widget of [ADR-0049](0049-plugin-widgets.md) in stages |

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

## Displays, panels and firmware

The hardware side: what a panel can do and how the firmware talks to it. See [`03-devices.md`](../design/03-devices.md).

| Nr. | Title | Status |
|---|---|---|
| [0012](0012-epaper-opendisplay.md) | ePaper-Anzeige über OpenDisplay im LAN | Teilweise abgelöst durch [ADR-0017](0017-display-service-trmnl.md) und [ADR-0018](0018-opendisplay-im-display-service.md) |
| [0014](0014-epaper-tastenbedienung.md) | Tastenbedienung der ePaper-Anzeigen | Teilweise abgelöst durch [ADR-0015](0015-tastenbedienung-per-schalter.md), [ADR-0017](0017-display-service-trmnl.md) und [ADR-0018](0018-opendisplay-im-display-service.md) |
| [0015](0015-tastenbedienung-per-schalter.md) | Tastenbedienung nur per Schalter | Teilweise abgelöst durch [ADR-0017](0017-display-service-trmnl.md) und [ADR-0018](0018-opendisplay-im-display-service.md) |
| [0016](0016-opendisplay-bluetooth.md) | OpenDisplay per Bluetooth über tinygo.org/x/bluetooth | Akzeptiert |
| [0018](0018-opendisplay-im-display-service.md) | OpenDisplay im Display-Service | Teilweise abgelöst durch [ADR-0019](0019-epaper-aus-solarai-entfernen.md) |
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

## Delivery and releases

Release trains, downloads and what an instance installs. See [`06-delivery.md`](../design/06-delivery.md).

| Nr. | Title | Status |
|---|---|---|
| [0056](0056-repository-layout-and-release-trains.md) | One repository with separate release trains | Accepted, implemented; firmware assets amended by [ADR-0059](0059-firmware-bundle.md) |
| [0059](0059-firmware-bundle.md) | One firmware bundle per release | Accepted, implemented; bundle name amended by [ADR-0063](0063-constant-firmware-bundle-name.md) |
| [0060](0060-download-from-releases.md) | Downloading plugins and firmware from releases | Accepted, implemented; which firmware images are installed amended by [ADR-0061](0061-install-every-bundle-image.md), plugin updates, interval and on-demand check by [ADR-0062](0062-plugin-updates-and-download-control.md), bundle asset name by [ADR-0063](0063-constant-firmware-bundle-name.md) |
| [0061](0061-install-every-bundle-image.md) | Install every image of a firmware bundle | Accepted, implemented |
| [0062](0062-plugin-updates-and-download-control.md) | Newer plugin downloads, a configurable interval and a check on demand | Accepted, implemented |
| [0063](0063-constant-firmware-bundle-name.md) | A constant name for the firmware bundle | Accepted, implemented |
| [0089](0089-release-channels.md) | Release channels and prerelease versions | Accepted, implemented; the cache layout and the ownership of `state/download` follow [ADR-0090](0090-download-cache-and-inventory.md) |
| [0090](0090-download-cache-and-inventory.md) | The download cache keeps one file, the inventory is pruned by hand | Accepted, implemented |
| [0101](0101-a-hint-about-a-newer-paperboy.md) | A hint about a newer Paperboy | Accepted, implemented; amends the release check of [ADR-0060](0060-download-from-releases.md) and [ADR-0062](0062-plugin-updates-and-download-control.md) (it names a release, it installs nothing) |
| [0107](0107-plugin-builds-reach-installations.md) | Plugin builds reach installations | Accepted, implemented; amends the version rule of [ADR-0075](0075-plugin-versions.md) (shared `pkg/plugin` and `pkg/render` raise every plugin version) and makes the `levels` value of [ADR-0105](0105-sixteen-gray-levels.md) tolerant |
