# 0037: One instance directory with a fixed layout

- Status: Accepted, implemented
- Date: 2026-10-22
- Supersedes: the `directories` section of ADR 0026, the storage locations described in ADR 0024 and ADR 0036
- Related: ADR 0025 (plans), ADR 0032 (provider model), ADR 0033 (language and timezone), ADR 0036 (exec providers)

## Context

Paperboy inherited its file layout from `displayd`: `-config` names a YAML file, `-data` or `directories.data` names a writable directory, and `directories.pages`, `directories.plans` and `directories.providers` point at the content directories. Every path can be relative to the configuration file or absolute, and `-data` overrides a setting that the configuration already contains.

That costs three things:

- **No recognisable installation.** Looking at a directory does not tell whether it is complete. README, container and backup instructions have to explain five paths, and two installations on the same machine can share directories by accident.
- **The names lie.** The "data directory" holds device keys (`state.json`), the data document (`data.json`) and, by default, the templates, plans and provider programs as well. "Data" therefore means four different things, and `-data` on the command line sounds like the place for the data document only.
- **`state.json` promises more than it holds.** The file is a map with one entry per device (API key, friendly ID, page cursor, last seen, battery level, active rule). It is the device register, not the state of the service.

Requirements: one directory handed to Paperboy, the same shape on Windows and in the container, a clear separation between operator-owned files and what the service writes, and names that describe their content.

## Decision

We configure Paperboy with **one instance directory**: `-config <dir>`, default `.`. It contains `paperboy.yaml` and a fixed layout:

```text
<dir>/
  paperboy.yaml      configuration (operator)
  pages/             page templates <page>.<display type>.yaml (operator)
  plans/             plans <name>.yaml (operator)
  providers/         exec provider programs (operator, ADR 0036)
  state/             written by Paperboy only
    devices.json     device register and device state (API keys!)
    data.json        last state of the data document with its write times
```

### 1. Directory and flag

- `-config` names the instance directory, not a file. A file argument is rejected with an explanation; there is no silent fallback to its parent directory.
- `-data` is removed. The instance directory is the only location Paperboy reads and writes.
- The `directories` section is removed. Because parsing is strict, a configuration that still contains it aborts the start with `field directories not found` instead of ignoring a stale path.

### 2. Operator content and service state

- `paperboy.yaml`, `pages/`, `plans/` and `providers/` are operator-owned. Paperboy only reads them; it never creates them, so a typo in the instance path cannot produce an empty but plausible installation.
- A missing plan or template keeps its existing behaviour: the device gets a notice page in the configured language. A missing providers directory is reported per run by the provider runner.
- `state/` is the only directory Paperboy writes, and it creates it at startup. It is the backup target; `paperboy.yaml`, `pages/`, `plans/` and `providers/` belong in the operator's repository or dotfiles as usual.

### 3. Names

- The writable directory is `state/`: both files are the last known state of the world and exist only to survive a restart.
- `state.json` becomes `devices.json`. The content is one entry per device, and the name matches `GET /api/devices`. No global service state is stored there, so a future need for it would get its own file instead of growing this one.
- `data.json` keeps its name: the file is the persisted form of the data document, and `PUT /api/data/{path}` and the templates already use that vocabulary (ADR 0024). Only the directory name changes.

### 4. No migration code

The previous location of `state.json` and `data.json` was configurable (`directories.data`, `-data`, or the default `paperboy-data`), so an automatic migration would have to guess and could move the wrong files. The README describes the manual move instead:

1. Create `state/` in the instance directory.
2. Move `state.json` to `state/devices.json` and `data.json` to `state/data.json`.
3. Move `pages/`, `plans/` and `providers/` to the instance directory if they lived below the old data directory.
4. Remove the `directories` section from `paperboy.yaml`.

Missing `state/devices.json` means new API keys, and every display needs a new setup. That is exactly why the file is documented as the one file to keep.

### 5. Container

The image ships no default paths beyond the instance directory: it sets `WORKDIR /config` and creates `/config` owned by the runtime user, so `-config .` is the default and a single volume carries the whole installation. An operator who wants the configuration read-only mounts `paperboy.yaml`, `pages/`, `plans/` and `providers/` individually and `state/` as a separate writable volume.

## Consequences

- One directory describes an installation completely; backups, container volumes and support questions shrink to "which instance directory?".
- The configuration no longer knows about paths, so it cannot contradict the layout, and pages, plans and providers are found without settings.
- `devices.json` is honest about its content and matches the API; the data document keeps its name and its endpoint.
- Writing and non-writing files are separated by directory, which allows a read-only configuration mount and makes accidental edits to the register less likely.
- **Breaking change:** `-data`, `directories` and `state.json` disappear. Existing scripts, compose files and installations must be adjusted once; a stale configuration fails loudly at startup.
- Paperboy cannot be pointed at content outside the instance directory (e.g. a shared pages repository). If that becomes necessary, a later ADR can add read-only includes rather than bringing back free-form paths.
- Tests build their instances the same way production does (one directory plus derived paths) instead of configuring page and plan paths individually, so a wrong layout assumption shows up in the test suite.

## Alternatives rejected

1. **Keep `directories` as optional overrides.** It would preserve the status quo, but every installation would still have to be inspected to be understood, and the documentation would keep explaining five paths for a use case (shared content between instances) that does not exist yet.
2. **Call the instance directory "data".** That repeats the naming problem on a larger scale: the directory would contain the configuration, templates and programs, none of which are data.
3. **Call the writable directory `data/`, `var/` or `runtime/`.** `data/` collides with the data document; `var/` is Unix folklore without a benefit here; `runtime/` suggests something that can be deleted and rebuilt, while `devices.json` cannot.
4. **Keep `state.json`.** The file holds a device map, and the name suggests a place for future global state. Renaming it now is cheaper than living with a misleading name that every new file would be pushed into.
