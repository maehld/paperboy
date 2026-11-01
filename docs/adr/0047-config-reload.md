# 0047: Reload the configuration on change

- Status: Accepted
- Date: 2026-11-01
- Amends: ADR 0025 (plans reload on change), ADR 0036 (exec providers)

## Context

Plans reload when their file changes (ADR 0025). The rest of `paperboy.yaml` does not: display types, devices, firmware policy, battery thresholds, timezone and providers are read at startup, so every change needs a restart. On a device server a restart is a manual interruption of the whole installation, and a broken configuration file aborts it entirely.

## Decision

Paperboy reloads `paperboy.yaml` on change. The server keeps a configuration loader that caches the file's mod time and size and re-reads it like `planLoader` does:

1. Every request handler reads the current configuration through `Server.config()`, so a change is visible immediately — the next sync or API call — without a restart.
2. A background loop (the interval of the providers) also reloads and restarts exec providers whose spec changed, stops removed ones and starts new ones.
3. The global timezone is part of the plan cache key: changing it re-parses the plans, so time rules move without a restart.
4. A file that fails to load keeps the last valid configuration and is logged once; a missing file leaves the current configuration alone.
5. A startup without a usable file is not fatal: the server starts degraded (no display types, no devices) and applies the file as soon as it becomes valid. An empty `upload_token` is never accepted by the data API.

## Consequences

- Configuration changes no longer require a restart; a malformed edit degrades to the last valid state instead of stopping the server.
- The reloaded configuration only affects registered devices and new requests; a device that is currently syncing keeps the snapshot it started with.
- The provider reconciliation compares the full spec, so renaming or reordering an unchanged provider does not restart it.
