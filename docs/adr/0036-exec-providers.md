# 0036: Exec providers in the data directory

- Status: Accepted
- Date: 2026-10-22
- Extends: ADR 0032 (data providers and regions), ADR 0026 (YAML configuration)
- Related: ADR 0024 (data document, no external calls from the service), ADR 0031 (English), ADR 0033 (language and timezone)

## Context

ADR 0032 settled that providers are **processes** that push JSON into the data document, and that SolarAI is one of them. The first provider that does not come from SolarAI is weather. Two shapes were on the table:

1. **A second service** like SolarAI: its own binary, container and configuration, polling a weather API and pushing `PUT /api/data/weather`.
2. **A simple data plugin model inside Paperboy**: configured commands whose JSON output is written into the document.

Constraints:

- ADR 0024 keeps Paperboy out of the internet: the service itself never calls external APIs and holds no source-specific knowledge.
- Paperboy is a single static binary, cross-built for linux/amd64, linux/arm64 and windows/amd64 (ADR 0032). Go's `plugin` package stays rejected (CGO, exact toolchain, no Windows).
- A weather provider is small: fetch one JSON, map it, write ~a few KB. As a separate service it brings a second image, a second configuration with Paperboy URL and token, and a second thing to monitor and update.
- Data, pages and plans already live in the data directory (ADR 0024, 0025) and are operator-owned files. The same should hold for provider programs.
- Run time, write timestamps, staleness handling, logging and the "never an error page" rule already exist in the service.

## Decision

We add **exec providers**. A provider is a program in the providers directory that prints one JSON value; Paperboy writes that value into the data document.

### 1. Declaration

```yaml
directories:
  providers: providers        # default <data>/providers

providers:
  weather:
    command: [weather, -lat, "52.52", -lon, "13.40"]
    path: weather             # data branch this provider owns
    interval: 15m             # default 15m, must not be shorter than 10s
    timeout: 30s              # default 30s, must not exceed interval
    env: { OPEN_METEO_KEY: "..." }   # optional, added to the inherited environment
```

- The map key is the provider name (a-z, 0-9, `-`); it only names the provider in logs.
- `command` is an argument vector, **never** a shell string: no globbing, no pipes, no quoting rules.
- The first element is resolved like a program name: a relative path (or a bare name) is looked up in the providers directory first, then on `PATH`. On Windows the usual `.exe` lookup applies.
- The working directory of a provider is the providers directory, so a provider may ship data files next to itself.
- Environment: the inherited environment plus `env`, and the Paperboy settings every provider needs:
  `PAPERBOY_LANGUAGE` (`de`/`en`) and `PAPERBOY_TIMEZONE` (IANA name). A provider never takes its own language or timezone; both are global (ADR 0033), so all texts on a page agree.
- `path` follows the data document rules (see below). One branch has exactly one owner (ADR 0032, decision 5); two providers naming the same path or one naming a parent of the other is a configuration error.
- There are no per-provider manifest files: the declaration lives in `paperboy.yaml` next to the devices, so the set of programs that may run stays in the trusted configuration file.

### 2. Run protocol

1. Paperboy starts every provider once shortly after startup, then every `interval`.
2. Standard output is the new value of the `path` branch, at most 1 MB. It must be valid JSON; the top level of the root path must be an object.
3. Exit code 0 and valid JSON → the value replaces the branch with `PUT` semantics. The write timestamp is the **data time**: if the value is an object with a top-level `updated` string in RFC 3339, that time is used, otherwise the run time. So a provider that returns an observation which lags behind the fetch stays visibly older, exactly as with `X-Data-Time` of the HTTP API (ADR 0024); future times are capped at the run time. `updated` stays in the data, so templates can show it.
4. Non-zero exit, timeout, oversized or invalid output, or a conflict in the document → the run is logged with the truncated stderr and **the previous data stays untouched**. Its age grows, the stale notice appears, devices keep their pages.
5. stderr is logged, stdout is not stored anywhere else. A provider never talks to Paperboy over HTTP for this; the write is in process and needs no token.
6. Runs of one provider never overlap; a slow run is cut off by `timeout`.
7. There is no provider API: success and failure appear only in the log, the first failure and the recovery each once per streak. `GET /api/devices` stays about displays, and `GET /api/data/{path}` already shows the result and its age.

### 3. Relation to the HTTP data API

