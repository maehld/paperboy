[← Paperboy](../README.md)

# Development

Requires Go 1.27 or newer.

```powershell
go test ./server/... ./plugins/... ./sdk/...
go vet ./server/... ./plugins/... ./sdk/...
.\scripts\build.ps1             # paperboy and the plugins for Linux amd64/arm64 and Windows into bin/
./scripts/package-plugins.sh dist  # the plugin zip for the host platform (GOOS/GOARCH select another)
```

The repository has three Go modules ([ADR 0146](adr/0146-server-plugins-and-firmware-in-their-own-directories.md)). `go.work` binds them, so commands from the repository root work. `go build ./...` at the root does not expand a workspace; name the modules instead.

The dashboard header shows version, commit and commit date. The commit and its date come from Go's VCS stamping, which works as long as the Git repository is present. The container image therefore keeps `.git` in the build context and installs the git binary in its build stage (the alpine Go image ships none).

The version name cannot come from VCS stamping. Go stamps any working-tree build with a pseudo-version derived from the commit (`v0.0.0-…`), even when the exact commit is tagged. Release builds therefore pass the tag in: `-ldflags "-X github.com/maehld/paperboy/server/internal/paperboy.version=v2.0.0"`. `release.yml`, the container image (`--build-arg VERSION=…`) and `scripts/build.ps1 -Version` do this. A build without it reports `dev` plus the short commit. A build without a repository or with `-buildvcs=false` reports `dev`. `go version -m <binary>` shows what was stamped.

The workflows build the three deliverables ([ADR 0056](adr/0056-repository-layout-and-release-trains.md), [ADR 0147](adr/0147-separate-server-plugin-and-firmware-releases.md)). *CI* runs formatting, vet, tests and a cross-build on every change. *Plugins* and *Firmware* build their artifacts only for changes in their sources. The firmware builds only the affected hardware types; a release builds all four. *Docker* checks the image on pull requests, and *Release* builds the train of the pushed tag, publishes the image for a server release and attaches the assets to one GitHub release ([ADR 0148](adr/0148-the-container-image-is-the-primary-server-artifact.md)).

## Repository layout

```text
go.work                          Workspace that binds the three Go modules (ADR 0146)
server/                          Go module github.com/maehld/paperboy/server
server/cmd/paperboy/             Startup, options and example configuration
server/internal/paperboy/        YAML configuration, device API, plans, battery warning, dashboard
server/internal/page/            Pages: grid parsing and composition from widgets
server/internal/widget/          Plugin processes, widget cache and background refresh
server/internal/builtin/         Built-in plugin "paperboy": header, text, clock
server/internal/plan/            Display plans: time rules that select pages per device (ADR 0025)
server/internal/download/        Release downloads and the verification against SHA256SUMS
server/internal/semver/          Version grammar for plugin and firmware names (ADR 0089)
plugins/                         Go module github.com/maehld/paperboy/plugins
plugins/cmd/weather/             Weather plugin: Open-Meteo (ADR 0049)
plugins/cmd/fuel/                Fuel plugin: Tankerkönig prices and their history
plugins/cmd/calendar/            Calendar plugin: CalDAV and iCalendar (ADR 0048, 0049)
plugins/cmd/evcc/                evcc plugin: energy flow, charging point, forecast, history, chart, values
plugins/cmd/xkcd/                xkcd plugin: comic with optional title and alt text
plugins/cmd/countdown/           Countdown plugin: days to a date with a progress bar
plugins/cmd/departures/          Departures plugin: EFA real-time departures (ADR 0053)
plugins/cmd/myskoda/             MyŠkoda plugin: vehicle state and commands
plugins/cmd/dexcom/              Dexcom plugin: glucose readings from the Share service
plugins/cmd/pokedex/             Pokédex plugin: a Pokémon card from PokéAPI
plugins/cmd/rss/                 RSS and Atom plugin: feed list and headline
plugins/cmd/github/              GitHub plugin: issues, pull requests, workflow runs and commits
plugins/cmd/prusalink/           PrusaLink plugin: state, progress and temperatures of a Prusa printer
plugins/internal/imagelimit/     Pixel budget for an image from an untrusted source
sdk/                             Go module github.com/maehld/paperboy/sdk
sdk/plugin/                      Plugin protocol and Go helper (public API)
sdk/render/                      Fonts, palettes and drawing shared by Paperboy and plugins (public API)
firmware/esphome/                Paperboy firmware on ESPHome for Sticky and E1001 (ADR 0041)
docs/design/                     How the system works today, one document per area
docs/glossary.md                 The terms that all documents use
docs/adr/                        Architecture decisions
```

## Writing documentation

English documents follow the rules of ASD-STE100 as the house style. This does not claim formal conformance to the standard. A sentence has at most 25 words, and the active voice and the present tense carry the text. Steps are imperative, one term stands for one concept, and a noun cluster has at most three words. The [glossary](glossary.md) fixes the terms; a new word for a known thing is a bug.

`./scripts/check-docs.sh` checks the part that needs no dictionary: sentence length and filler words. It covers the design documents and the glossary by default and takes other files as arguments.

`./scripts/screenshots.sh` renders the pictures of the plugin pages into `docs/images/plugins/`. It builds Paperboy and the plugins, copies the examples into a temporary instance and starts a mock of every upstream service, so it needs no network and no credentials. `-only <plugin>` renders one plugin, `-keep` keeps the temporary instance and its log, and `-bin <dir>` reuses built programs. The xkcd page and the Pokémon card show drawn placeholder art, because the repository ships neither the comic nor the sprites.

Each fact lives in exactly one place. The [design documents](design/README.md) keep the structure and the reason; the topic pages keep the steps; the [ADRs](adr/README.md) keep the history and are not rewritten.
