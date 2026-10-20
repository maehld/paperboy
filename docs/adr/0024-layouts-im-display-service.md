# 0024: Daten statt Bilder – Datendokument und YAML-Layouts im Display-Service

- Status: Akzeptiert (Umsetzung in Paperboy ausstehend)
- Datum: 2026-10-20

## Kontext

Heute rendert SolarAI die Bilder für die Anzeigen selbst (`internal/epaper`): ein `image.Gray` in Displaygröße, eigene Zeichenfunktionen (`fill`, `shade`, `line`, `text`, `fitted`), Bausteine (`header`, `tiles`, `rows`, `drawDayChart`) und fest programmierte Layouts (kompakt/ausführlich, vier Seiten). Der `Publisher` lädt fertige PNGs per `PUT /api/images/{profil}/{seite}` zum Display-Service (ADR 0017), der sie nur noch auf die Palette normalisiert und ausliefert.

Daraus ergeben sich Grenzen:

- Jede Layoutänderung erfordert Go-Code und einen neuen Build von SolarAI.
- Der Display-Service kann nur fertige Bilder ausliefern. Andere Inhalte (später z. B. Wetter, Kalender) oder andere Anzeigearten (Textdisplays) passen nicht ins Modell.
- Der Display-Service soll SolarAI-unabhängig bleiben (ADR 0017: importiert keine SolarAI-Pakete, späteres eigenes Repo).

Erwogene Alternativen für das Templating:

| Ansatz | Bewertung |
|---|---|
| HTML/CSS + Headless-Chromium (wie TRMNL) | sehr flexibel, aber Chromium im Container (≥ 300 MB, langsam auf ARM), geglättete Schrift auf 1-Bit-Displays unscharf |
| SVG-Template (`text/template`) + resvg | schlank, gut anpassbar, aber externe Rust-Binärdatei |
| Eigene Templatesprache | unnötig; Go `text/template` deckt Formatierung ab |
| **Deklaratives Layout (YAML) aus Bausteinen**, gerendert mit dem vorhandenen Go-Code | reines Go, scharfe Ausgabe wie heute; Flexibilität auf die Bausteine begrenzt |

## Entscheidung (Vorschlag)

### 1. Ein Datendokument, beschreibbar über Pfade

Der Display-Service hält **ein** JSON-Dokument. Dessen Inhalt ist frei; der Display-Service kennt keine fachlichen Begriffe. Weitere Zweige (z. B. `wetter`) sind jederzeit möglich, werden aber erst mit eigenem Bedarf festgelegt. Der Display-Service ruft **selbst keine externen APIs** auf; alle Daten werden von außen zugeliefert.

#### Zweig `solar` (in Anlehnung an die SolarAI-API)

SolarAI schreibt seine Daten voraussichtlich **in Anlehnung an seine eigene HTTP-API**, damit es nur ein Datenmodell gibt (Schlüssel wie `pv_power`, Einheiten W/kWh/%/°C, ungültig = `null`, Zeiten in UTC). **Das genaue Format wird nicht hier, sondern bei der Umsetzung in SolarAI festgelegt** (eigenes ADR bzw. README). Die folgende Aufteilung ist nur eine Orientierung:

| Pfad | Inhalt | Form wie | Schreiben |
|---|---|---|---|
| `solar/current` | `mode`, `connected`, `stale`, `last_error`, `latest` (`time`, `values`), `poll_seconds` | `GET /api/current` | nach jedem Poll, in einem PUT |
| `solar/history` | `points` (Tagesverlauf seit lokaler Mitternacht, reduziert, z. B. 5-Minuten-Raster), `retention_seconds`, `persistent` | `GET /api/history` | z. B. alle 5 min |
| `solar/metrics` | Messwertdefinitionen (`key`, `label`, `unit`, …) | `GET /api/metrics` | beim Start |
| `solar/derived` | heute in `internal/epaper` berechnete Werte: Tagesbilanz (Verbrauch, Autarkie, Eigenverbrauch, Kennzeichen „Näherung“), Akku-Restzeit | neu | mit `solar/current` bzw. `solar/history` |

