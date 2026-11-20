# 0135: A minimal environment for plugins

- Status: Accepted
- Datum: 2026-11-20

## Kontext

Paperboy starts every plugin with `os.Environ()`. A plugin therefore inherits
every variable of Paperboy, including `PAPERBOY_UI_PASSWORD` and
`PAPERBOY_DOWNLOAD_TOKEN`. A plugin needs only its configured `env`, the
language and the timezone. Appended keys also did not replace inherited ones,
so a secret in the parent environment could win over the configured value.

Plugins are separate programs, but Paperboy does not sandbox them. A plugin
runs with the rights of the operator and can read the instance directory when
it guesses the path. The environment is a least-privilege boundary, not a
sandbox.

## Entscheidung

Paperboy builds the environment of a plugin from an allowlist: `PATH`, `HOME`,
`USER`, `LOGNAME`, `SHELL`, `TMPDIR`, the locale variables, the proxy
variables and the variables Windows needs to start a program. The configured
`env` of the plugin and `PAPERBOY_LANGUAGE`/`PAPERBOY_TIMEZONE` follow.

A configured key replaces an inherited key of the same name. Windows compares
the names without case. Paperboy sorts the result, so a plugin sees a stable
order.

The operator installs only plugins they trust. Paperboy documents this trust
model and keeps the allowlist in the code; a plugin that needs another
variable gets it through its `env` section.

## Konsequenzen

A plugin no longer reads the operator password or the release token from its
environment. A plugin that relied on an inherited variable fails until its
`env` names that variable. The allowlist must grow when a platform needs
another variable to start a program, for example on Windows. The environment
is still no sandbox: file access and network access stay open to a plugin.
