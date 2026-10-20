# Architecture Decision Records

Wichtige Entscheidungen werden hier als kurze ADRs festgehalten (Format nach Michael Nygard, auf Deutsch).

- Dateiname: `NNNN-kurzer-titel.md`, fortlaufend nummeriert.
- Status: `Vorgeschlagen`, `Akzeptiert`, `Abgelöst durch ADR-NNNN` oder `Verworfen`.
- Bestehende ADRs nicht umschreiben; geänderte Entscheidungen bekommen ein neues ADR, das alte wird als abgelöst markiert.
- Vorlage: [`0000-vorlage.md`](0000-vorlage.md)

**Herkunft:** Paperboy ist aus dem Display-Service von [SolarAI](https://github.com/maehld/solarai) hervorgegangen. Die ADRs 0012–0029 sind Kopien aus SolarAI mit ihren ursprünglichen Nummern, damit Querverweise gültig bleiben; „SolarAI“, `displayd` und „Display-Service“ darin meinen den damaligen Stand. Fehlende Nummern betreffen nur SolarAI und stehen dort. Neue Paperboy-ADRs beginnen bei 0030.

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
| [0024](0024-layouts-im-display-service.md) | Daten statt Bilder – Datendokument und YAML-Layouts im Display-Service | Akzeptiert, Umsetzung ausstehend |
| [0025](0025-plaene-je-display.md) | Pläne – regelbasierte Seitenauswahl je Display | Akzeptiert, Umsetzung ausstehend |
| [0026](0026-konfiguration-als-yaml.md) | Konfiguration als YAML mit Displayarten und Geräten | Akzeptiert, Umsetzung ausstehend |
| [0027](0027-textdisplays-und-http-geraete.md) | Textdisplays und einfache HTTP-Geräte | Akzeptiert, Umsetzung ausstehend |
| [0028](0028-paperboy-eigenes-repository.md) | Display-Service als „Paperboy“ in eigenem Repository | Akzeptiert |
| [0029](0029-repository-trennung-umgesetzt.md) | Repository-Trennung SolarAI / Paperboy umgesetzt | Akzeptiert |
