# 0018: OpenDisplay im Display-Service

- Status: Akzeptiert
- Datum: 2026-10-17
- Ergänzt: ADR 0017
- Löst teilweise ab: ADR 0012, 0014 und 0015 (Ort der OpenDisplay-Anbindung)

## Kontext

Mit ADR 0017 bedient der Display-Service TRMNL-Geräte im Abholmodell. OpenDisplay-Geräte werden dagegen weiterhin von SolarAI selbst beschickt (`-epaper`). Damit gibt es zwei Orte mit Geräteverwaltung, und die beiden Ansätze lassen sich nicht unter gleichen Bedingungen vergleichen.

## Entscheidung

1. Der Display-Service kann zusätzlich OpenDisplay-Geräte beschicken. In der Konfiguration bekommt ein Gerät `"type": "opendisplay"`, `address` (Port 2446 als Standard) und optional `buttons`.
2. Die Logik entspricht dem bisherigen Sender aus SolarAI: Aktualisierung im Geräte-Intervall (tags/nachts), unverändertes Bild wird nicht erneut gesendet, schneller Refresh auf S/W-Panels mit Vollrefresh nach elf schnellen, mit `buttons` dauerhafte Verbindung und Tastenabfrage alle 500 ms (Taste 0 = Übersicht, 1 = zurück, 2 = weiter), Rückkehr zur Übersicht nach `page_timeout`.
3. Gezeigt werden dieselben hochgeladenen Seiten und derselbe Hinweisbalken für veraltete Bilder wie bei TRMNL. Die Größe des Profils muss zum Panel passen.
4. Nur TCP; Bluetooth bleibt dem Testprogramm `cmd/odtest` vorbehalten (ADR 0016).
5. `internal/displayd` darf `internal/opendisplay` verwenden. Das Paket hängt von keinem SolarAI-Paket ab und wandert bei einer Trennung der Repositories mit dem Display-Service.
6. `-epaper` in SolarAI bleibt vorerst bestehen, gilt aber als veraltet und soll nach erfolgreichem Test im Display-Service entfernt werden.

## Konsequenzen

- Beide Implementierungen (TRMNL-Abholmodell und OpenDisplay-Push) laufen im selben Dienst mit denselben Bildern und lassen sich direkt vergleichen.
- Die Tastenlogik für OpenDisplay ist vorübergehend doppelt vorhanden (SolarAI und Display-Service).
- Ein Gerät kann nur mit einer Firmware laufen; zum Vergleich muss ein Display wieder mit OpenDisplay geflasht werden.
