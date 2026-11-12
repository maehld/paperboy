[← Paperboy](../README.md)

# Development

Requires Go 1.27 or newer.

```powershell
go test ./...
go vet ./...
.\scripts\build.ps1             # paperboy, odtest and the plugins for Linux amd64/arm64 and Windows into bin/
./scripts/package-plugins.sh dist  # the plugin zip for the host platform (GOOS/GOARCH select another)
```

The dashboard header shows version, commit and commit date. The commit and its date come from Go's VCS stamping, which works as long as the Git repository is present. The container image therefore keeps `.git` in the build context and installs the git binary in its build stage (the alpine Go image ships none).

The version name cannot come from VCS stamping. Go stamps any working-tree build with a pseudo-version derived from the commit (`v0.0.0-…`), even when the exact commit is tagged. Release builds therefore pass the tag in: `-ldflags "-X github.com/maehld/paperboy/internal/paperboy.version=v2.0.0"`. `release.yml`, the container image (`--build-arg VERSION=…`) and `scripts/build.ps1 -Version` do this. A build without it reports `dev` plus the short commit. A build without a repository or with `-buildvcs=false` reports `dev`. `go version -m <binary>` shows what was stamped.

The workflows build the three deliverables (ADR 0056). *CI* runs formatting, vet, tests and a cross-build on every change. *Plugins* and *Firmware* build their artifacts; the firmware only when `firmware/esphome/` changes. *Docker* publishes the image, and *Release* collects everything into one GitHub release on a tag.

## Repository layout

```text
cmd/paperboy/            Startup, options and example configuration
cmd/weather/             Weather plugin: Open-Meteo (ADR 0049)
cmd/calendar/            Calendar plugin: CalDAV and iCalendar (ADR 0048, 0049)
cmd/evcc/                evcc plugin: energy flow, charging point, forecast
cmd/xkcd/                xkcd plugin: comic with optional title and alt text
cmd/countdown/           Countdown plugin: days to a date with a progress bar
cmd/departures/          Departures plugin: EFA real-time departures (ADR 0053)
cmd/myskoda/             MyŠkoda plugin: vehicle state and commands
cmd/pokedex/             Pokédex plugin: a Pokémon card from PokéAPI
cmd/rss/                 RSS and Atom plugin: feed list and headline
cmd/odtest/              Test program for OpenDisplay devices (TCP and Bluetooth)
pkg/plugin/              Plugin protocol and Go helper (public API)
pkg/render/              Fonts, palettes and drawing shared by Paperboy and plugins (public API)
internal/paperboy/       YAML configuration, device API, plans, battery warning, dashboard
internal/page/           Pages: grid parsing and composition from widgets
internal/widget/         Plugin processes, widget cache and background refresh
internal/builtin/        Built-in plugin "paperboy": header, text, clock
internal/plan/           Display plans: time rules that select pages per device (ADR 0025)
internal/download/       Release downloads and the verification against SHA256SUMS
internal/semver/         Version grammar for plugin and firmware names (ADR 0089)
internal/opendisplay/    OpenDisplay protocol; odble = Bluetooth transport
firmware/esphome/        Paperboy firmware on ESPHome for Sticky and E1001 (ADR 0041)
docs/adr/                Architecture decisions
```
