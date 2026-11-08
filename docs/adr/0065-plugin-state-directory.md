# 0065: A state directory per plugin

- Status: Accepted
- Date: 2026-11-08
- Builds on: ADR 0037 (instance directory), 0047 (config reload), 0049 (plugin widgets)
- Answers: issue #80

## Context

A plugin is a process that Paperboy starts, configures and restarts: when it
exits, when a request times out, and when its section of `paperboy.yaml`
changes (ADR 0049). Paperboy itself restarts on every deployment. A plugin's
memory dies with its process, and Paperboy's widget cache is no help: it holds
drawn pixels that depend on the page, the size, the language and the moment,
and it is deliberately in memory only, so a restart never serves pictures from
outdated code.

For the `myskoda` plugin (issue #77) that is expensive. The MyŠkoda API allows
20 requests per hour and VIN, and the plugin loads the vehicle right after
`configure` and then every hour. A restart therefore costs one request, and an
operator who edits pages or redeploys a few times burns the quota before the
hour is over. The render image is downloaded again, a `429` with `Retry-After`
is forgotten, and the remaining lifetime of the API key is lost.

A plugin cannot simply write next to its program: `plugins/` holds downloaded
programs that a release replaces (ADR 0060), and the working directory is that
same directory. `state/` belongs to Paperboy (ADR 0037).

## Decision

### 1. `state/plugins/<plugin>/`

Paperboy gives every configured plugin a directory of its own:

```
<instance>/state/plugins/<plugin>/
```

- It is created with mode 0700 before the plugin starts, so it exists when the
  plugin answers `configure`.
- Paperboy never reads, writes or deletes anything inside. The files belong to
  the plugin.
- The plugin learns the absolute path from the environment variable
  `PAPERBOY_STATE_DIR`, next to `PAPERBOY_LANGUAGE` and `PAPERBOY_TIMEZONE`
  (ADR 0036, 0049). No protocol field and no version bump are needed, so a
  plugin in any language can use it; `pkg/plugin.StateDir()` is the Go helper.
- `env:` keys of the plugin's section cannot override the variable: the host
  sets it last. A configuration therefore cannot point a plugin at another
  plugin's files.
- A plugin that is removed from `paperboy.yaml` leaves its directory behind.
  That is the operator's to delete, like the rest of `state/`; Paperboy has no
  lifecycle for it, because it does not know what the plugin stores.

### 2. What a plugin may keep there

The directory is for what a plugin needs to skip work after a restart: the
timestamp of the last successful load, a waiting time the API asked for, a
downloaded picture, a rotated pick.

- Only derived data, never a secret the operator already configured; a plugin
  may keep a fingerprint of a key to notice that it changed.
- A version number in the file, so a later release can discard what it does not
  understand instead of guessing.
- Atomic writes (temporary file, then rename): a crash leaves the old file.
- A file that cannot be read, belongs to another vehicle or key, or has another
  version is ignored with one log line and replaced. A missing directory or
  file is normal, never an error.

### 3. `myskoda`

`state/plugins/myskoda/` holds `state.json` (version, VIN, key fingerprint,
`fetched_at`, `next_attempt`, `key_expires_at`, the last vehicle answer, the
render URL) and `render.png`. `Configure` takes the state over, so the first
page after a restart has data. `Start` asks the API only when one refresh has
passed since `fetched_at` **and** `next_attempt` is over; a `429` persists the
wait it carries. A different VIN, a different key or a broken file discards the
state, and the next start loads as before.

## Consequences

- A restart within the refresh interval costs no request, and the widgets keep
  the last state including its capture time instead of showing a notice.
- Nothing new to operate: the directory lives under the gitignored `state/`, and
  a fresh instance simply starts without one.
- Plugins decide about their own format. Paperboy cannot inspect or migrate the
  files, so a plugin that gains a field has to version its own file.
- State stays on disk indefinitely and has no size cap. That is acceptable for
  the small files the plugins keep; a plugin that wrote large amounts would have
  to prune itself.
- The directory is not backed up or migrated with the instance. `state/` already
  held only what can be lost (device cursors, battery levels), and this is the
  same kind of data.
