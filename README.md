# Paperboy

Paperboy ist ein kleiner Display-Dienst für ePaper-Anzeigen. Zulieferer wie [SolarAI](https://github.com/maehld/solarai) laden fertige Seiten als PNG hoch; Paperboy liefert sie an Geräte mit der unveränderten [TRMNL-Firmware](https://github.com/usetrmnl/trmnl-firmware) (Deep Sleep, Akkubetrieb) oder an OpenDisplay-Geräte aus. Zulieferer und Paperboy kennen sich nur über HTTP.

Paperboy ist aus dem Display-Service `displayd` von SolarAI hervorgegangen (ADR 0028); die Git-Historie der übernommenen Dateien ist erhalten.

```text
Zulieferer ──PUT /api/images/{profil}/{seite}──▶ paperboy ◀──GET /api/display── Display (TRMNL)
```

## Schnellstart

1. Konfiguration anlegen: `cmd/paperboy/paperboy.example.json` nach `paperboy.json` kopieren, Token setzen und die MAC-Adressen der Displays eintragen (Freigabeliste; unbekannte Geräte werden abgelehnt und mit MAC im Log gemeldet).
2. Starten:
   ```powershell
   go run ./cmd/paperboy -config paperboy.json -data paperboy-data -listen :8090
   # Zulieferer, z. B. SolarAI:
   solarai -host 192.168.1.100 -display-service http://127.0.0.1:8090 -display-token <token>
   ```
   Alternativ als Container: `Dockerfile` und `compose.example.yaml`.
3. Firmware flashen: E1001 über den [Seeed-Wiki-Flasher](https://wiki.seeedstudio.com/reterminal_e10xx_trmnl/) (ab 1.6.7), Sticky über den [Sticky-Playground](https://www.seeedstudio.com/sticky/playground/) (TRMNL). Im WLAN-Einrichtungsportal als Server-URL `http://<rechner>:8090` eintragen.

**Umstieg von `displayd`:** Konfiguration und Datenverzeichnis sind unverändert verwendbar (`-config displayd.json -data displayd-data`); die Geräte brauchen kein neues Setup, solange Adresse und Port gleich bleiben. Die Umgebungsvariable heißt jetzt `PAPERBOY_UPLOAD_TOKEN`; `DISPLAYD_UPLOAD_TOKEN` wird weiterhin akzeptiert.

## Verhalten

- **Profile** (Größe, Graustufen, Panelbreite) bestimmen das Layout; Zulieferer fragen sie über `GET /api/profiles` ab. Mehrere Geräte können ein Profil teilen. Bilder werden als PNG mit 1 Bit bzw. 2 Bit (4 Graustufen) ausgeliefert und unter `-data` gespeichert.
- **Intervall** je Gerät (`refresh`, optional `night_refresh` zwischen `night_start` und `night_end`), mindestens 1 Minute. Unverändertes Bild → das Display zeichnet nicht neu.
- **Taste**: kurzer Druck = nächste Seite, ca. 2 s halten (1–5 s) = Übersicht, länger als 5 s = WLAN zurücksetzen (Firmware). Ein echter Doppelklick löst dieselbe Funktion aus, ist aus dem Tiefschlaf aber kaum zu treffen. Nach `page_timeout` (5 min) zurück zur Übersicht. Drei getrennte Tasten unterstützt die TRMNL-Firmware nicht.
- **Veraltet**: Kommt länger als `stale_after` (30 min) kein Upload, erscheint unten ein schwarzer Balken „Keine neuen Daten seit …“.
- **Akkuwarnung** (ADR 0020): Die TRMNL-Firmware meldet nur die Spannung (`Battery-Voltage`). Mit `"battery": { "warn_volts": 3.45, "critical_volts": 3.3, "hysteresis_volts": 0.1 }` (global oder pro Gerät) bekommt jede Seite unter `warn_volts` den Balken „Akku schwach – bitte laden“; unter `critical_volts` erscheint stattdessen ein Vollbild „Akku fast leer“, Tasten blättern dann nicht. Eine Stufe endet erst `hysteresis_volts` über ihrer Schwelle. Ohne `battery` keine Warnung.
- `GET /api/devices` zeigt registrierte Geräte mit Akkuspannung, Signalstärke, Weckgrund und letzter Abfrage (ohne Schlüssel).
- **OpenDisplay-Geräte** (ADR 0018): Eintrag mit `"type": "opendisplay"`, `"address": "<IP>[:2446]"`, Profil und Intervall; `"buttons": true` hält eine dauerhafte Verbindung und wertet die drei Tasten aus (0 = Übersicht, 1 = zurück, 2 = weiter). Ohne Deep Sleep, nur per TCP. Noch nicht an Hardware getestet.
- **Sicherheit:** Upload nur mit Token (`Authorization: Bearer`, alternativ per `PAPERBOY_UPLOAD_TOKEN`); Geräte per Freigabeliste und eigenem Schlüssel. Paperboy setzt niemals `reset_firmware` und antwortet nie mit Status 500.

## OpenDisplay-Testprogramm

```powershell
# Firmware und Panelkonfiguration lesen
go run ./cmd/odtest -host 192.168.1.50
# Zusätzlich ein Testbild senden (optional -fast, -png vorschau.png)
go run ./cmd/odtest -host 192.168.1.50 -send
# Per Bluetooth: Geräte suchen, dann wie oben per Name oder Adresse
go run ./cmd/odtest -scan
go run ./cmd/odtest -ble ODE46FD8 [-send] [-buttons]
```

Bluetooth (ADR 0016) gibt es nur im Testprogramm. An einer reTerminal Sticky unter Windows dauert ein Vollbild (48 KB) etwa 30 s, über WLAN wenige Sekunden. Nach dem Trennen braucht das Display einige Sekunden, bis es wieder gefunden wird (ggf. `-scan-time 20s`).

## Entwicklung

Benötigt Go 1.27 oder neuer.

```powershell
go test ./...
go vet ./...
.\scripts\build.ps1   # paperboy und odtest für Linux amd64/arm64 und Windows nach bin/
```

```text
cmd/paperboy/            Start und Optionen, Beispielkonfiguration
cmd/odtest/              Testprogramm für OpenDisplay-Geräte (TCP und Bluetooth)
internal/paperboy/       Upload-API, TRMNL-API, OpenDisplay-Pusher, Akkuwarnung
internal/opendisplay/    OpenDisplay-Protokoll; odble = Bluetooth-Transport
docs/adr/                Architekturentscheidungen
```
