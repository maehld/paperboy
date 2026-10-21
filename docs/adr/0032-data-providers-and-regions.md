# 0032: Data providers with named regions instead of image fragments

- Status: Accepted
- Date: 2026-10-21
- Extends: ADR 0024 (data document, page templates), ADR 0027 (text displays)
- Related: ADR 0017 (own display service, no SolarAI imports), ADR 0026 (YAML configuration)

## Context

ADR 0024 plans a shared data document plus YAML page templates rendered by Paperboy. The open question was whether data providers should instead be **plugins** that deliver **image fragments**, which Paperboy only composes.

Constraints that shape the answer:

- TRMNL devices fetch one complete PNG per wake-up; there is no partial refresh. Fragments never reach the device, so composing is always server-side.
- Panels differ in size, gray levels and physical width; ADR 0027 adds text displays (e.g. 20x4 characters). A fragment in pixels is tied to one display type.
- Paperboy is one static binary, cross-built for linux/amd64, linux/arm64 and windows/amd64 and run in an Alpine container without CGO.
- Suppliers and Paperboy talk only via HTTP (ADR 0017); no SolarAI packages are imported.
- Additional dependencies require their own ADR (AGENTS.md); Go's `plugin` package needs CGO, the exact toolchain and does not support Windows.

## Evaluation

A throwaway spike in a worktree compared both models on the same page ("solar overview" with header, four tiles and a history chart):

- **regions:** one shared JSON document (`solar`, `wetter` branches); Paperboy owns a skeleton with named regions (`kopf`, `kacheln`, `verlauf`) and renders each region from the data.
- **fragments:** each provider renders its own region as PNG and uploads it per display type; Paperboy pastes the fragments into the same regions.
- Displays: `e1001` (800x480, 4 gray levels), `sticky` (800x480, 4 gray levels), `klein` (400x300, 1 bit), `lcd20x4` (text).
- Criteria: artifacts and bytes per page, determinism, redraws on unchanged data, quality across display types, multi-provider composition, text displays.

Results (measured, reproducible with the spike command):

| Criterion | regions | fragments |
|---|---|---|
| Artifacts for 3 image displays | 1 skeleton + 1 document | 9 uploads (3 displays x 3 regions) |
| Transferred bytes | 1,049 (all displays) | 8,408 |
| Deterministic preview | yes | yes |
| Distinct hashes after 10 identical provider writes | 1 | 1 |
| Render time per preview | 0.5–1 ms | 2–8 ms |
| Text display | 4 lines from the same document | no artifact possible |

Findings:

1. Determinism and redraws do not separate the models: the existing content hash (`filename`) works in both.
2. Artifacts grow with **display types**, not with content. Fragments for `e1001` scaled down to `klein` break strokes and are unreadable; native `klein` fragments look fine, so the cost is the artifact count.
3. Fragments cannot share a region: the header needs weather text (one provider) and the solar timestamp (another). In the fragment model exactly one provider must own the whole header.
4. Independently rendered regions do not align: each provider brings its own padding and separators, visible as double lines and shifted text.
5. `e1001` and `sticky` are pixel-identical (both 800x480) but differ physically (`width_mm`), which matters as soon as layouts use it.
6. A text display has no sensible fragment equivalent.

Limits of the evaluation: the spike used a hard-coded skeleton instead of YAML, no per-path timestamps or staleness, and fragment providers used the same renderer as the service (own padding only). This favours fragments, so the measured quality gap is a lower bound.

## Decision

1. **Providers are processes, not Go plugins.** A provider is an external service that pushes JSON to Paperboy, as SolarAI does today. Rejected: Go plugins (`plugin` package), compile-time provider packages in Paperboy (breaks the service's neutrality), and a pull model where Paperboy calls provider APIs (ADR 0024 keeps external calls out of Paperboy).
2. **Pages are an operator-owned skeleton with named regions.** The skeleton defines the grid and the region names for a page and display type. Providers fill regions with data or with declarative blocks from the ADR 0024 vocabulary (`header`, `tiles`, `rows`, `chart`, `text`, `bar`); they never deliver pixels.
3. **Image fragments are rejected as the default model.** They remain an explicit escape hatch only: a fragment must be tied to the display types it supports, and a missing artifact for a display type is a configuration error, not a silent scaling. Whether that hatch is needed at all is decided when a concrete page cannot be expressed with blocks.
4. **Rendering stays in the service:** fonts, palettes (1 bit / 4 gray), quantization, content hashes, staleness banners, battery warnings and caching per (page, display type). Text displays are rendered from the same data document, not from fragments.
5. **One region, one owner.** A provider may write several regions, but a region is filled by exactly one provider, so fragments of different providers never mix inside a slot. Regions that combine data from several sources (like the header above) are the operator's skeleton, not a provider artifact.

## Consequences

- Layouts stay consistent across displays and remain changeable without touching providers; new display types (including text) need no provider work.
- Providers stay simple: push JSON on their own schedule, no pixel rendering, no font or palette knowledge.
- The plugin question is settled without new dependencies or build complexity; the single static binary and cross-build stay as they are.
- Image fragments and Go plugins can be revisited only with a new ADR that shows a page blocks cannot express.
- The open work of ADR 0024–0027 remains: data document with timestamps, templates with regions, plans, text and HTTP devices. Region ownership must be expressible in the YAML configuration (ADR 0026).
- A provider that renders its own charts loses control over typeface, margins and dithering; charts become a block type of the service instead.

## Open questions

1. Does a page template name the provider per region, or is ownership derived from the written data paths (`watch`)?
2. Is the fragment escape hatch needed for externally rendered charts, or do the block types cover every real page?
