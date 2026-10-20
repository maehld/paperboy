# 0027: Textdisplays und einfache HTTP-Geräte

- Status: Akzeptiert (Umsetzung in Paperboy ausstehend)
- Datum: 2026-10-20

## Kontext

Neben ePaper-Geräten (TRMNL, OpenDisplay) sollen Textdisplays bedient werden. Das vorhandene Textdisplay läuft heute mit **ESPHome**. Solche Anzeigen zeigen typischerweise wenige Zeilen fester Breite (z. B. 20 × 4), werden häufig aktualisiert (Sekundentakt) und haben oft einen eingeschränkten Zeichensatz (HD44780: Umlaute nur teilweise bzw. als eigene Zeichen).

Erwogene Anbindungen:

| Variante | Richtung | Bewertung |
|---|---|---|
| Gerät holt fertigen Text per HTTP | Gerät → Dienst | wie TRMNL; Gerät braucht nur Adresse und Token; Dienst muss Gerät nicht erreichen; bevorzugt |
| Dienst schreibt per ESPHome-`web_server` (`POST /text/<id>/set`) | Dienst → Gerät | wie OpenDisplay, sofortige Aktualisierung; Dienst muss Gerät erreichen, an ESPHome-Version gebunden; spätere Option |
| MQTT (Dienst veröffentlicht, ESPHome abonniert) | über Broker | echtes Push, braucht Broker und MQTT-Client; spätere Option |
| Rohdaten ans Gerät, Formatierung in ESPHome | Gerät → Dienst | verworfen: Gestaltung läge wieder in der Firmware statt in Vorlagen und Plänen |

Grafische ESPHome-Displays können außerdem fertige PNGs laden (`online_image`).

## Entscheidung (Vorschlag)

### 1. Textvorlagen

- Displayarten mit `kind: text` (ADR 0026) haben `size: [spalten, zeilen]` und `charset`.
- Seitenvorlagen `pages/<seite>.<displayart>.yaml` (ADR 0024) enthalten statt Bausteinen Zeilen als Go-`text/template` mit denselben Hilfsfunktionen (`power`, `energy`, `percent`, …):

  ```yaml
  watch:
    - { path: solar/current, stale_after: 1m }
  lines:
    - "PV   {{ power .solar.current.latest.values.pv_power }}"
    - "Haus {{ power .solar.current.latest.values.load_power }}"
    - "Akku {{ percent .solar.current.latest.values.battery_soc }}"
    - "{{ time .solar.current.latest.time }}"
  ```

- Zusätzliche Hilfsfunktionen für feste Breiten: `pad`, `right`, `center`.
- Zeilen werden auf die Spaltenzahl gekürzt, fehlende Zeilen leer ergänzt. Zu viele Zeilen sind ein Konfigurationsfehler.
- **Zeichensatz:** Der Dienst wandelt die Ausgabe gemäß `charset` um, z. B. `ascii` (ä → ae, ° → o) oder `hd44780` (Ersetzungstabelle für vorhandene Zeichen). Ohne Angabe: UTF-8 unverändert.
- Veraltete Daten: statt Hinweisbalken ein konfigurierbares Kennzeichen (z. B. `!` in der letzten Spalte der ersten Zeile).
- Pläne (ADR 0025) gelten unverändert; ein Plan kann Seiten nennen, für die es Bild- und Textvorlagen gibt.

### 2. Protokoll `text-http` (Gerät holt Text)

- `GET /api/text` mit `Authorization: Bearer <geräte-token>`.
- **Das Token identifiziert das Gerät.** Jedes Gerät hat ein eigenes Token (`token_env` in der Konfiguration); eine zusätzliche ID im Header ist nicht nötig. Das passt zu ESPHome, das feste Header einfach setzen kann; die MAC wäre dort umständlicher.
- Antwort:

  ```json
  { "lines": ["PV   4,21 kW", "Haus  820 W", "Akku 82 %", "09:12"], "page": "solar-overview", "refresh_seconds": 1 }
  ```

- Header `ETag` (Inhalts-Hash); fragt das Gerät mit `If-None-Match` an und hat sich nichts geändert, antwortet der Dienst mit **304** ohne Inhalt. So bleibt Abfragen im Sekundentakt billig.
- **Tasten** (optional): `POST /api/text/button` mit `{"button": "next"|"previous"|"home"}` wirkt wie bei TRMNL (Blättern innerhalb der aktiven Regel, `home` = erste Seite).
- Unbekanntes Token → 401. Gerät ohne Plan → Zeilen mit Hinweis „Kein Plan“.

### 3. Protokoll `image-http` (Gerät holt Bild)

- Für grafische Displays ohne TRMNL-Firmware, z. B. ESPHome mit `online_image`: `GET /api/image` mit `Authorization: Bearer <geräte-token>` liefert das PNG der aktuellen Seite, mit `ETag`/304 wie oben.
- Displayart `kind: image`; Rendern, Pläne und Tasten-Endpunkt wie bei `text-http` (`POST /api/image/button`).

### 4. Spätere Optionen (nicht Teil dieser Umsetzung)

- **Push an ESPHome** über dessen `web_server`-REST-Schnittstelle, analog zum OpenDisplay-Pusher (ADR 0018): sofortige Aktualisierung bei Datenänderung, Dienst muss Gerät erreichen.
- **MQTT** als Ausgabeweg (fertige Zeilen auf ein Topic je Gerät).

## Konsequenzen

- Textdisplays nutzen Datendokument, Vorlagen und Pläne wie ePaper-Geräte; die ESPHome-Konfiguration beschränkt sich auf Abruf und Anzeige.
- Je Gerät ein Token: einfache Zuordnung und einzeln widerrufbar; Tokens müssen in der ESPHome-Konfiguration (bzw. deren `secrets.yaml`) hinterlegt werden.
- Abfragen im Sekundentakt erzeugen Last nur im HTTP-Teil; gerendert wird weiterhin nur bei Datenänderung (ADR 0024).
- Für das vorhandene ESPHome-Gerät ist eine Anpassung der Firmware-Konfiguration nötig (`http_request` + Anzeige-Lambda); eine Beispielkonfiguration gehört in die Doku.
- Aufwandsschätzung: Textvorlagen und Zeichensatz ca. 1 Tag, `text-http`/`image-http` mit ETag und Tasten ca. 1 Tag, ESPHome-Beispiel ½ Tag.

## Offene Fragen

1. Welche Hardware genau (Controller, Spalten × Zeilen, Zeichensatz) hat das vorhandene Textdisplay, und hat es Tasten?
2. Reicht HTTP-Abfrage im Sekundentakt, oder ist sofortige Aktualisierung (Push) nötig?
