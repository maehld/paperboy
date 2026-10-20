# 0028: Display-Service als „Paperboy“ in eigenem Repository

- Status: Akzeptiert, Schritt 2 umgesetzt durch ADR-0029
- Datum: 2026-10-20

## Kontext

Der Display-Service (`cmd/displayd`, `internal/displayd`, ADR 0017) war von Anfang an als eigenständiger Dienst gedacht: Er importiert keine SolarAI-Pakete und soll später ein eigenes Repository und einen eigenen Container bekommen. Mit ADR 0024–0027 wird er zu einem allgemeinen Dienst (Datendokument, Vorlagen, Pläne, mehrere Geräteprotokolle), der fachlich nichts mehr mit Solar zu tun hat. Der Name „displayd“ ist zudem sehr allgemein.

Die Konfiguration bricht mit ADR 0026 ohnehin; das ist der richtige Zeitpunkt für Umbenennung und Trennung.

## Entscheidung (Vorschlag)

### 1. Name

Der Dienst heißt **Paperboy** (er „trägt“ Inhalte an die Displays aus).

| Bisher | Neu |
|---|---|
| `cmd/displayd`, Binary `displayd` | `cmd/paperboy`, Binary `paperboy` |
| `internal/displayd` | `internal/paperboy` bzw. fachliche Unterpakete |
| `displayd.json` | `paperboy.yaml` (ADR 0026) |
| `displayd-data/` | `paperboy-data/` (Standard für `directories.data`) |
| `SOLARAI_DISPLAY_TOKEN` (Zulieferer) | `PAPERBOY_UPLOAD_TOKEN` |
| Container `APP=displayd` | eigenes Image `paperboy` |

In SolarAI heißt die Anbindung weiterhin `-display-service`/`-display-token` oder wird bei der Umstellung auf das Datendokument in `-paperboy-url`/`-paperboy-token` umbenannt (bei der Umsetzung entscheiden).

### 2. Eigenes Git-Repository

- Paperboy bekommt ein eigenes Repository `paperboy` mit eigenem Go-Modul. Die Modulpfad-Basis (z. B. `github.com/<konto>/paperboy`) wird beim Anlegen festgelegt.
- **Umfang:**
  - `cmd/displayd` → `cmd/paperboy`, `internal/displayd` → `internal/…`
  - `internal/opendisplay` inkl. `odble` und `cmd/odtest` (OpenDisplay wird nur noch von Paperboy genutzt, ADR 0019); damit wandert auch die Abhängigkeit `tinygo.org/x/bluetooth` aus SolarAI heraus.
  - Zeichenfunktionen und Schrift aus `internal/epaper` (ADR 0024), `golang.org/x/image`.
  - `Dockerfile`/`compose`-Teile für Paperboy.
- **Historie:** Die Git-Historie der übernommenen Pfade wird mitgenommen (z. B. `git filter-repo --path …`), damit Herkunft und Begründungen nachvollziehbar bleiben.
- **ADRs:** Paperboy beginnt eine eigene ADR-Reihe. Die für Paperboy relevanten ADRs (0012–0020, 0024–0028) werden als Kopie mit Hinweis auf ihre Herkunft übernommen; in SolarAI bleiben sie unverändert und werden als „in Paperboy weitergeführt“ gekennzeichnet.
- SolarAI behält danach nur den Zulieferer (Schreiben ins Datendokument per HTTP) und keine Display-Pakete mehr. `internal/epaper` entfällt in SolarAI, sobald SolarAI keine Bilder mehr hochlädt (ADR 0024, Abschnitt 6).

### 3. Reihenfolge

1. ADRs 0024–0028 akzeptieren.
2. Repository anlegen und Code mit Historie übernehmen (reine Verschiebung, Umbenennung, Build/Tests grün). SolarAI verweist im README auf Paperboy.
3. Umbau in Paperboy (ADR 0024–0027) dort entwickeln.
4. SolarAI auf das Datendokument umstellen und Bild-Upload sowie `internal/epaper`, `internal/displayd`, `internal/opendisplay`, `cmd/displayd`, `cmd/odtest` entfernen.

### 4. Bestehende Geräte

- TRMNL-Geräte speichern Server-Adresse, eigenen API-Schlüssel und MAC. Damit sie **kein neues Setup** brauchen, muss Paperboy unter derselben Adresse und demselben Port erreichbar sein und die Schlüssel aus dem alten `state.json` übernehmen (einmaliges Migrationskommando, z. B. `paperboy migrate-state <alt> <neu>`, das MAC-Einträge den neuen Geräte-IDs aus `paperboy.yaml` zuordnet).
- Weiterhin gilt: nie `reset_firmware`, nie Status 500 (ADR 0017).

## Konsequenzen

- Klare Trennung: SolarAI ist Wechselrichter-Monitoring, Paperboy ist ein allgemeiner Display-Dienst; beide sprechen nur per HTTP miteinander.
- Zwei Repositories, zwei Release-/Build-Abläufe; `scripts/build.ps1` und `Dockerfile` müssen aufgeteilt werden.
- Änderungen am Datenformat des Zweigs `solar` betreffen beide Repos (Vorlagen in Paperboy-Konfiguration, Zulieferer in SolarAI); das Format wird in SolarAI dokumentiert.
- `AGENTS.md` in SolarAI wird deutlich kürzer; Paperboy braucht ein eigenes `AGENTS.md` mit den Display-spezifischen Hardware-Erkenntnissen (E1001, Sticky, Tasten, Akku).
- Aufwandsschätzung: Repository-Trennung inkl. Historie, Umbenennung und Build ca. ½–1 Tag, Zustandsmigration ½ Tag.

## Offene Fragen

1. Wo liegt das Repository (Konto/Organisation, öffentlich oder privat) und wie lautet damit der Modulpfad?
2. Bekommt SolarAI dabei ebenfalls ein Remote-Repository (bisher keines eingerichtet)?
