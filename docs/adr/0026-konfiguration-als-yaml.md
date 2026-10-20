# 0026: Konfiguration des Display-Service als YAML mit Displayarten und Geräten

- Status: Akzeptiert (Umsetzung in Paperboy ausstehend)
- Datum: 2026-10-20

## Kontext

Die Konfiguration des Display-Service liegt heute in `displayd.json` (ADR 0017, 0018, 0020): `upload_token`, `public_url`, `stale_after`, `page_timeout`, `battery`, `profiles` (Größe, Graustufen, Panelbreite) und `devices` (TRMNL nach MAC bzw. OpenDisplay nach Adresse, mit `profile`, `refresh`, `night_refresh`, `night_start`, `night_end`, `buttons`).

Mit ADR 0024 (Datendokument, Seitenvorlagen je Displayart, YAML) und ADR 0025 (Pläne) ändern sich die Begriffe: Profile werden zu **Displayarten**, die Seitenreihenfolge und das Tag-/Nachtintervall wandern in Pläne. Vorlagen und Pläne sind YAML; die Hauptkonfiguration sollte dasselbe Format nutzen. Mit der Umbenennung (ADR 0028) bricht die Konfiguration ohnehin; ein Übergang mit beiden Formaten ist nicht nötig.

## Entscheidung (Vorschlag)

### 1. Eine YAML-Datei, kein JSON mehr

Die Konfiguration liegt in `paperboy.yaml` (Name nach ADR 0028). `displayd.json` wird nicht mehr gelesen; eine Umstellung erfolgt von Hand anhand der Beispieldatei.

```yaml
server:
  public_url: "http://192.168.1.10:8090"   # für TRMNL-Bild-URLs
  upload_token_env: PAPERBOY_UPLOAD_TOKEN    # Token aus Umgebungsvariable (bevorzugt)
  # upload_token: "…"                        # alternativ direkt, nicht empfohlen

directories:          # relativ zur Konfigurationsdatei
  pages: pages
  plans: plans
  data: data          # Datendokument, state.json, gerenderte Bilder

defaults:
  page_timeout: 5m
  refresh: 5m
  battery: { warn_volts: 3.45, critical_volts: 3.3, hysteresis_volts: 0.1 }

display_types:
  e1001:
    kind: image
    size: [800, 480]
    gray_levels: 4
    width_mm: 163
    min_refresh: 60s
  sticky:
    kind: image
    size: [800, 480]
    gray_levels: 4
    width_mm: 86
    min_refresh: 60s
  lcd2004:            # siehe ADR 0027
    kind: text
    size: [20, 4]     # Spalten × Zeilen
    charset: hd44780
    min_refresh: 1s

devices:
  wohnzimmer:
    display_type: e1001
    protocol: trmnl
    mac: "44:1B:F6:85:AA:1C"
    plan: standard
  kueche:
    display_type: sticky
    protocol: trmnl
    mac: "B8:1F:3F:D8:6A:FC"
    plan: standard
    battery: { warn_volts: 3.5 }          # überschreibt einzelne Standardwerte
  flur:
    display_type: sticky
    protocol: opendisplay
    address: 192.168.1.50
    buttons: false
    plan: flur
  buero-lcd:
    display_type: lcd2004
    protocol: text-http                   # ADR 0027
    token_env: PAPERBOY_TOKEN_BUERO_LCD
    plan: buero
```

### 2. Begriffe und Regeln

- **Displayart** (`display_types`, früher `profiles`): beschreibt die Anzeige – `kind` (`image` oder `text`), Größe, Graustufen bzw. Zeichensatz, Panelbreite, `min_refresh`. Der Schlüssel ist zugleich die Dateiendung der Seitenvorlagen (`pages/<seite>.<displayart>.yaml`, ADR 0024).
- **Gerät** (`devices`): ein physisches Display. Der **Schlüssel ist die stabile Geräte-ID** (Kleinbuchstaben, Ziffern, `-`); sie wird in `state.json`, Logs und URLs verwendet. Felder: `display_type`, `protocol`, protokollspezifische Kennung (`mac`, `address`, `token_env`), `plan` und optionale Überschreibungen von `defaults`.
- **Protokoll** (`protocol`): `trmnl` (ADR 0017), `opendisplay` (ADR 0018), `text-http` und `image-http` (ADR 0027). Das Protokoll legt fest, welche Kennungsfelder Pflicht sind; die Displayart muss zum Protokoll passen (`text-http` nur mit `kind: text`).
- **Plan** (`plan`): Name einer Datei unter `plans/` (ADR 0025). Mehrere Geräte dürfen denselben Plan nutzen. Ohne `plan` zeigt das Gerät das Hinweisbild „Kein Plan konfiguriert“.
- **Entfallen:** `profiles`, `refresh`/`night_refresh`/`night_start`/`night_end` je Gerät (ersetzt durch `refresh` je Regel im Plan; `defaults.refresh` bzw. Gerät-`refresh` nur als Rückfall), globales `stale_after` (je beobachtetem Pfad in der Vorlage, ADR 0024), Bild-Upload-Profile.
- `refresh` wird nie kleiner als `min_refresh` der Displayart an ein Gerät gemeldet.
- **Geheimnisse** bevorzugt über Umgebungsvariablen (`*_env`), damit die YAML-Datei ohne Token versioniert werden kann. Direkte Angabe bleibt möglich.

### 3. Validierung und Neuladen

- Beim Start wird die ganze Konfiguration geprüft (unbekannte Schlüssel, fehlende Pflichtfelder, doppelte MAC/Adresse/Token, unbekannte Displayart oder unbekannter Plan, Displayart passt nicht zum Protokoll). Fehler beim Start → Abbruch mit verständlicher Meldung.
- **Neuladen ohne Neustart** über die Änderungszeit wie bei Vorlagen und Plänen (ADR 0024): `defaults`, `display_types` und `devices` werden übernommen, wenn die neue Fassung gültig ist; sonst bleibt die alte aktiv und der Fehler wird protokolliert.
- Änderungen unter `server` und `directories` (Adresse, Token, Verzeichnisse) wirken erst nach einem Neustart; das wird beim Neuladen protokolliert.
- Wird ein Gerät aus der Konfiguration entfernt, bleibt sein Eintrag in `state.json` (z. B. TRMNL-Schlüssel) erhalten, bis er ausdrücklich gelöscht wird; ein versehentliches Entfernen erzwingt so kein neues Setup.

## Konsequenzen

- Eine einheitliche Konfigurationssprache für Konfiguration, Vorlagen und Pläne; Kommentare möglich.
- **Bruch:** Bestehende `displayd.json` muss von Hand übertragen werden. TRMNL-Geräte behalten ihre Zugangsdaten nur, wenn `state.json` übernommen und die Geräte-IDs dort zugeordnet werden (Migration in ADR 0028).
- Geräte haben stabile, lesbare IDs statt Name/MAC als Schlüssel; Umbenennen eines Geräts ist damit eine Änderung der ID (Zustand geht verloren, sofern nicht migriert).
- Aufwandsschätzung: Konfigurationsmodell, Validierung und Neuladen ca. 1–1,5 Tage, Beispielkonfiguration und Doku ½ Tag.

## Offene Fragen

1. Wird `width_mm` noch gebraucht (heute nur für kompakt/ausführlich in SolarAI), oder entfällt es mit Vorlagen je Displayart?
2. Soll ein abgemeldetes Gerät über die API (`DELETE /api/devices/{id}/state`) oder nur durch Bearbeiten von `state.json` gelöscht werden?
