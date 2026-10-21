# Paperboy

Paperboy is a small display service for ePaper displays. Suppliers such as [SolarAI](https://github.com/maehld/solarai) upload finished pages as PNG; Paperboy delivers them to devices running the unmodified [TRMNL firmware](https://github.com/usetrmnl/trmnl-firmware) (deep sleep, battery operation) or to OpenDisplay devices. Suppliers and Paperboy know each other only via HTTP.

Paperboy evolved from SolarAI's display service `displayd` (ADR 0028); the Git history of the carried-over files is preserved.

```text
Supplier ──PUT /api/images/{profile}/{page}──▶ paperboy ◀──GET /api/display── Display (TRMNL)
```

## Quick start

1. Create the configuration: copy `cmd/paperboy/paperboy.example.json` to `paperboy.json`, set the token and enter the MAC addresses of the displays (allow list; unknown devices are rejected and reported with their MAC in the log).
2. Start:
   ```powershell
   go run ./cmd/paperboy -config paperboy.json -data paperboy-data -listen :8090
   # Supplier, e.g. SolarAI:
   solarai -host 192.168.1.100 -display-service http://127.0.0.1:8090 -display-token <token>
   ```
   Alternatively as a container: `Dockerfile` and `compose.example.yaml`.
3. Flash the firmware: E1001 via the [Seeed wiki flasher](https://wiki.seeedstudio.com/reterminal_e10xx_trmnl/) (1.6.7 or later), Sticky via the [Sticky playground](https://www.seeedstudio.com/sticky/playground/) (TRMNL). In the Wi-Fi setup portal, enter `http://<computer>:8090` as the server URL.

**Migrating from `displayd`:** Configuration and data directory can be used unchanged (`-config displayd.json -data displayd-data`); the devices need no new setup as long as address and port stay the same. The environment variable is now called `PAPERBOY_UPLOAD_TOKEN`; `DISPLAYD_UPLOAD_TOKEN` is still accepted.

## Behavior

- **Profiles** (size, grayscale levels, panel width) determine the layout; suppliers query them via `GET /api/profiles`. Several devices can share a profile. Images are delivered as PNG with 1 bit or 2 bits (4 gray levels) and stored under `-data`.
- **Interval** per device (`refresh`, optionally `night_refresh` between `night_start` and `night_end`), at least 1 minute. Unchanged image → the display does not redraw.
- **Button**: short press = next page, hold approx. 2 s (1–5 s) = overview, longer than 5 s = reset Wi-Fi (firmware). A real double click triggers the same function, but is hardly achievable from deep sleep. After `page_timeout` (5 min) back to the overview. The TRMNL firmware does not support three separate buttons.
- **Data document** (ADR 0024, first stage): suppliers write JSON branches via `PUT /api/data/{path}` (bearer token; `PUT` replaces instead of merging, missing intermediate objects are created, 409 on type conflicts, 1 MB per write, optional `X-Data-Time` in RFC 3339). `GET` and `DELETE` on the same paths help with debugging. Each path keeps its write time, so stale data can be detected later without comparing content. Page templates that render from the document follow in a later step.
- **Stale**: If no upload arrives for longer than `stale_after` (30 min), a black bar „Keine neuen Daten seit …“ (“No new data since …”) appears at the bottom.
- **Battery warning** (ADR 0020): The TRMNL firmware reports only the voltage (`Battery-Voltage`). With `"battery": { "warn_volts": 3.45, "critical_volts": 3.3, "hysteresis_volts": 0.1 }` (global or per device), every page below `warn_volts` gets the bar „Akku schwach – bitte laden“ (“Battery low – please charge”); below `critical_volts` a full-screen image „Akku fast leer“ (“Battery almost empty”) appears instead, and buttons then do not page. A level ends only `hysteresis_volts` above its threshold. Without `battery` no warning.
- `GET /api/devices` shows registered devices with battery voltage, signal strength, wake reason and last request (without keys).
- **OpenDisplay devices** (ADR 0018): entry with `"type": "opendisplay"`, `"address": "<IP>[:2446]"`, profile and interval; `"buttons": true` keeps a persistent connection and evaluates the three buttons (0 = overview, 1 = back, 2 = next). Without deep sleep, TCP only. Not yet tested on hardware.
- **Security:** Upload only with token (`Authorization: Bearer`, alternatively via `PAPERBOY_UPLOAD_TOKEN`); devices via allow list and their own key. Paperboy never sets `reset_firmware` and never responds with status 500.

## OpenDisplay test program

```powershell
# Read firmware and panel configuration
go run ./cmd/odtest -host 192.168.1.50
# Additionally send a test image (optional -fast, -png preview.png)
go run ./cmd/odtest -host 192.168.1.50 -send
# Via Bluetooth: scan for devices, then as above by name or address
go run ./cmd/odtest -scan
go run ./cmd/odtest -ble ODE46FD8 [-send] [-buttons]
```

Bluetooth (ADR 0016) exists only in the test program. On a reTerminal Sticky under Windows a full image (48 KB) takes about 30 s, via Wi-Fi a few seconds. After disconnecting, the display takes a few seconds until it is found again (if necessary `-scan-time 20s`).

## Development

Requires Go 1.27 or newer.

```powershell
go test ./...
go vet ./...
.\scripts\build.ps1   # paperboy and odtest for Linux amd64/arm64 and Windows into bin/
```

```text
cmd/paperboy/            Startup and options, example configuration
cmd/odtest/              Test program for OpenDisplay devices (TCP and Bluetooth)
internal/paperboy/       Upload API, TRMNL API, OpenDisplay pusher, battery warning
internal/data/           Shared data document for page templates (ADR 0024)
internal/opendisplay/    OpenDisplay protocol; odble = Bluetooth transport
docs/adr/                Architecture decisions
```
