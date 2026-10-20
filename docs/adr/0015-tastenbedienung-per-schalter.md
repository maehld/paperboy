# 0015: Tastenbedienung nur per Schalter

- Status: Akzeptiert
- Datum: 2026-10-14
- Löst teilweise ab: ADR 0014 (Aktivierung)

## Kontext

Die Tastenbedienung aus ADR 0014 hält eine dauerhafte Verbindung und fragt alle 500 ms ab. Damit ist das Display für andere Programme belegt und das WLAN ständig aktiv. Eine Alternative mit selteneren Abfragen über den Tastenzähler wird noch erwogen.

## Entscheidung

Die Tastenbedienung ist standardmäßig **aus** und wird mit `-epaper-buttons` eingeschaltet. Ohne den Schalter verhält sich SolarAI wie vor ADR 0014: nur die Übersicht, eine Verbindung pro Aktualisierung, kein Seitenhinweis in der Kopfzeile. Fehlerwiederholung und Erfolgslog gelten in beiden Fällen.

## Konsequenzen

- Displays bleiben zwischen den Aktualisierungen für andere Programme erreichbar.
- Seiten Batterie, Tagesbilanz und Tagesverlauf sind nur mit Schalter erreichbar.
