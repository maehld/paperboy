# 0030: Englisch für Lognachrichten, Kommentare und Commits

- Status: Abgelöst durch ADR-0031
- Datum: 2026-10-20

## Kontext

Go-Bezeichner und Code-Kommentare sind bereits englisch, Lognachrichten und Commit-Nachrichten waren bisher deutsch. Mit der Trennung in die öffentlichen Repositories SolarAI und Paperboy (ADR 0029) sollen Logs und Historie auch für nicht deutschsprachige Mitwirkende und beim Suchen nach Fehlermeldungen verständlich sein. Gilt gleichermaßen für SolarAI und Paperboy.

## Entscheidung

- **Englisch:** Go-Bezeichner, Code-Kommentare, Lognachrichten (`slog`-Nachrichten und Attributschlüssel) sowie Git-Commit-Nachrichten (Imperativ, z. B. „Add battery warning“).
- **Deutsch bleibt:** Weboberfläche, Bildtexte auf den Displays, für Nutzer sichtbare Fehlermeldungen in API/Dashboard, Hilfetexte der Optionen, README, ADRs und `AGENTS.md`.
- Bestehende Lognachrichten wurden mit diesem ADR übersetzt; die bisherige Git-Historie bleibt unverändert.

## Konsequenzen

- Log-Auswertungen, die auf deutsche Nachrichtentexte filtern, müssen angepasst werden.
- Neue Beiträge mischen innerhalb einer Ebene keine Sprachen mehr; die Grenze verläuft zwischen Entwickler- (englisch) und Nutzersicht (deutsch).
