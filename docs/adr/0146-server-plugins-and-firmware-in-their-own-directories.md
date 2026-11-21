# 0146: Server, plugins and firmware in their own directories

- Status: Accepted, implemented
- Date: 2026-11-21
- Builds on: ADR 0028 (own repository), ADR 0031 (English), ADR 0056 (one repository and release trains), ADR 0075 (plugin versions), ADR 0107 (plugin builds reach installations)
- Answers: issue #364

## Context

All Go code lived in one module. `cmd/` held the server and the plugins, `pkg/` held the shared protocol and drawing, and `internal/` held the service. One module forced three deliverables into one build unit, although a release ships them separately ([ADR 0056](0056-repository-layout-and-release-trains.md)).

The plugins and the server share the plugin protocol and the drawing. The server and the plugins do not import each other. The firmware shares no code at all.

`pkg/` carries no meaning. The name comes from GOPATH, and Go gives the directory no role. A module named after its content is easier to read.

## Decision

The repository keeps one history and one issue tracker. It gets one directory per deliverable:

| Directory | Go module | Content |
|---|---|---|
| `server/` | `github.com/maehld/paperboy/server` | The service: startup, configuration, device API, operator UI, plans and downloads |
| `plugins/` | `github.com/maehld/paperboy/plugins` | The shipped plugin programs under `cmd/<plugin>/`, each with its own `VERSION` |
| `sdk/` | `github.com/maehld/paperboy/sdk` | The public plugin protocol under `plugin/` and the shared drawing under `render/` |
| `firmware/` | none | The ESPHome sources, unchanged |

`go.work` binds the three modules for local builds. `server/go.mod` and `plugins/go.mod` require the SDK and replace it with `../sdk`, so each module also builds on its own.

The shipped plugins stay in one module. They share the protocol, they release together as one zip per platform, and no plugin imports another. A split into one module per plugin stays mechanical, because each plugin is already a separate program with its own version. The repository itself stays together ([ADR 0056](0056-repository-layout-and-release-trains.md)).

`internal/imagelimit` moves to `plugins/internal/imagelimit`. Only plugins use it.

The move changes the import paths of the SDK. Every plugin source and every plugin binary therefore changes, so every plugin version rose in the same commit ([ADR 0075](0075-plugin-versions.md), [ADR 0107](0107-plugin-builds-reach-installations.md)).

This ADR replaces the "one Go module" part of [ADR 0056](0056-repository-layout-and-release-trains.md). The release trains, the assets and the rule against splitting the repository stay.

## Consequences

- The instance directory, the device API, the plugin protocol and the release asset names do not change.
- Tests, vet, formatting and the cross-build name the modules: `go test ./server/... ./plugins/... ./sdk/...`. `go build ./...` at the root does not expand a workspace.
- The scripts follow the new paths. The version check reads `plugins/cmd/<plugin>/VERSION` and warns about changes under `sdk/`.
- The container build copies the workspace and the three module files before the first dependency download.
- A later per-plugin split or an SDK for third parties starts from a clean boundary. The protocol module can get its own tags without a second repository.