- Providers outside the machine keep using `PUT /api/data/{path}` with the token. The data model is the same; exec and push are deployment choices, not two formats.
- Unlike a pushed write, an exec provider needs no token, no open port and no loopback request, and it cannot overwrite branches it does not own through a typo in the URL.

### 4. Weather as the first provider

`cmd/weather` maps [Open-Meteo](https://open-meteo.com/) (no API key, free for non-commercial use) to the branch `weather`:

```json
{
  "updated": "2026-10-22T09:45:00Z",
  "current": {"time": "...", "temp": 12.4, "apparent": 10.9, "humidity": 78,
              "precip": 0.0, "precip_prob": 20, "wind": 14.0, "beaufort": 3,
              "code": 3, "is_day": true, "symbol": "cloud", "text": "Bewölkt"},
  "today":   {"min": 8.1, "max": 15.2, "precip_prob": 40,
              "sunrise": "...", "sunset": "..."},
  "days":    [{"date": "2026-10-22", "label": "Do", "min": 8.1, "max": 15.2,
               "precip_prob": 40, "code": 61, "text": "Leichter Regen"}],
  "hours":   [{"time": "...", "values": {"temp": 12.1, "precip_prob": 20}}]
}
```

- The output is the branch value, exactly like the body of `PUT /api/data/weather`.
- `hours` covers the local day of the location (midnight to midnight) and uses the point shape of `solar/history/points`, so the existing `chart` block plots it unchanged.
- The chart connects samples whose timestamps are at most 90 minutes apart; hourly forecast data therefore draws a continuous curve while a real outage stays a gap.
- The provider delivers the WMO `code`, a short text and the weekday label in the global language (`PAPERBOY_LANGUAGE`). Page templates stay language-neutral (ADR 0033) and cannot map codes without a helper, and a `weather_text` helper would put source-specific knowledge into the service.
- `current.symbol` names the pictogram of the condition (`sun`, `cloud`, `rain`, `snow`, `thunder`) and `current.beaufort` the wind force, so a page can pick an icon from `icons/` and compare a wind force without knowing the units of the source (ADR 0038).
- Times are RFC 3339 in UTC; templates and the chart convert to the configured timezone.
- Daily aggregation uses the timezone of the location, so "today" means the local day at the place, not the Paperboy day.

## Consequences

- One binary and one container remain: weather arrives by dropping a program into `<data>/providers` and adding a few lines to `paperboy.yaml`.
- Paperboy still makes no external calls, so ADR 0024 holds; the provider is a separate process with its own failure domain.
- The same seam later serves calendar, transit or RSS data without touching the core.
- **Running configured programs is code execution.** Anyone who can edit `paperboy.yaml` or drop a file into the providers directory can run code as the Paperboy user. This is accepted because both are operator-owned, but it is documented prominently and the directory belongs to the same trust boundary as the token.
- The provider program must exist in the image (Linux) or next to the data directory (Windows); a shell script additionally needs an interpreter in the container. Missing programs are logged per interval, not fatal at startup.
- Configuration reloading is still open; providers are read at startup like the rest of `paperboy.yaml`.
- The provider owns its branch but not its visual representation: no icons, no font or palette knowledge. A weather page is text, tiles, bars and a temperature chart; a pictogram or icon block would need its own ADR.
- The data directory is now executable content, so backups and container volumes that carry it carry programs too.
- Providers depend on two environment variables (`PAPERBOY_LANGUAGE`, `PAPERBOY_TIMEZONE`); a provider that ignores them produces texts or days that disagree with the rest of the page.

## Open questions

1. Does a provider need a command-line convention for its output beyond `updated`, e.g. an explicit `schema` or `source` key?
2. Should the providers directory be validated at startup (existing program, executable bit) and reported once, instead of failing on the first run?

## Resolved open questions

1. **Data time:** yes. A provider may report its own data time as a top-level `updated` RFC 3339 string in its output; Paperboy then uses that time for the write timestamp instead of the run time. Without it the run time applies, and future times are capped.
2. **Health state:** no. Providers report success and failure only in the log (first failure and recovery per streak); `GET /api/data/{path}` already shows the result and its age.
3. **Manifests per provider:** no. The declaration stays in `paperboy.yaml`, where the trusted set of programs belongs; one fewer file format and no second place for interval and path.
4. **Language:** no per-provider setting. The language (and timezone) is global for all of Paperboy (ADR 0033) and reaches every provider as `PAPERBOY_LANGUAGE` / `PAPERBOY_TIMEZONE`, so `command` lines stay free of duplicated settings.
