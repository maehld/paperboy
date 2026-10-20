# 0014: Tastenbedienung der ePaper-Anzeigen

- Status: Teilweise abgelöst durch ADR-0015 (nur noch per -epaper-buttons aktiv)
- Datum: 2026-10-14
- Ergänzt: ADR 0012

## Kontext

reTerminal Sticky und E1001 melden drei Tasten im 16-Byte-Statusblock, der über WLAN per `0x0044` lesbar ist. Messungen mit `odtest -buttons`:

- Ein Statusbyte (laut Konfiguration, Paket `0x25`) zeigt die **zuletzt gedrückte** Taste: Bits 0–2 ID, Bits 3–6 ein **eigener Zähler je Taste**, Bit 7 „gedrückt“.
- Kurze Drücke sind bei 500 ms Abfrage oft nur am Zähler erkennbar.
- IDs: 0 = Sticky obere Taste / E1001 grüne Taste, 1 = Sticky Seite hoch / E1001 links daneben, 2 = Sticky Seite runter / E1001 weiter links.
- Das Gerät bedient nur einen Client und trennt untätige Verbindungen.

## Entscheidung

- Hat das Display Tasten in der Konfiguration, hält SolarAI **eine dauerhafte Verbindung** und fragt den Statusblock alle 500 ms ab; Bilder gehen über dieselbe Verbindung. Ohne Tasten bleibt es bei einer Verbindung pro Aktualisierung.
- Ein Druck ist jede Änderung des Paares (ID, Zähler) gegenüber der letzten Abfrage; die erste Abfrage nach dem Verbinden ist nur Ausgangswert.
- Vier Seiten: Übersicht, Batterie, Tagesbilanz, Tagesverlauf. Taste 0 = Übersicht/sofort aktualisieren, 1 = vorherige, 2 = nächste Seite (zyklisch). Nach 5 min ohne Eingabe zurück zur Übersicht.
- Seitenwechsel lösen sofort ein Bild aus (Monochrom schnell, 4 Graustufen voll). Die Kopfzeile zeigt „Seite n/4“.
- Fehler: Verbindung schließen, erneuter Versuch nach 30 s, verdoppelt bis zum Aktualisierungsintervall. Erfolgreiche Übertragungen und Tastendrücke werden protokolliert.
- Touch (Sticky) wird vorerst nicht ausgewertet; Koordinatenbereich ist noch nicht kalibriert.
- Tasten steuern ausschließlich die Anzeige; der Wechselrichter bleibt nur lesend (ADR 0003).

## Konsequenzen

- Reaktion auf Tasten innerhalb von etwa 0,5 s plus Bildaufbau.
- Solange SolarAI läuft, können sich andere Programme (z. B. `odtest`) nicht mit Displays mit Tasten verbinden.
- Dauerhafte WLAN-Aktivität erhöht den Stromverbrauch; Displays sollten am Netzteil betrieben werden.
- Mehr als 15 Drücke derselben Taste zwischen zwei Abfragen würden wegen des 4-Bit-Zählers übersehen; praktisch irrelevant.
