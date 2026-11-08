# 0062: Newer plugin downloads, a configurable interval and a check on demand

- Status: Accepted
- Date: 2026-11-08
- Builds on: ADR 0040 (dashboard), 0047 (config reload), 0049 (plugin widgets), 0060 (downloads)
- Amends: ADR 0060 §2 (installation), §6 (schedule), §8 (visibility); ADR 0040 (read-only dashboard)
- Answers: issue #74

## Context

ADR 0060 shipped three gaps. A plugin program was installed only when nothing
resolved, so an instance that once got `weather-v2.1.0` kept it forever even
when the release page offered a newer one; updating by hand meant deleting the
old file, the work the feature was meant to remove. The check interval was fixed
at 24 hours. And after fixing a token or a network problem there was no way to
ask for a check, although the dashboard already showed the last error.

## Decision

### 1. Newer plugin versions are installed next to the old one

During a check, the version in use is compared with the version the release
ships. A newer release installs `<plugin>-v<new>` next to the existing file.

- Lookup keeps the highest version (ADR 0060 §2), so the resolved program
  changes, the plugin spec differs and exactly that plugin restarts on its next
  use (ADR 0049).
- The old files stay: never overwrite, never delete. A newer release is only
  installed when its version is higher than the one in use, so a late-published
  older tag does not roll back.
- An unversioned `plugins/<plugin>` and a program from `PATH` always win and are
  left alone; a `version` pin means "exactly this one" and is never moved. Each
  check logs one line per such plugin that says why.
- Deleting the newer file is the manual rollback: the older file resolves again.
  A version that this server run installed but that no longer exists is not
  installed a second time; a release with an even newer version still is. A
  pin is the durable rollback and survives a restart.

### 2. `download.interval`

```yaml
download:
  source: https://github.com/maehld/paperboy
  interval: 6h          # default 24h, at least 1h
```

The value is a Go duration and is validated at startup; shorter than an hour is
rejected with a hint. The background loop keeps waking every minute and runs a
check when the interval has passed (ADR 0060 §6 keeps the startup check). The
loop reads the interval from the current configuration, so a reload with a
shorter interval applies without a restart (ADR 0047).

### 3. A check on demand

`POST /ui/download` starts a check in the background and redirects to `/ui`
with `303 See Other`; the request never waits for the check. A second request
while one runs is ignored, and the card shows "checking…" until the result
replaces it.

The dashboard has been read-only (ADR 0040); this is its first action. It needs
no CSRF token because it does exactly one idempotent thing: start a release
check that the background loop would run anyway. It changes no configuration,
accepts no input, and the running guard makes repeated posts harmless. Every
other dashboard page stays a plain GET.

## Consequences

- Updating an instance is now "keep `download` enabled": new plugin versions
  arrive, restart on their own and show up in `/api/plugins` and the gallery
  (ADR 0055) without touching the installation.
- Rollback stays manual: delete the newer program or pin the older version. A
  deletion is respected for the rest of the server run; after a restart the
  newest release wins again unless a pin says otherwise.
- Disk usage grows with every version; pruning stays out of scope, as does
  updating the Paperboy binary and a per-plugin update switch.
- The dashboard is no longer strictly read-only. The one POST is narrow,
  token-free and documented here instead of introducing a general action
  framework.
- ADR 0060 §2 and §6 keep their history; this decision replaces their
  "install only when missing" and "once a day" sentences.
