# 0012: ePaper-Anzeige über OpenDisplay im LAN

- Status: Akzeptiert (Schriftfrage durch ADR 0013 entschieden)
- Datum: 2026-10-14

## Umsetzung (Stand Annahme)

- Verbindungstest mit `cmd/odtest` an zwei reTerminal Sticky und einem E1001 laut Nutzer erfolgreich; Bild in nativer Panelausrichtung korrekt, daher keine Rotation.
- `internal/epaper`: Option `-epaper host[:port]` (mehrfach), `-epaper-interval` (Standard 5 min, mindestens 1 min). Pro Aktualisierung neue TCP-Verbindung (Gerät bedient nur einen Client und trennt untätige Sitzungen), Konfiguration einmal gelesen und zwischengespeichert.
- Layout nach Panelbreite: unter 120 mm kompakt (Sticky: vier große Kacheln + Tageszeile), sonst ausführlich (E1001: Kacheln, Tagesdiagramm Solar/Haus, Tagesbilanz).
- Unverändertes Bild wird nicht erneut gesendet. Monochrom: schneller Refresh, nach 11 schnellen ein Vollrefresh; 4 Graustufen: immer Vollrefresh.
- Veraltete Daten werden invertiert im Kopf markiert, fehlende Werte als „—“.

## Kontext

Vorhanden sind zwei Seeed **reTerminal Sticky** und ein Seeed **reTerminal E1001** mit OpenDisplay-Firmware. WLAN kann auf allen eingerichtet, die Verschlüsselung deaktiviert werden.

| Gerät | Controller | Panel (laut Hersteller/Handel) |
|---|---|---|
| reTerminal Sticky | ESP32-S3 (Wi‑Fi + BLE), 750 mAh Akku | 3,97″, 800 × 480, Touch |
| reTerminal E1001 | ESP32-S3 | 7,5″, 800 × 480, Monochrom mit 4 Graustufen, Teilaktualisierung |

OpenDisplay (Protokoll laut opendisplay.org): Der **Sender rendert** das fertige Bild; das Display schreibt die Pixel nur in den Framebuffer. Transport per BLE-GATT oder – bei ESP32 mit Flex-Firmware – **per TCP im LAN** (Gerät ist Server, Port 2446, mDNS `_opendisplay._tcp`). Rahmen: `[Länge 2 Byte LE][Payload ≤ 4096]`, Payload identisch zu BLE.

Relevante Befehle: `0x0040` Konfiguration lesen (Displaypaket Typ 32: Breite, Höhe, Farbschema, Teilaktualisierung), `0x0070` Start, `0x0071` Pixeldaten (LAN bis ca. 1000 Byte pro Stück, je Stück ein ACK), `0x0072` Ende (optional Refresh-Modus, ETag), danach `0x0073` Erfolg bzw. `0x0074` Timeout. Pixelformate u. a.: Schema 0 = 1 bpp S/W (MSB links, 1 = weiß), Schema 5 = 2 bpp vier Graustufen (00 schwarz … 11 weiß). Teilaktualisierung (`0x0076`) nur für 1 bpp.

## Entscheidung (vorgeschlagen)

SolarAI erhält einen optionalen **OpenDisplay-LAN-Sender** im Go-Backend:

- Option z. B. `-epaper host[:port]` (mehrfach angebbar), alternativ später mDNS-Erkennung.
- Beim Verbinden Konfiguration lesen (`0x0040`), Panelgröße und Farbschema daraus übernehmen; nur unterstützte Schemata (0 und 5) bedienen, sonst Fehler melden.
- Bild serverseitig mit `image`/`image/draw` rendern: großer Wert für aktuelle PV-Leistung, Batterie-SoC, Netz/Haus, Tagesbilanz, Uhrzeit der Messung; „—“ bzw. „veraltet“ gemäß ADR 0005. Layout je Panelgröße (Sticky kompakt, E1001 ausführlicher, ggf. mit kleinem Tagesdiagramm).
- Übertragung unkomprimiert per Direct Write. Optional später Kompression nur mit Huffman-Codierung (`flate.HuffmanOnly` erzeugt keine Rückverweise und hält damit das 512-Byte-Fenster der Firmware ein), sofern das Gerät `streaming_decompression` meldet.
- Aktualisierung selten und nur bei relevanter Änderung (z. B. alle 5–15 min oder bei deutlicher Wertänderung), unabhängig vom Modbus-Polling; Fehler am Display beeinflussen das Polling nicht.
- Keine Verschlüsselung in der ersten Ausbaustufe (am Gerät deaktiviert). AES-CMAC/CCM fehlen in der Standardbibliothek und wären separat zu implementieren.
- Kein BLE (würde eine externe Abhängigkeit erfordern, ADR 0002).

### Offene Punkte

- **Schriftdarstellung:** Die Go-Standardbibliothek kann keine Schriften rastern. Optionen: (a) eigene eingebettete Bitmap-/Ziffernschrift (keine Abhängigkeit, Ziffern groß skalierbar), (b) `golang.org/x/image/font` + TrueType (bessere Typografie, aber Abhängigkeit → eigenes ADR, löst ADR 0002 teilweise ab), (c) Headless-Browser-Screenshot des Dashboards (externe Laufzeitabhängigkeit). Tendenz: (a).
- **Stromversorgung:** Für LAN muss das ESP32 dauerhaft im WLAN erreichbar sein. OpenDisplay rät von Akkubetrieb bei ESP32 ab; die Sticky (750 mAh) bräuchte daher vermutlich USB-Strom. Zu prüfen, ob die Firmware Schlafzyklen mit LAN unterstützt.
- Farbschema und Teilaktualisierung der Sticky am Gerät per `0x0040` ermitteln.
- Ghosting bei häufigen Teilaktualisierungen: regelmäßig Vollrefresh einplanen.

## Verworfene Alternativen

- **Home Assistant als Sender** (offizielle OpenDisplay-Integration): weniger eigener Code, aber zusätzliches System; beim Nutzer nicht gesetzt.
- **Python-SDK `py-opendisplay` als Hilfsprozess:** BLE-fähig, aber zweite Laufzeitumgebung.

## Konsequenzen

- Lokale, cloudfreie Statusanzeige ohne zusätzliches System.
- Neues Paket (z. B. `internal/opendisplay`) mit Protokolltests (Rahmenbildung, Chunking, ACK-/Fehlerbehandlung, Pixelpacking).
- SolarAI schreibt erstmals aktiv auf ein Fremdgerät (das Display) – der Wechselrichter bleibt unberührt (ADR 0003 gilt weiter).
