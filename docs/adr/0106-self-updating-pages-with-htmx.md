# 0106: Self-updating operator pages with htmx

- Status: Accepted
- Date: 2026-11-15
- Related: ADR 0040 (operator dashboard), ADR 0068 (page editor without a build step), ADR 0096 (UI password), ADR 0097 (page downloads take the device key), ADR 0104 (runtime stats)

## Context

The dashboard, the stats page and the downloads page refreshed themselves with `<meta http-equiv="refresh">`, 30, 10 and 3/60 seconds (ADR 0040). A meta refresh is a full document reload, so every tick

- closed an opened *Details* block on a device card and pushed everything below it up,
- dropped focus, text selection and the scroll position,
- fetched every preview again, because `GET /api/devices/{id}/image` answered `Cache-Control: no-store`, and left the card empty until the picture arrived,
- rebuilt tables whose row count changes, which moved the content under the cursor.

The measurement that started this was a device card whose *Details* block collapsed every 30 seconds. ADR 0040 had called live updates "a later enhancement on top of `/api/devices`"; this is that enhancement.

The options were a framework (React), a hypermedia library (htmx) or a few lines of hand-written DOM patching. React would bring a build step, a bundler and a second rendering world into a UI that is server-rendered Go templates with embedded vanilla JavaScript (ADR 0068). Hand-written patching (fetch the page, parse, replace regions) is maybe a hundred lines, but it has to solve exactly the problems a morphing library already solves: node identity, focus, selection, `<details>`, moved nodes.

htmx 4 (September 2026) integrates the idiomorph algorithm as `hx-swap="innerMorph"`, so a response is merged into the existing DOM: nodes with the same `id` keep their identity, and with it focus, selection and scroll position. That is the whole feature this case needs, and it costs one vendored file.

## Decision

1. **The three pages poll themselves.** Each wraps its changing part in a region `id="live"` that carries `hx-get` (its own URL), `hx-select="#live > *"`, `hx-swap="innerMorph"` and `hx-trigger="every 30s|10s|5s [document.visibilityState == 'visible'], visibilitychange from:document [document.visibilityState == 'visible']"`. The request is the page again, so no endpoint is added and the templates stay one document. Morphing only the region keeps the header, the navigation and every attribute on the polling element untouched.
2. **Hidden tabs do not poll**, and a tab that becomes visible again refreshes at once. The intervals stay what they were for the dashboard (30 s) and the stats page (10 s); the downloads page uses one interval of 5 s instead of 3 s while a check runs and 60 s otherwise, because an htmx trigger is fixed once the element is initialised.
3. **The downloads poll keeps the page's query string** (`Self`), so a notice it was opened with stays. The cleanup preview does not poll at all: its list of files to delete must not change under the button next to it.
4. **The device cards, their *Details* block and their preview carry stable ids** (`device-<id>`, `details-<id>`, `preview-<id>`), because a morph matches nodes by id. `htmx.config.morphIgnore` keeps the `open` attribute, so an opened *Details* block survives a tick; without it the server's markup would close it again.
5. **A served picture names its pixel hash**: the dashboard links `GET /api/devices/{id}/image?h=<hash>`, and the route answers `Cache-Control: private, max-age=31536000, immutable` when the hash is the current one, `no-store` otherwise. A tick therefore downloads no image at all, and only a new page changes the URL. `last_page_url` in `/api/devices` keeps its shape.
6. **htmx is vendored, not linked**: `internal/paperboy/ui/vendor/htmx-4.0.1.min.js` (37 448 bytes, 13 KB gzipped, 0BSD) is embedded with `go:embed`, served as `GET /ui/htmx.js` behind the UI password and revalidated with `no-cache`. Its version, source URL, SHA-256 and licence sit next to it in `ui/vendor/README.md` and `htmx.LICENSE`. An upgrade replaces the file and the embed line — there is no package manager and no build step.
7. **Without JavaScript the pages simply stop updating.** They stay readable and every link works; they were read-only before and are read-only now.

## Alternatives

- **React**: a build step, a bundle and client-side rendering for three read-only pages, plus a second source of truth next to the Go templates (ADR 0068). Rejected.
- **htmx 2 with the idiomorph extension**: works the same way in principle, but 2.x ships without morphing and the two parts would be vendored separately. htmx 4 is a fresh major release (4.0.1); the trade-off is a young version against the integration we would otherwise write ourselves. Vendoring pins it, so only a deliberate upgrade moves.
- **Hand-written patching**: no dependency, but it re-implements node matching, focus/selection handling and the `<details>` case, and every later page would need the same care.
- **Server-sent events or WebSockets**: push instead of poll, and more moving parts (a long-lived connection per tab, a broadcast on every state change) for pages that nobody watches continuously.
- **An ETag on the image instead of a hash in the URL**: fewer changes to the dashboard, but every tick would still make a conditional request; the hash URL is already how pages are named (`/pages/{profile}/{page}/{hash}.png`).

## Consequences

- A tick costs one request for the page, no image, and no repaint of unchanged nodes. The polled pages render the same template as before, so a card looks exactly as it did on a reload.
- The UI now ships foreign JavaScript, the only such file. `ui/vendor/` is the single place for it, and a new file there needs an ADR.
- `morphIgnore` contains `open` for the whole UI: a page that ever wants a details block closed by the server has to say so in its own markup or reset it in a script.
- Everything that is not inside `id="live"` (the header, the build label) stays as it was when the page was opened; a running build change still needs a reload.
