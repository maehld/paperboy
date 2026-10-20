# Projektwissen und Arbeitsregeln

## Ziel und Kommunikation

Paperboy ist ein Display-Dienst für ePaper-Anzeigen (TRMNL-Firmware mit Deep Sleep, OpenDisplay). Zulieferer wie [SolarAI](https://github.com/maehld/solarai) laden Seiten als PNG hoch; Paperboy liefert sie an die Geräte aus. Hervorgegangen aus dem Display-Service `displayd` von SolarAI (ADR 0028, 0029).

- Mit dem Nutzer auf Deutsch kommunizieren. Dokumentation und Bildtexte sind deutsch; Go-Bezeichner, Kommentare, Lognachrichten und Commits sind englisch (ADR 0030).
- Änderungen an Struktur und Benennung an den vorhandenen Code anpassen.
- Größere Anfragen (z. B. Funktionen oder Optionen entfernen, mehrere Dateien/ADRs betroffen) vor der Umsetzung kurz zusammenfassen (was wird geändert, was bleibt, Risiken) und erst nach Bestätigung durch den Nutzer durchführen.
- Paperboy importiert keine SolarAI-Pakete; Zulieferer und Paperboy sprechen nur per HTTP miteinander.

## Versionierung und Entscheidungen

- Git-Repository `github.com/maehld/paperboy`, Branch `main`. Nach **jedem abgeschlossenen Arbeitsschritt** einen Commit erstellen, mit **englischer** Commit-Nachricht im Imperativ (ADR 0030). Vor dem Commit die passenden Prüfungen ausführen.
- Wesentliche Entscheidungen als ADR unter `docs/adr/` dokumentieren (Vorlage `0000-vorlage.md`, Index `docs/adr/README.md` pflegen). ADR 0012–0030 stammen aus SolarAI (0030 gilt für beide); neue ADRs ab 0031. Bestehende ADRs nicht umschreiben, sondern durch neue ablösen.
- `paperboy.json`/`displayd.json` (enthalten Token), `paperboy-data/`/`displayd-data/` und `.env` sind per `.gitignore` ausgeschlossen und dürfen nicht committet oder gelöscht werden.

## Architektur

Go **1.27 oder neuer**. Abhängigkeiten: `golang.org/x/image` **v0.24.0** (ADR 0013) und `tinygo.org/x/bluetooth` **v0.16.0** nur im Paket `internal/opendisplay/odble` (ADR 0016; das Paperboy-Binary enthält es nicht). Weitere Abhängigkeiten nur mit eigenem ADR.

| Pfad | Verantwortung |
|---|---|
| `cmd/paperboy/` | Start und Optionen (`-config paperboy.json`, `-data paperboy-data`, `-listen`), Beispiel `paperboy.example.json` |
| `internal/paperboy/` | JSON-Konfiguration (Token, Profile, Geräte-Freigabeliste nach MAC, Intervalle tags/nachts). Upload `PUT /api/images/{profil}/{seite}` (Bearer-Token oder `PAPERBOY_UPLOAD_TOKEN`, früher `DISPLAYD_UPLOAD_TOKEN`, weiterhin akzeptiert; Seiten lückenlos ab 0, PNG wird auf 1-/2-Bit-Palette normalisiert und atomar unter `-data` gespeichert). TRMNL-API: `/api/setup` (nur bekannte MAC, Schlüssel in `state.json`), `/api/display` (`filename` = Inhalts-Hash, `refresh_rate`, Taste: `Update-Source` ≠ timer → nächste Seite, Header `special_function` → Übersicht über `restart_playlist`, nach `page_timeout` zurück), `/api/log`, `/images/{key}.png`, `GET /api/profiles`, `GET /api/devices`. Veraltete Uploads (`stale_after`) bekommen einen Hinweisbalken |
| `internal/paperboy/battery.go` | **Akkuwarnung (ADR 0020):** optionaler Abschnitt `battery` (global/pro Gerät: `warn_volts` → Balken, `critical_volts` → Vollbild ohne Blättern, `hysteresis_volts`), Stufe in `state.json` (`battery_level`), Dateiname enthält Stufe + Spannung in 0,1 V (`-low34`, `<profil>-battery-33`) |
| `internal/paperboy/opendisplay.go` | **OpenDisplay (ADR 0018):** Gerät mit `"type": "opendisplay"`, `address`, optional `buttons`; `pusher` nur per TCP, Zustandsschlüssel `opendisplay:<name>`, keine MAC/TRMNL-API. Noch nicht an Hardware getestet |
| `internal/opendisplay/` | OpenDisplay-Sender (Rahmen, Konfiguration lesen, Direct Write, Pixelformate S/W und 4 Graustufen); unverschlüsselt. Transport austauschbar über `Transport`: TCP eingebaut, BLE in `odble` (GATT-Dienst/Characteristic `0x2446`, Payload = MTU − 3) |
| `cmd/odtest/` | Testprogramm: `go run ./cmd/odtest -host <IP> [-send] [-buttons]`, per Bluetooth `-scan` bzw. `-ble <Name>` |
| `Dockerfile`, `compose.example.yaml` | Container-Image `paperboy` |

## Unverzichtbare Regeln für TRMNL-Geräte

- **Nie `reset_firmware` setzen und nie Status 500 senden** – die Firmware löscht sonst ihre Zugangsdaten. Unbekannte Schlüssel → Status 202.
- Geräte speichern Server-Adresse, API-Schlüssel und MAC. Adresse/Port und `state.json` beibehalten, sonst ist ein neues Setup nötig.

## Hardware-Erkenntnisse

- **E1001** (MAC 44:1B:F6:85:AA:1C, TRMNL-Firmware, bestätigt 2026-10-17): Setup, Abruf und Blättern per Taste funktionieren; Tastendruck meldet `Update-Source: EXT0`, Akkuspannung z. B. `4.08`. Taste 1–5 s halten (`BUTTON_MEDIUM_HOLD_TIME` 1000 ms, wie Doppelklick) sendet `special_function` → Übersicht; echter Doppelklick aus dem Tiefschlaf praktisch nicht auslösbar; > 5 s halten setzt WLAN zurück. 4 Graustufen sehen gut aus. OpenDisplay: ID 0 = grüne Taste, 1 = links daneben, 2 = links davon.
- **Sticky** (MAC B8:1F:3F:D8:6A:FC, TRMNL) ebenso bestätigt: Taste meldet `EXT0`, Blättern und Halten → Übersicht funktionieren. Der erste Abruf nach dem Setup meldet ebenfalls `EXT0` und blättert deshalb nicht (`LastSeen` leer).
- **Akku:** Sticky „Küche“ 3,40 V nach längerer Zeit ohne Laden, E1001 „Wohnzimmer“ 3,75 V; Schwankung ca. 0,02 V.
- **OpenDisplay-Firmware** (`cmd/odtest`): BLE unter Windows an Sticky ODE46FD8 erfolgreich (Konfiguration lesen, Testbild 48 KB in ca. 31–33 s; nach dem Trennen einige Sekunden nicht auffindbar). WLAN an reTerminal Sticky und E1001 erfolgreich (Testbild in nativer Ausrichtung). `-buttons` fragt den 16-Byte-Statusblock (`0x0044`) zyklisch ab; Sticky (10.1.2.133): Taste auf Statusbyte 5, Touch Bytes 0–4. Byte 5 zeigt die zuletzt gedrückte Taste (ID 0 = obere Taste, 1 = Seite hoch, 2 = Seite runter) mit **eigenem 4-Bit-Zähler je Taste**; kurze Drücke oft nur am Zähler sichtbar → Druck = Änderung von (ID, Zähler). Touch: Kontakte 1–5 = berührt, 6 = losgelassen (Koordinaten bleiben stehen); Touch-Flags 0b111 (invert x/y, swap); Koordinatenbereich noch nicht kalibriert.

## Geplante Arbeiten

ADR 0024–0027 sind akzeptiert, aber noch nicht umgesetzt: Datendokument statt Bilder mit YAML-Layouts, Pläne je Display, YAML-Konfiguration (`paperboy.yaml`, dann Migration von `state.json`), Textdisplays und HTTP-Geräte. Für das Rendern werden dann Zeichenfunktionen und Schrift aus SolarAIs `internal/epaper` übernommen.

## Start und Prüfungen

```powershell
go run ./cmd/paperboy -config paperboy.json -data paperboy-data -listen :8090
go test ./...
go vet ./...
.\scripts\build.ps1   # paperboy und odtest nach bin/<os>-<arch>/
```

- `gofmt` auf geänderte Go-Dateien anwenden.
- Tests für Protokollverhalten und Zustandsübergänge bevorzugen.

Dieses Dokument bei relevanten Architektur- oder Verhaltensänderungen mitpflegen.
