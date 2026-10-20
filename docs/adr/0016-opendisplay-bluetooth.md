# 0016: OpenDisplay per Bluetooth über tinygo.org/x/bluetooth

- Status: Akzeptiert
- Datum: 2026-10-14

## Kontext

ADR 0012 hat BLE ausgeschlossen, weil dafür eine externe Abhängigkeit nötig ist. Die OpenDisplay-Befehle sind über BLE und LAN byte-identisch: Jeder TCP-Rahmen entspricht einem Schreibvorgang auf die GATT-Characteristic `0x2446` (Dienst `0x2446`), jede Antwort einer Benachrichtigung. Bluetooth würde Displays ohne WLAN-Einrichtung bzw. ohne dauerhaft aktives WLAN ermöglichen.

Die Go-Standardbibliothek bietet keinen Bluetooth-Zugriff. `tinygo.org/x/bluetooth` unterstützt trotz des Namens normales Go unter Windows (WinRT), Linux (BlueZ/D-Bus) und macOS. Version v0.16.0 verlangt **Go 1.25**; der Nutzer hat die Anhebung der Mindestversion freigegeben.

## Entscheidung

- `internal/opendisplay` erhält die Schnittstelle `Transport` (Payload schreiben, Antwort-Payload lesen, maximale Payloadgröße). TCP ist der eingebaute Transport; Protokolllogik (Konfiguration, Upload, Status) bleibt unverändert und gemeinsam.
- BLE liegt im eigenen Paket `internal/opendisplay/odble` mit `tinygo.org/x/bluetooth` v0.16.0. Nur Programme, die dieses Paket importieren, enthalten die Abhängigkeit; das SolarAI-Binary bleibt vorerst frei davon.
- Zunächst nur im Testprogramm: `odtest -scan`, `odtest -ble <Name|Adresse>`. Gefunden werden Geräte mit Namenspräfix `OD` oder Dienst `0x2446`.
- Payloadgröße aus der ausgehandelten MTU (`MTU − 3`, höchstens 512), sonst 20 Byte. Schreiben standardmäßig mit Bestätigung; `-ble-noresponse` optional.
- Go-Mindestversion steigt auf 1.25.

## Ergebnis erster Test (Windows, reTerminal Sticky ODE46FD8)

- Suche, Verbindung, Firmware (2.27.0) und Konfiguration lesen erfolgreich; 253 Byte Payload je Befehl.
- Testbild 48.000 Byte in 251-Byte-Blöcken: 32,5 s mit Bestätigung, 30,9 s ohne – begrenzt durch das Protokoll-ACK je Block.
- Direkt nach dem Trennen war das Gerät einige Sekunden nicht auffindbar.

## Konsequenzen

- Zusätzliche, umfangreiche Abhängigkeit (u. a. WinRT-, D-Bus-Bindings) – nur im BLE-Paket.
- BLE ist rund eine Größenordnung langsamer als WLAN; für seltene Aktualisierungen ausreichend, für Tastenbedienung mit Seitenwechsel nur eingeschränkt.
- Einbau in `internal/epaper` (z. B. `-epaper ble:<Name>`) ist eine eigene spätere Entscheidung.
- Unter Linux sind BlueZ und passende Rechte nötig; dort noch ungetestet.
