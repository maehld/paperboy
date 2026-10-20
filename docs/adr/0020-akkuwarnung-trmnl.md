# 0020: Akkuwarnung für TRMNL-Geräte

- Status: Akzeptiert
- Datum: 2026-10-19

## Kontext

Die TRMNL-Firmware misst die Akkuspannung und sendet sie bei jedem Abruf im Header `Battery-Voltage`, warnt selbst aber nicht. Den Bildschirm „Low battery“ liefert die TRMNL-Cloud aus, die der Display-Service ersetzt. Ohne eigene Warnung entlädt sich ein Gerät unbemerkt, bis die Schutzschaltung bei etwa 2,75–3 V abschaltet, und das letzte Bild bleibt stehen. Beobachtet: Sticky „Küche“ nach längerer Zeit ohne Laden bei 3,40 V, E1001 bei 3,75 V. Die Werte schwanken zwischen Abrufen nur um etwa 0,02 V und werden unter WLAN-Last gemessen.

## Entscheidung

1. Optionaler Abschnitt `battery` (global, pro Gerät überschreibbar) mit `warn_volts`, `critical_volts` und `hysteresis_volts` (Standard 0,1 V). Ohne Abschnitt keine Warnung. Empfohlene Werte 3,45 V und 3,3 V; die Spannungsteiler der Geräte sind nicht kalibriert.
2. Stufe `warn`: Jede Seite bekommt unten den Balken „Akku schwach – bitte laden (ca. x,x V)“, zusätzlich zum Veraltet-Balken. Blättern bleibt möglich.
3. Stufe `critical`: Statt der Seiten erscheint ein Vollbild „Akku fast leer“. Tasten blättern nicht; das Abrufintervall bleibt das normale.
4. Eine Stufe beginnt unter ihrer Schwelle und endet erst `hysteresis_volts` darüber. Fehlende oder ungültige Spannungen (`-1`) ändern die Stufe nicht. Die Stufe wird in `state.json` gespeichert (`battery_level`).
5. Der Dateiname enthält Stufe und Spannung auf 0,1 V gerundet, damit das Gerät beim Wechsel neu lädt, aber nicht bei jeder kleinen Schwankung.

## Konsequenzen

- Gilt nur für TRMNL-Geräte; OpenDisplay meldet keine Spannung.
- Die Prozentangabe der TRMNL-Cloud wird nicht nachgebildet; Schwellen sind Spannungen.
- Schwellen müssen eventuell je Gerät an der Hardware nachjustiert werden.
