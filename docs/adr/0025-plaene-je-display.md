# 0025: Pläne – regelbasierte Seitenauswahl je Display

- Status: Akzeptiert (Umsetzung in Paperboy ausstehend)
- Datum: 2026-10-20

## Kontext

Mit ADR 0024 rendert der Display-Service Seiten selbst aus YAML-Vorlagen (je Seite und Displayart eine Datei). Offen ist, **welches Gerät wann welche Seiten zeigt**. Heute gilt: ein Gerät gehört zu einem Profil, zeigt dessen Seiten 0…n in fester Reihenfolge, blättert per Taste, kehrt nach `page_timeout` zur Übersicht zurück und hat ein Tages-/Nachtintervall (`refresh`, `night_refresh`, `night_start`, `night_end` in `displayd.json`).

Gewünscht ist ein Ablauf je Gerät, z. B.:

- 07:00–09:00: Kalender und Wetter, zweite Seite PV-Daten
- 09:00–19:00: Wetter klein, PV groß (weitere PV-Seiten zum Blättern)
- ab 19:00: Nachtmodus, z. B. Zustand der Türen

Später soll die Auswahl nicht nur von der Uhrzeit, sondern auch von **Daten** abhängen, z. B. „Tür steht um 22:00 noch offen → Seite Tür-Warnung“. Ein reiner Zeitplan reicht dafür nicht; deshalb heißt das Konzept **Plan** und besteht aus **Regeln**.

Die Daten selbst sind nicht Gegenstand dieses ADR.

## Entscheidung (Vorschlag)

### 1. Plan als eigene Datei mit Regeln

Ein **Plan** beschreibt, welche Seiten ein Gerät unter welchen Bedingungen zeigt. Er liegt als YAML-Datei unter `plans/` (z. B. `plans/wohnzimmer.yaml`) und wird wie Seitenvorlagen ohne Neustart über die Änderungszeit neu geladen (ADR 0024).

```yaml
timezone: Europe/Berlin  # optional, Standard: Zeitzone des Dienstes

rules:
  - name: morgen
    when: { from: "07:00", to: "09:00", days: [mo, di, mi, do, fr] }
    pages: [kalender-wetter, solar-overview]

  - name: tag
    when: { from: "09:00", to: "19:00" }
    pages: [solar-gross-wetter-klein, solar-battery, solar-history]
    rotate: true
    refresh: 5m

  - name: nacht
    when: { from: "19:00", to: "07:00" }   # über Mitternacht erlaubt
    pages: [tueren]
    refresh: 30m

fallback: nacht          # optional: Regel, wenn keine andere passt
```

Ein Plan enthält **keine Displayart**: Er verweist nur auf Seitennamen, die Vorlage wird je Gerät nach dessen Displayart gewählt (ADR 0024, Abschnitt 4). Derselbe Plan kann daher für mehrere Geräte, auch unterschiedlicher Displayart, verwendet werden.

Regeln:

- Eine **Regel** besteht aus Name, Bedingung `when`, Seitenliste `pages` und optional `rotate` und `refresh`.
- **Die erste passende Regel gewinnt** (Reihenfolge in der Datei). Ausnahmen und Warnungen stehen deshalb oben, das Normalprogramm darunter.
- `when` fehlt oder ist leer → Regel passt immer (praktisch als letzte Regel).
- **Zeitbedingungen:** `from` (inklusive) und `to` (exklusiv) in lokaler Zeit; `to` < `from` bedeutet über Mitternacht. Optional `days`; bei Zeiträumen über Mitternacht zählt der Tag des Beginns.
- Mehrere Bedingungen in einem `when` gelten **gemeinsam** (UND). Ein ODER entsteht durch mehrere Regeln. Weitere Bedingungsarten (z. B. Datumsbereiche) lassen sich später als zusätzliche Schlüssel in `when` ergänzen, ohne das Format zu ändern.
- Passt keine Regel und gibt es keinen `fallback`, zeigt das Gerät eine Hinweisseite „Für diese Situation ist keine Seite festgelegt“.
- `pages` sind Seitennamen (ADR 0024); die Reihenfolge ist die Blätterreihenfolge, die erste Seite ist die Startseite der Regel. Seiten dürfen in mehreren Regeln vorkommen.
- `rotate: true` → bei jeder zeitgesteuerten Aktualisierung (also im Takt von `refresh`) die nächste Seite der Regel zeigen, zyklisch. Standard `false`: Seite wechselt nur per Taste.
- `refresh` je Regel ersetzt das heutige Tages-/Nachtintervall (`refresh`, `night_refresh`, `night_start`, `night_end`). Ohne Angabe gilt der Gerätewert.

### 2. Datenbedingungen (vorgesehen, später umgesetzt)

Das Format sieht Bedingungen auf das Datendokument (ADR 0024) vor; umgesetzt werden sie erst bei Bedarf:

```yaml
  - name: tuer-warnung
    when:
      from: "22:00"
      to: "06:00"
      data:
        - { path: haus/tueren/haustuer, equals: offen }
    pages: [tuer-warnung]
    refresh: 5m
```

