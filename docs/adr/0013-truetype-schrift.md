# 0013: TrueType-Schrift über golang.org/x/image

- Status: Akzeptiert
- Datum: 2026-10-14
- Löst teilweise ab: ADR 0002 (nur Standardbibliothek)

## Kontext

Für die ePaper-Anzeige (ADR 0012) muss SolarAI Text rastern. Die Go-Standardbibliothek kann keine Schriften darstellen. Der Nutzer hat sich für TrueType statt einer eigenen Pixelschrift entschieden.

## Entscheidung

- Einzige externe Abhängigkeit: `golang.org/x/image` (Go-Projekt, BSD-Lizenz), Pakete `font/opentype` und `font/gofont` (Go Regular/Bold, im Binary eingebettet; keine Schriftdateien zur Laufzeit).
- Version **v0.24.0**, weil neuere Versionen Go 1.23+ voraussetzen; die Mindestversion Go 1.22 bleibt. `golang.org/x/text` erscheint nur als indirekte Modulabhängigkeit und wird nicht eingebunden.
- Weiterhin kein Frontend-Build, kein CDN, ein einzelnes Binary.

## Konsequenzen

- Gut lesbare, skalierbare Schrift inklusive Umlauten, „—“, „≈“ und „°“.
- Binary wächst um die eingebetteten Schriften (Größenordnung 0,5 MB).
- Abhängigkeit muss gelegentlich aktualisiert werden; ein Update über v0.24.0 hinaus hebt die Mindestversion von Go an und erfordert ein neues ADR.