```json
{
  "solar": {
    "current": {
      "mode": "live", "connected": true, "stale": false, "poll_seconds": 10,
      "latest": { "time": "2026-10-20T09:12:03Z", "values": { "pv_power": 4210, "battery_soc": 81.5, "grid_power": null } }
    },
    "history": { "points": [ { "time": "…", "values": { … } } ], "retention_seconds": 86400, "persistent": true },
    "derived": { "balance": { "consumption": 9.8, "autarky": 0.87, "self_consumption": 0.64, "approximate": false }, "battery_remaining": "3 h 20 min" }
  }
}
```

Ändert sich die SolarAI-API, ändert sich der Zweig mit; die Vorlagen sind entsprechend anzupassen.

**Schnittstelle** (Bearer-Token wie bisher):

| Anfrage | Wirkung |
|---|---|
| `PUT /api/data` | ersetzt das **gesamte** Dokument (löscht auch Zweige anderer Zulieferer; in der Doku deutlich kennzeichnen) |
| `PUT /api/data/{pfad}` | ersetzt den Teilbaum an `pfad` vollständig, z. B. `/api/data/solar` oder `/api/data/solar/daily` |
| `GET /api/data[/{pfad}]` | liefert Dokument bzw. Teilbaum (Fehlersuche) |
| `DELETE /api/data/{pfad}` | entfernt den Teilbaum |

Regeln:

- **PUT ersetzt, es wird nicht zusammengeführt.** Wer einzelne Teile aktualisieren will, schreibt auf einen tieferen Pfad. Kein `PATCH` vorerst.
- **Fehlende Zwischenobjekte werden angelegt** (`PUT /api/data/solar/daily` funktioniert auch ohne vorhandenes `solar`).
- **Konflikt:** Ist ein Zwischenpfad vorhanden, aber kein Objekt (z. B. `solar` ist eine Zahl), antwortet der Dienst mit **409** und überschreibt nicht.
- **Pfadsegmente** bestehen nur aus `a-z`, `0-9`, `_`, `-`. Keine Array-Indizes: **Arrays werden immer als Ganzes geschrieben.**
- `null` ist ein gültiger Wert und bedeutet „ungültig/unbekannt“; Layouts zeigen ihn als `—` (ADR 0005).
- **Zusammengehörige Werte in einem PUT schreiben** (z. B. eine Messung komplett nach `solar/current`), damit kein Bild aus einer halb aktualisierten Momentaufnahme entsteht.
- Größenbegrenzung je Anfrage (z. B. 1 MB).
- Das Dokument wird bei jeder Änderung atomar unter `-data` gespeichert und übersteht Neustarts.

### 2. Änderungszeitstempel je Schreibpfad

Der Dienst führt eine flache Tabelle `Pfad → Zeitpunkt des letzten Schreibens`, und zwar **genau auf der Tiefe des jeweiligen Schreibzugriffs**:

- `PUT /api/data/solar` setzt den Eintrag `solar` und **entfernt alle Einträge darunter** (`solar/daily` usw.), da der Teilbaum ersetzt wurde.
- `PUT /api/data/solar/daily` setzt nur `solar/daily`.
- `PUT /api/data` leert die Tabelle und setzt einen Eintrag für die Wurzel; `DELETE` entfernt die Einträge des Teilbaums.

**Alter eines Pfades P** = Zeit seit dem jüngsten Eintrag auf P, auf einem übergeordneten Pfad oder unterhalb von P (ein Schreiben weiter unten verändert P ebenfalls). Die Tabelle bleibt klein (so viele Einträge wie verschiedene Schreibpfade) und wird zusammen mit dem Dokument gespeichert.

**Zeitpunkt vom Zulieferer (optional):** Ein Schreibzugriff darf seinen Zeitpunkt selbst angeben (Header `X-Data-Time`, RFC 3339). Dann wird dieser statt der Empfangszeit eingetragen. So bleiben Daten, die verspätet oder erneut zugestellt werden, korrekt als alt erkennbar. Zeitpunkte in der Zukunft werden auf die Empfangszeit begrenzt; ohne Header gilt die Empfangszeit.

