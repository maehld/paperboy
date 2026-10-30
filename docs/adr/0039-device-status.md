# 0039: Device status in the devices API

- Status: Accepted
- Date: 2026-10-30
- Extends: ADR 0026 (devices in the configuration), ADR 0025 (plans)
- Related: ADR 0037 (state directory), ADR 0032 (providers and pages)

## Context

An operator wants to know which displays are alive and what each of them is showing right now. The data was partly there, partly misleading:

- `GET /api/devices` already listed `last_seen`, battery, signal strength and the wake reason, but nothing about the content.
- `page` in `state/devices.json` is the index inside the active plan rule, not the name of a template. It is also set before rendering, so a broken template showed a page that never reached the display.
- TRMNL devices deep-sleep and poll. There is no connection that could be opened or closed, so "online" can only mean "the last request is not overdue".
- A failure (missing plan, broken template) only appeared in the server log. Nobody looking at the API could tell that a display had been showing a notice page for hours.

A separate `GET /api/status` with an aggregate view was considered and rejected: the per-device data belongs next to the devices it describes, and the flat list is what a later UI will consume.

## Decision

We extend `GET /api/devices`; there is no second endpoint and no new authentication (the endpoint stays readable without a token, like before).

### 1. Persisted content per device

`state/devices.json` gains six fields, written after rendering and outside the state lock, so the recorded page is the one the device was actually pointed at:

| Field | Meaning |
|---|---|
| `last_page` | template name of the rendered page; empty for a notice or the battery warning |
| `last_page_kind` | `page`, `notice` or `battery` |
| `last_page_hash` | content hash of the PNG, identical to the `filename` of the device response |
| `last_page_time` | time of the request that served it |
| `last_refresh` | reported `refresh_rate` in seconds |
| `last_error` | last render or plan error, kept until a request succeeds again |

`last_error` is persisted on purpose: the interesting case is a display that has been stuck on a notice page since yesterday, and a restart must not hide it. Errors are cut to one line and 200 characters.

### 2. Derived values in the response

The response additionally carries:

- `last_page_url`: the image path below the server root (`/pages/{profile}/{page}/{hash}.png` or `/hints/{hash}.png`). It is derived, not stored, because the profile comes from the configuration.
- `status`: `ok`, `late`, `never` or `unregistered`. `never` means registered but no `/api/display` yet, `late` means the last request is more than twice the reported refresh old (falling back to the configured refresh). Twice the interval absorbs one missed cycle; an idle device is not an offline device.

## Consequences

- The devices API answers the two operational questions directly: which displays are alive and what they show. A missing plan, a broken template or a panicking render is visible as `last_page_kind: notice` plus `last_error` instead of being buried in the log.
- `devices.json` grows by a few fields per device; older files stay valid because missing fields are empty.
- A display request writes the state file twice: once before rendering (`last_seen`, battery) and once after (content). The second write is what makes the recorded page honest.
- `last_page_url` points at the newest render for that profile and page, so an outdated hash still resolves (ADR 0024 behaviour).
- A UI on top of this API (HTML overview) is not part of this decision.