- Vergleichsarten zunächst bewusst wenige: `equals`, `not_equals`, `above`, `below`, `exists`. Keine Ausdruckssprache.
- Mehrere Einträge unter `data` gelten gemeinsam (UND), zusammen mit den Zeitbedingungen.
- **Fehlender Pfad, `null` oder veralteter Wert** (`stale_after` aus ADR 0024) erfüllen eine Datenbedingung nicht – außer bei `exists: false`. Eine Warnung erscheint also nicht aufgrund fehlender Daten.
- Die in Datenbedingungen verwendeten Pfade gelten als beobachtet (ADR 0024, Abschnitt 3): Ein Schreibzugriff darauf löst eine neue Auswertung des Plans aus.
- Bis zur Umsetzung ist `data` in einem Plan ein Konfigurationsfehler mit klarer Meldung, damit niemand sich auf eine nicht ausgewertete Bedingung verlässt.

### 3. Verhalten zur Laufzeit

- **Auswertung:** bei jedem Abruf des Geräts, bei Ablauf von Zeitgrenzen und (später) bei Änderung beobachteter Datenpfade.
- **Regelwechsel:** Wechselt die aktive Regel, startet das Gerät auf deren erster Seite; die bisherige Blätterposition verfällt.
- **Pünktlicher Wechsel:** Die an das Gerät gemeldete Wartezeit (`refresh_rate` bei TRMNL) wird auf die Zeit bis zur nächsten Zeitgrenze einer Regel begrenzt, damit ein schlafendes Gerät rechtzeitig aufwacht (mit Mindestdauer, z. B. 60 s). Datenbedingungen wirken bei TRMNL-Geräten erst beim nächsten Abruf; `refresh` der Regeln entsprechend wählen. Push-Geräte (OpenDisplay, ADR 0018) können sofort aktualisiert werden.
- **Tasten:** Blättern wie heute, aber nur innerhalb der Seiten der aktiven Regel (zyklisch). Halten (`special_function`) → erste Seite der Regel. Nach `page_timeout` zurück zur ersten Seite; bei `rotate: true` entfällt dieser Rücksprung, die Rotation läuft von der per Taste gewählten Seite aus weiter.
- **Akkuwarnung** (ADR 0020) hat Vorrang vor dem Plan: Balken bzw. Vollbild wie bisher.
- **Veraltete Daten** zeigen pro Seite den Hinweis aus ADR 0024; bei Zeitregeln ändert das die Auswahl nicht.
- Seitenposition, aktive Regel und Rotation sind **Zustand je Gerät**, auch wenn mehrere Geräte denselben Plan nutzen. Die Bilder dagegen werden je (Seite, Displayart) gerendert und geteilt.

### 4. Zuordnung Gerät → Plan

- Ein Gerät verweist in `displayd.json` per `"plan": "wohnzimmer"` auf seinen Plan; mehrere Geräte dürfen denselben Plan verwenden. Fehlt für eine im Plan genannte Seite die Vorlage zur Displayart eines zugeordneten Geräts, ist das ein Konfigurationsfehler (Meldung beim Laden, Hinweisseite auf dem Gerät).
- Wie die Gerätekonfiguration insgesamt künftig aussieht (Displayarten, Geräte, Zuordnung zu Plänen), wird in einem **eigenen ADR** festgelegt.

### 5. Gerät ohne gültigen Plan

- **Kein Plan zugeordnet:** Das Gerät bekommt ein statisches Bild „Kein Plan konfiguriert“ mit Gerätename bzw. MAC, damit man es in der Konfiguration findet. Langes Abrufintervall.
- **Plan fehlerhaft oder Datei fehlt:** Beim Neuladen bleibt die letzte gültige Fassung aktiv. Gab es nie eine gültige Fassung, zeigt das Gerät ein Bild mit kurzer Fehlermeldung (Datei, Zeile).
- In keinem Fall Status 500 oder `reset_firmware` an TRMNL-Geräte (siehe ADR 0017).

## Konsequenzen

- Inhalte, Reihenfolge und Zeiten lassen sich je Gerät ohne Neustart ändern; Vorlagen (wie es aussieht) und Pläne (wann was gezeigt wird) sind getrennt. Ein Plan ist displayunabhängig und für beliebig viele Geräte nutzbar.
- Datenabhängige Seiten (Warnungen) und weitere Bedingungsarten passen später ohne Formatbruch hinein.
- Die bisherigen Tages-/Nachtfelder und die feste Seitenreihenfolge der Profile werden durch Regeln ersetzt; für den Übergang bleiben sie für Geräte ohne Plan gültig (Bild-Upload nach ADR 0017).
- Der Gerätezustand in `state.json` braucht zusätzlich die aktive Regel (Name) neben der Seite, um Regelwechsel zu erkennen.
- Zeitlogik (Mitternacht, Wochentage, Sommer-/Winterzeit) muss sorgfältig getestet werden; Zeiten werden in der Zeitzone des Plans ausgewertet, ein Zeitraum `02:00–03:00` existiert am Umstellungstag ggf. nicht bzw. doppelt.
- Aufwandsschätzung: Planformat, Validierung und Neuladen ca. 1 Tag; Regelauswahl, Wechsel, Rotation, Wartezeitbegrenzung und Tasten ca. 1–2 Tage; Hinweisbilder ½ Tag. Datenbedingungen später ca. 1 Tag.

## Offene Fragen

Derzeit keine.

Geklärt: Datumsbereiche werden derzeit nicht gebraucht und wären später nur eine weitere Bedingung in `when`. Rotation ist ein Schalter `rotate` je Regel. Mehrere Geräte können sich einen Plan teilen; gerätespezifische Abweichungen gibt es nicht – ein abweichendes Gerät bekommt eine Kopie des Plans. Warnregeln lassen sich vorerst nicht quittieren; sie gelten, solange ihre Bedingung erfüllt ist.
