# 0017: Eigener Display-Service mit TRMNL-Protokoll

- Status: Akzeptiert
- Datum: 2026-10-17
- Löst teilweise ab: ADR 0012 (Anbindung der Displays), ADR 0014 und 0015 (Tastenbedienung im Batteriebetrieb)

## Kontext

Bisher sendet SolarAI Bilder per OpenDisplay aktiv an die Displays (ADR 0012). Für Tasten braucht es eine dauerhafte Verbindung mit Abfrage alle 500 ms (ADR 0014/0015). Beides verhindert Deep Sleep; im Batteriebetrieb halten die Geräte so nicht lange.

Bewertete Alternativen:

- **ESPHome**: Deep Sleep, Wecken über mehrere Tasten und Bildabruf per HTTP sind möglich. Pro Gerät ist eine YAML-Konfiguration samt Build nötig, und das Intervall vom Server zu steuern erfordert Zusatzcode.
- **Eigene Firmware**: volle Kontrolle (z. B. alle Seiten vorladen und ohne WLAN blättern), aber eine neue C++-Codebasis mit dauerhafter Pflege, grob 1–2 Wochen bis zur Zuverlässigkeit.
- **OpenEPaperLink**: braucht einen eigenen Access Point; für wenige Geräte zu aufwendig.
- **TRMNL-Firmware mit eigenem Server (BYOS)**: offiziell für reTerminal E1001 (ab Firmware 1.6.7) und als Build-Variante `seeed_sticky` für die Sticky. Das Gerät wacht auf, ruft `GET /api/display` auf, lädt das Bild (PNG/BMP), zeigt es an und schläft `refresh_rate` Sekunden. Fertige BYOS-Server wie Tesserae oder Terminus sind für unseren Zweck zu umfangreich.

## Entscheidung

1. **Abholmodell mit TRMNL-Firmware.** Die Displays laufen mit unveränderter TRMNL-Firmware. Ein eigener, kleiner Server implementiert nur `/api/setup`, `/api/display`, `/api/log` und die Bildauslieferung.
2. **Eigener Dienst `displayd`** (`cmd/displayd`, `internal/displayd`), getrennt von SolarAI. SolarAI rendert und lädt Bilder per `PUT` hoch, der Display-Service speichert sie auf der Festplatte und bedient die Geräte. Die Dienste kennen sich nur über HTTP und sollen später in getrennten Containern und Repositories laufen können; `internal/displayd` importiert deshalb keine SolarAI-Pakete.
3. **Profile statt Geräte.** Bilder werden je *Profil* (Größe, Graustufen, Panelbreite) und Seite abgelegt. Geräte werden einem Profil zugeordnet. SolarAI fragt die Profile beim Display-Service ab (`GET /api/profiles`).
4. **Konfiguration in einer JSON-Datei**: Upload-Token, Profile, Geräte als Freigabeliste nach MAC-Adresse, Intervall tagsüber/nachts. Unbekannte Geräte werden nicht automatisch aufgenommen. API-Schlüssel der Geräte werden beim ersten Setup erzeugt und im Datenverzeichnis gespeichert.
5. **Tasten über TRMNL-Mittel**: Ein kurzer Druck weckt das Gerät (`Update-Source` ≠ `timer`) und blättert zur nächsten Seite. Ein Doppelklick löst die Spezialfunktion `restart_playlist` aus und führt zur Übersicht. Nach fünf Minuten ohne Tastendruck kehrt das Display zur Übersicht zurück; solange eine andere Seite angezeigt wird, ist das Intervall entsprechend kürzer.
6. **Kein Zurücksetzen durch den Server.** `reset_firmware` wird nie gesetzt. Unbekannte Tokens beantworten wir mit Status `202` statt `500`, weil die Firmware bei `500` ihre Zugangsdaten zurücksetzt.
7. **Veraltete Bilder**: Ist das letzte Upload eines Profils älter als die konfigurierte Frist, setzt der Display-Service einen deutlichen Hinweisbalken ins Bild.
8. Bilder werden als PNG mit Palette (1 Bit bzw. 2 Bit für 4 Graustufen) ausgeliefert. `filename` ist ein Inhalts-Hash, damit das Gerät nur bei Änderungen neu zeichnet.
9. Der bisherige OpenDisplay-Weg (`-epaper`, `internal/opendisplay`, `cmd/odtest`) bleibt vorerst erhalten.

## Konsequenzen

- Displays können im Deep Sleep laufen; SolarAI-Neustarts lassen sie nicht ohne Bild.
- SolarAI braucht keine Gerätedetails (MAC, Token, Tasten, Akku) mehr zu kennen.
- Nur eine Wecktaste mit kurzem Druck/Doppelklick statt drei Tasten; Blättern dauert pro Schritt einige Sekunden (WLAN + Abruf + Refresh). Drei Tasten bräuchten einen Firmware-Fork oder ESPHome.
- Der Upload ist durch ein Token geschützt; die Geräte-API ist nur über die Freigabeliste und gerätespezifische Schlüssel gesichert und läuft per HTTP im LAN.
- Palette-PNG mit 2 Bit sowie Tastenverhalten sind noch an echter Hardware zu prüfen.
