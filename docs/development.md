[← Paperboy](../README.md)

# Development

Requires Go 1.27 or newer.

```powershell
go test ./...
go vet ./...
.\scripts\build.ps1             # paperboy and the plugins for Linux amd64/arm64 and Windows into bin/
./scripts/package-plugins.sh dist  # the plugin zip for the host platform (GOOS/GOARCH select another)
```

The dashboard header shows version, commit and commit date. The commit and its date come from Go's VCS stamping, which works as long as the Git repository is present. The container image therefore keeps `.git` in the build context and installs the git binary in its build stage (the alpine Go image ships none).

The version name cannot come from VCS stamping. Go stamps any working-tree build with a pseudo-version derived from the commit (`v0.0.0-…`), even when the exact commit is tagged. Release builds therefore pass the tag in: `-ldflags "-X github.com/maehld/paperboy/internal/paperboy.version=v2.0.0"`. `release.yml`, the container image (`--build-arg VERSION=…`) and `scripts/build.ps1 -Version` do this. A build without it reports `dev` plus the short commit. A build without a repository or with `-buildvcs=false` reports `dev`. `go version -m <binary>` shows what was stamped.

The workflows build the three deliverables ([ADR 0056](adr/0056-repository-layout-and-release-trains.md)). *CI* runs formatting, vet, tests and a cross-build on every change. *Plugins* and *Firmware* build their artifacts; the firmware only when `firmware/esphome/` changes. *Docker* publishes the image, and *Release* collects everything into one GitHub release on a tag.

## Repository layout

```text
cmd/paperboy/            Startup, options and example configuration
cmd/weather/             Weather plugin: Open-Meteo (ADR 0049)
cmd/calendar/            Calendar plugin: CalDAV and iCalendar (ADR 0048, 0049)
cmd/evcc/                evcc plugin: energy flow, charging point, forecast, history, chart, values
cmd/xkcd/                xkcd plugin: comic with optional title and alt text
cmd/countdown/           Countdown plugin: days to a date with a progress bar
cmd/departures/          Departures plugin: EFA real-time departures (ADR 0053)
cmd/myskoda/             MyŠkoda plugin: vehicle state and commands
cmd/dexcom/              Dexcom plugin: glucose readings from the Share service
cmd/pokedex/             Pokédex plugin: a Pokémon card from PokéAPI
cmd/rss/                 RSS and Atom plugin: feed list and headline
pkg/plugin/              Plugin protocol and Go helper (public API)
pkg/render/              Fonts, palettes and drawing shared by Paperboy and plugins (public API)
internal/paperboy/       YAML configuration, device API, plans, battery warning, dashboard
internal/page/           Pages: grid parsing and composition from widgets
internal/widget/         Plugin processes, widget cache and background refresh
internal/builtin/        Built-in plugin "paperboy": header, text, clock
internal/plan/           Display plans: time rules that select pages per device (ADR 0025)
internal/download/       Release downloads and the verification against SHA256SUMS
internal/semver/         Version grammar for plugin and firmware names (ADR 0089)
firmware/esphome/        Paperboy firmware on ESPHome for Sticky and E1001 (ADR 0041)
docs/design/             How the system works today, one document per area
docs/glossary.md         The terms that all documents use
docs/adr/                Architecture decisions
```

## Writing documentation

English documents follow the rules of ASD-STE100 as the house style. This does not claim formal conformance to the standard. A sentence has at most 25 words, and the active voice and the present tense carry the text. Steps are imperative, one term stands for one concept, and a noun cluster has at most three words. The [glossary](glossary.md) fixes the terms; a new word for a known thing is a bug.

`./scripts/check-docs.sh` checks the part that needs no dictionary: sentence length and filler words. It covers the design documents and the glossary by default and takes other files as arguments.

Each fact lives in exactly one place. The [design documents](design/README.md) keep the structure and the reason; the topic pages keep the steps; the [ADRs](adr/README.md) keep the history and are not rewritten.
