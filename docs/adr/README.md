# Architecture Decision Records

Wichtige Entscheidungen werden hier als kurze ADRs festgehalten (Format nach Michael Nygard, auf Deutsch).

- Dateiname: `NNNN-kurzer-titel.md`, fortlaufend nummeriert.
- Status: `Vorgeschlagen`, `Akzeptiert`, `Abgelöst durch ADR-NNNN` oder `Verworfen`.
- Bestehende ADRs nicht umschreiben; geänderte Entscheidungen bekommen ein neues ADR, das alte wird als abgelöst markiert.
- Vorlage: [`0000-vorlage.md`](0000-vorlage.md)

**Herkunft:** Paperboy ist aus dem Display-Service von [SolarAI](https://github.com/maehld/solarai) hervorgegangen. Die ADRs 0012–0031 sind Kopien aus SolarAI mit ihren ursprünglichen Nummern, damit Querverweise gültig bleiben; „SolarAI“, `displayd` und „Display-Service“ darin meinen den damaligen Stand. Fehlende Nummern betreffen nur SolarAI und stehen dort. ADR 0030 und 0031 gelten für beide Repositories; neue Paperboy-ADRs beginnen bei 0032 und sind englisch (ADR 0031).

| Nr. | Titel | Status |
|---|---|---|
| [0012](0012-epaper-opendisplay.md) | ePaper-Anzeige über OpenDisplay im LAN | Teilweise abgelöst durch ADR-0017 und ADR-0018 |
| [0013](0013-truetype-schrift.md) | TrueType-Schrift über golang.org/x/image | Akzeptiert |
| [0014](0014-epaper-tastenbedienung.md) | Tastenbedienung der ePaper-Anzeigen | Teilweise abgelöst durch ADR-0015, ADR-0017 und ADR-0018 |
| [0015](0015-tastenbedienung-per-schalter.md) | Tastenbedienung nur per Schalter | Teilweise abgelöst durch ADR-0017 und ADR-0018 |
| [0016](0016-opendisplay-bluetooth.md) | OpenDisplay per Bluetooth über tinygo.org/x/bluetooth | Akzeptiert |
| [0017](0017-display-service-trmnl.md) | Eigener Display-Service mit TRMNL-Protokoll | Akzeptiert; TRMNL-Firmware und -Protokoll abgelöst durch ADR-0041 und ADR-0042 |
| [0018](0018-opendisplay-im-display-service.md) | OpenDisplay im Display-Service | Teilweise abgelöst durch ADR-0019 |
| [0019](0019-epaper-aus-solarai-entfernen.md) | ePaper-Sender aus SolarAI entfernen | Akzeptiert |
| [0020](0020-akkuwarnung-trmnl.md) | Akkuwarnung für TRMNL-Geräte | Akzeptiert |
| [0024](0024-layouts-im-display-service.md) | Daten statt Bilder – Datendokument und YAML-Layouts im Display-Service | Abgelöst durch ADR-0049 |
| [0025](0025-plaene-je-display.md) | Pläne – regelbasierte Seitenauswahl je Display | Akzeptiert, umgesetzt für TRMNL-Geräte; Zeitzone abgelöst durch ADR-0033 |
| [0026](0026-konfiguration-als-yaml.md) | Konfiguration als YAML mit Displayarten und Geräten | Akzeptiert, teilweise umgesetzt (Konfiguration wird noch nicht neu geladen); `directories` abgelöst durch ADR-0037 |
| [0027](0027-textdisplays-und-http-geraete.md) | Textdisplays und einfache HTTP-Geräte | Akzeptiert; Textvorlagen umgesetzt mit ADR-0044, `text-http` abgelöst durch ADR-0044, `image-http` abgelöst durch ADR-0042 |
| [0028](0028-paperboy-eigenes-repository.md) | Display-Service als „Paperboy“ in eigenem Repository | Akzeptiert |
| [0029](0029-repository-trennung-umgesetzt.md) | Repository-Trennung SolarAI / Paperboy umgesetzt | Akzeptiert |
| [0030](0030-englisch-fuer-logs-kommentare-commits.md) | Englisch für Lognachrichten, Kommentare und Commits | Abgelöst durch ADR-0031 |
| [0031](0031-everything-in-english.md) | Everything in the software is English | Accepted |
| [0032](0032-data-providers-and-regions.md) | Data providers with named regions instead of image fragments | Superseded by ADR-0049 |
| [0033](0033-global-language-and-timezone.md) | Global language and timezone in the configuration | Accepted |
| [0034](0034-layout-primitives-and-typography.md) | Layout primitives and typography in page templates | Superseded by ADR-0049 |
| [0035](0035-duration-format.md) | Duration format and explicit charge direction for derived values | Superseded by ADR-0049 |
| [0036](0036-exec-providers.md) | Exec providers in the data directory | Superseded by ADR-0049 |
| [0037](0037-instance-directory-layout.md) | One instance directory with a fixed layout | Accepted, implemented; `providers/` and `icons/` replaced by `plugins/` (ADR-0049) |
| [0038](0038-icons.md) | Icons from the instance directory | Superseded by ADR-0049 |
| [0039](0039-device-status.md) | Device status in the devices API | Accepted, implemented |
| [0040](0040-operator-dashboard.md) | Operator dashboard as a Go template | Accepted, implemented |
| [0041](0041-esphome-firmware.md) | Paperboy firmware built on ESPHome | Accepted, implemented (not yet flashed) |
| [0042](0042-device-protocol.md) | Paperboy device protocol | Accepted, implemented |
| [0043](0043-firmware-distribution.md) | Firmware distribution through Paperboy | Accepted, implemented |
| [0044](0044-text-displays-on-the-device-api.md) | Text displays on the device API | Accepted, implemented (firmware not yet flashed); poll interval and 304 amended by ADR-0045; text templates superseded by ADR-0049 |
| [0045](0045-text-poll-interval-from-the-plan.md) | Text display poll interval from the plan | Accepted, implemented |
| [0046](0046-width-aware-value-formatting.md) | Width-aware value formatting for text displays | Superseded by ADR-0049 |
| [0047](0047-config-reload.md) | Reload the configuration on change | Accepted, implemented |
| [0048](0048-calendar-provider-and-agenda.md) | Calendar provider over CalDAV and the agenda block | Accepted; provider and agenda block replaced by the calendar plugin (ADR-0049); not yet run against iCloud |
| [0049](0049-plugin-widgets.md) | Paperboy v2 – pages composed from plugin widgets | Accepted, implemented |
| [0050](0050-physical-scaling-by-display-ppi.md) | Scale widget layout by display PPI | Accepted, implemented |
| [0051](0051-power-source-refresh-policy.md) | Refresh policy by power source | Accepted, implemented (firmware not yet flashed) |
| [0053](0053-departures-from-efa.md) | Real-time departures from an EFA installation | Accepted, implemented |
| [0054](0054-plan-preview-in-the-web-ui.md) | Plan preview per device in the web UI | Accepted, implemented |
| [0055](0055-allowed-arg-values-and-gallery-form.md) | Allowed argument values and the gallery form | Accepted, implemented |
| [0056](0056-repository-layout-and-release-trains.md) | One repository with separate release trains | Accepted, implemented; firmware assets amended by ADR-0059 |
| [0057](0057-plugin-actions-from-the-plan.md) | Plugin actions from the plan and the action menu | Proposed |
| [0058](0058-plugin-protocol-version-check.md) | Check the plugin protocol version at runtime | Accepted, implemented |
| [0059](0059-firmware-bundle.md) | One firmware bundle per release | Accepted, implemented; bundle name amended by ADR-0063 |
| [0060](0060-download-from-releases.md) | Downloading plugins and firmware from releases | Accepted, implemented; which firmware images are installed amended by ADR-0061, plugin updates, interval and on-demand check by ADR-0062, bundle asset name by ADR-0063 |
| [0061](0061-install-every-bundle-image.md) | Install every image of a firmware bundle | Accepted, implemented |
| [0062](0062-plugin-updates-and-download-control.md) | Newer plugin downloads, a configurable interval and a check on demand | Accepted, implemented |
| [0063](0063-constant-firmware-bundle-name.md) | A constant name for the firmware bundle | Accepted, implemented |
