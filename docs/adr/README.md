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
| [0017](0017-display-service-trmnl.md) | Eigener Display-Service mit TRMNL-Protokoll | Akzeptiert |
| [0018](0018-opendisplay-im-display-service.md) | OpenDisplay im Display-Service | Teilweise abgelöst durch ADR-0019 |
| [0019](0019-epaper-aus-solarai-entfernen.md) | ePaper-Sender aus SolarAI entfernen | Akzeptiert |
| [0020](0020-akkuwarnung-trmnl.md) | Akkuwarnung für TRMNL-Geräte | Akzeptiert |
| [0024](0024-layouts-im-display-service.md) | Daten statt Bilder – Datendokument und YAML-Layouts im Display-Service | Akzeptiert, teilweise umgesetzt; Sprache/Formatierung teilweise abgelöst durch ADR-0033, Ablageort abgelöst durch ADR-0037 |
| [0025](0025-plaene-je-display.md) | Pläne – regelbasierte Seitenauswahl je Display | Akzeptiert, umgesetzt für TRMNL-Geräte; Zeitzone abgelöst durch ADR-0033 |
| [0026](0026-konfiguration-als-yaml.md) | Konfiguration als YAML mit Displayarten und Geräten | Akzeptiert, teilweise umgesetzt (Konfiguration wird noch nicht neu geladen); `directories` abgelöst durch ADR-0037 |
| [0027](0027-textdisplays-und-http-geraete.md) | Textdisplays und einfache HTTP-Geräte | Akzeptiert, Umsetzung ausstehend |
| [0028](0028-paperboy-eigenes-repository.md) | Display-Service als „Paperboy“ in eigenem Repository | Akzeptiert |
| [0029](0029-repository-trennung-umgesetzt.md) | Repository-Trennung SolarAI / Paperboy umgesetzt | Akzeptiert |
| [0030](0030-englisch-fuer-logs-kommentare-commits.md) | Englisch für Lognachrichten, Kommentare und Commits | Abgelöst durch ADR-0031 |
| [0031](0031-everything-in-english.md) | Everything in the software is English | Accepted |
| [0032](0032-data-providers-and-regions.md) | Data providers with named regions instead of image fragments | Accepted |
| [0033](0033-global-language-and-timezone.md) | Global language and timezone in the configuration | Accepted |
| [0034](0034-layout-primitives-and-typography.md) | Layout primitives and typography in page templates | Accepted, implemented |
| [0035](0035-duration-format.md) | Duration format and explicit charge direction for derived values | Accepted, implemented |
| [0036](0036-exec-providers.md) | Exec providers in the data directory | Accepted, implemented; providers directory moved by ADR-0037 |
| [0037](0037-instance-directory-layout.md) | One instance directory with a fixed layout | Accepted, implemented |
| [0038](0038-icons.md) | Icons from the instance directory | Accepted, implemented |
| [0039](0039-device-status.md) | Device status in the devices API | Accepted, implemented |
| [0040](0040-operator-dashboard.md) | Operator dashboard as a Go template | Accepted, implemented |
