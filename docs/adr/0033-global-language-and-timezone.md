# 0033: Global language and timezone in the configuration

- Status: Accepted
- Date: 2026-10-21

## Context

Paperboy renders notices, a setup page, battery warnings, the stale-data bar and chart placeholders itself. Their texts were hard-coded in German, so an English installation or a device in another country could not be served consistently. ADR 0024 and ADR 0025 placed language-specific formatting into page templates (`locale`) and the evaluation timezone into each plan (`timezone`). Both settings describe the installation as a whole, not one page or one plan: repeating them in every template and plan invites contradictions and makes new files easy to get wrong.

## Decision

We configure language and timezone once, at the top level of `paperboy.yaml`:

- `language: de` or `language: en` selects the language of all built-in image texts and the number format (decimal comma for `de`, dot for `en`). Without the key Paperboy uses the system language (Windows UI language, otherwise `LANGUAGE`, `LC_ALL`, `LC_MESSAGES` or `LANG`); unsupported languages fall back to English.
- `timezone: <IANA name>` is the timezone for plan time windows and built-in dates. Without the key the system timezone is used.
- Page templates no longer have a `locale`; a `locale` key is rejected. Plans no longer have a `timezone`; a `timezone` key is rejected.
- Built-in texts stay in code with German and English variants. Texts that belong to a page remain template content.

## Consequences

- Operators set the language and timezone in one place; plans and templates stay installation-independent and can be shared between devices.
- Existing plan and template files must drop `timezone` and `locale`. Strict parsing rejects both keys; a broken plan shows a notice page, a broken template shows a page error notice.
- Mixed-language installations are not supported. If devices ever need different languages, a per-device override can be added without changing the global default; the built-in texts are already selected through one `texts` value.
- The timezone list comes from `time/tzdata`, which is embedded in the binary, so no system tzdata is required.
