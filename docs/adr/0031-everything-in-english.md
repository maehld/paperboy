# 0031: Everything in the software is English

- Status: Accepted
- Date: 2026-10-20
- Supersedes: ADR-0030

## Context

ADR 0030 made log messages, comments and commit messages English but kept documentation and error messages German. With two public repositories (SolarAI and Paperboy, ADR 0029) one consistent language makes code, history and documentation accessible to everyone. Paperboy is a general display service and must not be tied to one language or locale. This ADR applies to both repositories.

## Decision

- **English:** identifiers, code comments, log messages, error messages (including API error responses), CLI help texts and tool output, test messages, commit messages, README, AGENTS.md and new ADRs.
- **Commit history:** before the first push, all existing commit messages of both repositories were rewritten in English (content, authors and dates unchanged).
- **Exceptions:**
  - SolarAI has a German user interface: the dashboard (`internal/dashboard/web/`) and the texts drawn on ePaper images (`internal/epaper`) stay German, including German number formats (decimal comma).
  - Existing ADRs (0001–0030) stay German as historical records; they are not translated.
- **Paperboy and languages:** Paperboy must support multiple languages and locale-specific number and date formats. Language and formatting belong in the templates (ADR 0024), not in code. The image texts currently built into Paperboy (battery warning, stale-data bar, placeholder) stay German until templates exist and then move into them.
- Communication between the user and coding agents remains German.

## Consequences

- Users and contributors read logs, errors and documentation in English; only SolarAI's end-user surfaces are German.
- Commit hashes changed by the rewrite; this is harmless because nothing had been pushed yet.
- Paperboy's template work (ADR 0024) must include a locale setting (language, decimal separator, date format).