Die Zeitstempel dienen der Erkennung **veralteter** Daten: Bleibt ein Zulieferer aus, ändern sich die Daten nicht – das lässt sich nur über das Alter erkennen, nicht über einen Inhaltsvergleich. Ein Layout legt je beobachtetem Pfad ein `stale_after` fest; danach erscheint der bekannte Hinweisbalken.

### 3. Neu rendern über beobachtete Pfade und Inhalts-Hash

Jede Seitenvorlage gibt an, welche Pfade sie verwendet:

```yaml
watch:
  - { path: solar/current, stale_after: 5m }
  - { path: solar/history, stale_after: 30m }
```

Ablauf:

1. **Bei jedem Schreibzugriff** werden alle Seitenvorlagen als „vielleicht geändert“ markiert, deren beobachtete Pfade sich mit dem geschriebenen Pfad überschneiden (gleich, darüber oder darunter).
2. Für markierte Vorlagen wird ein **Hash** gebildet über die beobachteten Teilbäume (kanonisches JSON) **und** den Veraltet-Zustand je Pfad. Ist er gleich dem gespeicherten Hash des letzten Laufs, passiert nichts; sonst wird gerendert und der neue Hash gespeichert. Alte Daten müssen dafür nicht aufbewahrt werden.
3. **Zeitgesteuert** wird zusätzlich geprüft, ob ein beobachteter Pfad sein `stale_after` überschritten hat (der Hash ändert sich dann über den Veraltet-Zustand), sowie bei Vorlagen mit Uhrzeitanzeige.

Der vorhandene Bild-Hash (`filename` bei TRMNL) sorgt danach dafür, dass ein Gerät nur bei tatsächlich anderem Bild neu lädt.

Der Datenvergleich ist nötig, weil SolarAI regelmäßig schreibt (alle 10 s), auch wenn sich z. B. die Tageswerte nicht geändert haben.

### 4. Seitenvorlagen je Seite und Displayart

- Eine **Seite** hat einen displayunabhängigen Namen (z. B. `solar-overview`). Für jede Displayart gibt es eine eigene **Vorlage** als Datei; die Displayart steht in der Dateiendung:

  ```
  pages/solar-overview.e1001.yaml
  pages/solar-overview.sticky.yaml
  pages/tuer-warnung.e1001.yaml
  pages/tuer-warnung.sticky.yaml
  ```

- Die **Displayart** (Größe, Graustufen, ggf. Panelbreite) ist in der Konfiguration definiert (heute `profiles` in `displayd.json`) und nicht in der Vorlage; die Vorlage gilt genau für diese Art.
- Pläne (ADR 0025) verweisen nur auf Seitennamen. Zur Laufzeit wählt der Dienst die Vorlage `<seite>.<displayart>.yaml` des jeweiligen Geräts. Dadurch kann **ein Plan für Geräte unterschiedlicher Displayart** genutzt werden.
- **Fehlt die Vorlage** für eine Displayart, ist das beim Laden ein Konfigurationsfehler (Meldung) und das Gerät zeigt an dieser Stelle eine Hinweisseite „Seite `solar-overview` für Displayart `sticky` fehlt“. Eine allgemeine Vorlage ohne Displayart als Rückfall ist vorerst nicht vorgesehen, da Layouts für sehr unterschiedliche Größen selten gut skalieren.
- Gerendert und zwischengespeichert wird je (Seite, Displayart); alle Geräte derselben Art teilen sich das Bild.
- **Neu laden ohne Neustart:** Der Dienst prüft regelmäßig (z. B. alle 5 s) die Änderungszeit (`ModTime`) der Dateien unter `pages/`. Bei Änderung wird die Datei neu gelesen und validiert; nur eine gültige Fassung ersetzt die alte, sonst bleibt die bisherige aktiv und der Fehler wird protokolliert (und in der Vorschau angezeigt). Neue und gelöschte Dateien werden ebenso erkannt. Ein erfolgreiches Neuladen markiert die Vorlage zum Neurendern.
- Positionierung bewusst einfach: Zeilen/Spalten mit Anteilen (`rows: [1, 3, 2]`), keine freie CSS-artige Layout-Engine.

