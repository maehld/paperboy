# 0029: Repository-Trennung SolarAI / Paperboy umgesetzt

- Status: Akzeptiert
- Datum: 2026-10-20

## Kontext

ADR 0028 sieht vor, den Display-Service als „Paperboy“ in ein eigenes Repository auszugliedern. Die Repositories `github.com/maehld/solarai` und `github.com/maehld/paperboy` sind angelegt; damit sind die offenen Fragen aus ADR 0028 beantwortet.

## Entscheidung

- **Modulpfade:** `github.com/maehld/solarai` und `github.com/maehld/paperboy`.
- **Paperboy** übernimmt mit Git-Historie (`git filter-repo`) `cmd/displayd` → `cmd/paperboy`, `internal/displayd` → `internal/paperboy`, `internal/opendisplay` (inkl. `odble`) und `cmd/odtest`. Abhängigkeiten dort: `golang.org/x/image`, `tinygo.org/x/bluetooth`.
- Umbenennungen in Paperboy: Standard-Konfiguration `paperboy.json`, Datenverzeichnis `paperboy-data`, Umgebungsvariable `PAPERBOY_UPLOAD_TOKEN` (`DISPLAYD_UPLOAD_TOKEN` wird weiter akzeptiert), Platzhaltertext „Paperboy“. Konfigurationsformat und Zustandsdatei bleiben unverändert (JSON); bestehende Konfiguration und `state.json` sind direkt weiterverwendbar, die Geräte brauchen kein neues Setup. Damit entfällt das in ADR 0028 skizzierte Migrationskommando vorerst; es wird erst mit dem YAML-Umbau (ADR 0026) nötig.
- **ADRs:** Paperboy enthält Kopien der für ihn relevanten ADRs (0012–0020, 0024–0029) mit ihren ursprünglichen Nummern, damit Querverweise gültig bleiben. Neue ADRs beider Repositories werden ab 0030 unabhängig voneinander nummeriert.
- **SolarAI** entfernt `cmd/displayd`, `internal/displayd`, `internal/opendisplay`, `cmd/odtest` und die Abhängigkeit `tinygo.org/x/bluetooth`. `internal/epaper` mit dem Bild-Upload (`-display-service`, `-display-token`) bleibt, bis SolarAI auf das Datendokument (ADR 0024) umgestellt ist. `Dockerfile`, `compose.example.yaml` und `scripts/build.ps1` bauen nur noch SolarAI.

## Konsequenzen

- ADR 0024–0027 sind akzeptiert; ihre Umsetzung erfolgt in Paperboy.
- Die Historie in SolarAI bleibt vollständig; die verschobenen Pfade sind dort ab diesem Commit gelöscht.
- Display-spezifisches Hardwarewissen (E1001, Sticky, Tasten, Akku) steht künftig in `AGENTS.md` von Paperboy.
