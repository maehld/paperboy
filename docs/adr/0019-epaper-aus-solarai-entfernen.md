# 0019: ePaper-Sender aus SolarAI entfernen

- Status: Akzeptiert
- Datum: 2026-10-17
- Löst ab: ADR 0018, Punkt 6
- Löst teilweise ab: ADR 0012, 0014 und 0015 (OpenDisplay-Sender in SolarAI)

## Kontext

Seit ADR 0018 kann der Display-Service OpenDisplay-Geräte beschicken. Der ältere Sender in SolarAI (`-epaper`, `-epaper-interval`, `-epaper-buttons`) war als veraltet markiert; die Tastenlogik existierte doppelt.

## Entscheidung

1. Der OpenDisplay-Sender und die Optionen `-epaper`, `-epaper-interval` und `-epaper-buttons` werden aus SolarAI entfernt.
2. SolarAI rendert ePaper-Seiten nur noch für den Display-Service (`-display-service`, ADR 0017). OpenDisplay-Geräte werden dort mit `"type": "opendisplay"` eingetragen.
3. `internal/opendisplay` und `cmd/odtest` bleiben bestehen (Display-Service und Testprogramm).

## Konsequenzen

- Die Tastenlogik für OpenDisplay gibt es nur noch im Display-Service.
- Wer bisher `-epaper` nutzte, muss den Display-Service betreiben.
- Die OpenDisplay-Anbindung im Display-Service ist noch nicht an Hardware getestet.