Skizze `pages/solar-overview.e1001.yaml` (nicht endgültig):

```yaml
title: Übersicht
watch:
  - { path: solar/current, stale_after: 5m }
  - { path: solar/history, stale_after: 30m }
rows: [1, 4, 3]
blocks:
  - header: { title: "Solar", time: solar.current.latest.time }
  - tiles:
      - { label: PV,   value: solar.current.latest.values.pv_power,    format: power }
      - { label: Haus, value: solar.current.latest.values.load_power,  format: power }
      - { label: Netz, value: solar.current.latest.values.grid_power,  format: power, sign: [Einspeisung, Bezug] }
      - { label: Akku, value: solar.current.latest.values.battery_soc, format: percent }
  - chart: { points: solar.history.points, series: [pv_power, load_power], unit: kW }
```

### 5. Bausteine und Formatierung

- Startumfang, aus dem heutigen Code übernommen: `header`, `tiles`, `rows` (Beschriftung/Wert), `chart` (Tagesdiagramm), `text`, `bar`, Schraffur, Trennlinien. Weitere Bausteine (z. B. Wetter, Kalender) später mit eigenem Bedarf.
- Bausteine nutzen die vorhandenen Zeichenfunktionen und die TrueType-Schrift (ADR 0013).
- Texte in Layouts nutzen Go `text/template` mit festen Hilfsfunktionen: `power` (< 1.000 W ganze Watt, sonst kW mit zwei Nachkommastellen), `energy`, `percent`, `temp`, `time`, `abs`. Keine eigene Templatesprache.
- Fachliche Berechnungen, die heute in `internal/epaper` stecken (`DailyBalance`, Akku-Restzeit), liefert **SolarAI im Datendokument mit**, damit der Display-Service fachneutral bleibt.

### 6. Übergang

- Die bisherige Bild-API `PUT /api/images/…` bleibt erhalten; ein Profil nutzt entweder hochgeladene Bilder oder Seitenvorlagen.
- Erst wenn die Vorlagen die heutigen Seiten auf Sticky und E1001 gleichwertig darstellen, wird der Bild-Upload in SolarAI entfernt (eigenes ADR).

## Konsequenzen

- Layouts lassen sich ohne Neubau anpassen.
- Beliebige weitere Datenzweige passen ins selbe Modell, ohne dass der Display-Service fachlich erweitert oder ins Internet verbunden werden muss; SolarAI bleibt auf Wechselrichterdaten beschränkt.
- Mehrere Zulieferer teilen sich ein Dokument. Mit einem gemeinsamen Token kann jeder alles überschreiben; später denkbar: Tokens, die nur unterhalb eines Pfades schreiben dürfen.
- Die Zeichenfunktionen und die Schrift wandern von `internal/epaper` nach `internal/displayd` (bzw. ein Unterpaket); `golang.org/x/image` wird dann auch dort benötigt.
- **Neue Abhängigkeit (akzeptiert):** Go hat keinen YAML-Parser in der Standardbibliothek; verwendet wird `go.yaml.in/yaml/v3` (Version bei der Umsetzung festlegen und in `AGENTS.md` eintragen). Vorlagen und Pläne sind YAML.
- Das Layoutformat ist faktisch eine kleine eigene Sprache und wird mit jedem Wunsch wachsen. Gegenmaßnahme: wenige, klar dokumentierte Bausteine; Validierung beim Laden mit verständlichen Fehlermeldungen; Vorschau-PNGs per Test bzw. `GET /api/preview/{displayart}/{seite}`.
- Aufwandsschätzung: Datendokument mit Pfad-API, Zeitstempeln und Speicherung ca. 1 Tag; Change Detection ½ Tag; Layout-Parser + Bausteine + Portierung der vier Seiten 3–5 Tage.

## Offene Fragen

1. **Textdisplays:** siehe ADR 0027.

Geklärt: YAML-Abhängigkeit ist akzeptiert. Vorlagen sind nicht pro Gerät überschreibbar; ein abweichendes Gerät bekommt eine eigene Seite und einen kopierten Plan (ADR 0025). Das Format des Zweigs `solar` wird in SolarAI festgelegt.
