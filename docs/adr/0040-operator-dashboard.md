# 0040: Operator dashboard as a Go template

- Status: Accepted
- Date: 2026-10-30
- Extends: ADR 0039 (device status in the devices API)
- Related: ADR 0031 (everything in the software is English), ADR 0026 (configuration)

## Context

After ADR 0039 the devices API answers "which displays are alive and what are they showing", but only as JSON. The operator wants to see that on a phone or a wall tablet next to a preview of the current panel content.

Two implementations were considered:

- **A JavaScript frontend** (framework or a small hand-written app) that fetches `/api/devices`. That needs a build step, Node in the image, a bundler or a vendored file, and a second implementation of the status logic. Paperboy currently depends on three libraries, each with its own ADR.
- **Server-rendered HTML** with the standard library's `html/template`. No dependency, no build, no JavaScript; the template is embedded in the binary.

The page is read-only, has no client state and shows the same data the API already computes, so the second option carries the work and the first only the tooling.

## Decision

Paperboy serves a read-only dashboard rendered on the server.

- `GET /` answers with a redirect to `/ui`; `GET /ui` returns the page. All other unknown paths stay 404.
- The template lives in `internal/paperboy/ui/index.html`, is embedded with `go:embed` and executed with `html/template`, so every value is escaped. Styles are inline; there is no external asset and no script.
- The handler reuses `deviceInfos()` from the devices API. Status, page, URL, refresh and error therefore have exactly one implementation.
- One card per device: ID and status badge, plan plus active rule, preview image, what is shown and since when, refresh, last contact and battery. The last render or plan error appears as a red line.
- The preview is the **last served image** (`last_page_url`), not a fresh render. A live preview would show content the panel never received and would render on every page view.
- Refresh is a 30-second `<meta http-equiv="refresh">`. That is enough for a status board and keeps the page script-free; live updates would be a later enhancement on top of `/api/devices`.
- Texts are English like the rest of the software (ADR 0031); the dashboard is an operator tool, not a device page, so it does not use the configured `language` or the built-in display texts.
- No token: the page shows the same data as the unauthenticated devices API and the page images. It exposes no keys.
- The header names the running build: the module version (or `dev` for an untagged build), the short commit with `+dirty` when the worktree was modified, and the commit date in the configured timezone, with the Go toolchain in a tooltip. The values come from `runtime/debug.ReadBuildInfo`, so no `-ldflags`, version file or release process is needed.

## Consequences

- The instance gets a usable overview without any frontend dependency; the binary stays self-contained and the Docker image unchanged.
- The dashboard cannot break rendering or the data API: it only reads a state snapshot, like the devices API.
- A later JavaScript view can consume `/api/devices` without replacing this page, so progressive enhancement stays possible.
- The page renders for every request. With the state read under the lock and no rendering, this is cheap; if instance sizes ever make it matter, caching the HTML would be a separate decision.
