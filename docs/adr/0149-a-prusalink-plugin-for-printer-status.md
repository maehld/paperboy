# 0149: A PrusaLink plugin for 3D printer status

- Status: Accepted, implemented
- Date: 2026-11-21
- Builds on: ADR 0049 (plugin widgets), 0075 (plugin versions), 0111 (values for text pages), 0126 (schedule event), 0127 (plan conditions are values)
- Answers: issue #365

## Context

A CORE One prints parts for the workshop. A display next to it should show
whether the printer runs, how far the job is, when it ends and how hot the
nozzle and the bed are.

Prusa offers two services with similar names. Prusa Connect is the cloud at
`connect.prusa3d.com`. It has no public, documented API for its users: the
web and mobile apps use an undocumented API, and the printer-scoped key only
uploads files for PrusaSlicer. PrusaLink runs on the printer itself and is
documented as OpenAPI. It answers the printer state, the job and the
temperatures over HTTP. A second, self-hosted Prusa Connect server exists as a
public beta, and its API is still 0.x.

PrusaLink protects the API with HTTP Digest: the user is `maker`, the password
is the API key from the printer's network settings. It also accepts the key in
the `X-Api-Key` header. The API sends no push and no events, so the plugin
polls, and it reads only while a display needs the data (ADR 0126).

## Decision

1. One plugin `prusalink` reads the local PrusaLink API of one printer. It adds
   no Go dependency. The configuration names the address and the API key; the
   key stays out of `paperboy.yaml` through `${PRUSALINK_API_KEY}`.
2. One fetch reads `/api/v1/status`. While the status names a job, it also
   reads `/api/v1/job` for the file name; the endpoint answers 204 without a
   job. `/api/v1/info` is read once for the printer name.
3. The first request carries the key as `X-Api-Key`. A `401` switches the
   plugin to Digest for this and every later request, with the challenge
   cached. A stale nonce refreshes the challenge. MD5 and SHA-256 are
   supported.
4. The plugin reads on demand through the schedule event and never more often
   than `refresh`. The default is 1m, the minimum 30s. A failed request keeps
   the last data and the plugin retries within 30s. The first failure is the
   error of the cell.
5. The plugin only reads. Pause, stop and start need a write path and a
   confirmation in the device menu, and they get their own decision.
6. The widgets are `printer`, `progress` and `temperatures`. The values for
   text pages are `state` as an enum with the keys `idle`, `busy`, `printing`,
   `paused`, `finished`, `stopped`, `error`, `attention` and `ready`,
   `progress`, `time-remaining`, `time-printing`, `nozzle`, `bed` and `job`.
   The enum lets a plan show the printer page only while a print runs
   (ADR 0127).
7. The first version has no camera widget. `/api/v1/cameras/{id}/snap` exists,
   but a photo in one or four gray tones is of little use on an e-paper panel,
   and the endpoint is optional in the firmware.
8. Prusa Connect Cloud and the self-hosted Prusa Connect server are out of
   scope. A self-hosted server with its documented device API can become a
   second plugin when its API leaves beta.

## Consequences

- The plugin needs a route from Paperboy to the printer on the local network.
  It reaches no cloud, and an offline printer shows its last state.
- PrusaLink must be enabled on the printer, and its API key is a secret of the
  instance, like every other plugin password.
- A print on an e-paper display is a snapshot: the page shows the progress of
  the last fetch, not a live animation. The plan's refresh sets the pace.
- The plugin keeps no state. A restart costs one fetch.
- A printer model without PrusaLink, or with PrusaLink disabled, answers
  nothing. The plugin reports the first failure in the cell.
