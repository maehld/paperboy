# 0101: A hint about a newer Paperboy

- Status: Accepted, implemented
- Date: 2026-11-13
- Builds on: ADR 0056 (release trains, stamped version), 0060 (release checks), 0062 (interval and check on demand), 0089 (channels), 0090 (cache per train)
- Answers: issue #229

## Context

Paperboy never updates itself: a release build is a binary the operator replaces
by hand (ADR 0056), and the release check only installs plugins and firmware a
running instance needs (ADR 0060, 0062). An instance therefore says nothing when
a newer Paperboy appears, and exactly the operators who read the dashboard every
day are the ones who notice last.

The information is already there. `download.List` reads the releases of the
source, and the tags of the three trains (`vX.Y.Z`, `plugins-vX.Y.Z`,
`firmware-vX.Y.Z`) share one list. A third consumer that only reads the `v` train
costs one conditional request per check — the ETag usually answers `304`.

Considered and rejected:

- **an embedded update client (GitHub API, self-update, restart):** what
  ADR 0056 deliberately leaves out. A service that replaces its own binary
  changes the file under its own feet and needs a supervisor, a rollback and a
  second signature chain.
- **a static link to the releases page in the UI:** no information, so nobody
  follows it.
- **checking on every page view:** a request per refresh, and the answer cannot
  change between checks.

## Decision

### 1. The check names the version, it installs nothing

After the plugin and firmware half, `downloadOnce` looks at Paperboy's own train
and records the result in `downloadStatus`. Nothing is fetched, verified,
unpacked or started: no asset download, no `SHA256SUMS`, no restart, and an
update hint never appears in `lastError`. A failure to list the releases is one
journal line; the plugins and firmware of that check are unaffected.

The train is a tag of the form `vX.Y.Z` — the shape no other train uses — and is
kept in its own cache directory `state/download/paperboy/cache.json` (ADR 0090),
so a source that only carries plugin and firmware releases answers no hint
instead of an error.

### 2. The channel follows the running version

A final version hears about the final releases, a version with a prefix
(`1.4.0-rc.1`) hears about the prereleases too. The rule is derived from
`build.Version`, so there is no `paperboy_channel` key: nobody selects a channel
for a hint they cannot install from. A `stable` instance is never pointed at an
rc, an instance that runs an rc is not stranded on it, and the newest final
outranks its own rc in the `latest` channel (ADR 0089), so the hint moves on by
itself.

An older running version produces no hint, a version without one — no tag,
`(devel)`, a pseudo-version from a working tree — cannot be compared at all and
is shown as `dev build`. The page says `no release in the channel` when the
source carries no Paperboy release and `up to date` otherwise.

### 3. The dashboard links the release, the page names the state

The dashboard shows one line, `2.14.0 is available`, and the version itself is
the link to the release; the text appears instead of the periodic `Checking for
releases…` line and names no action, because there is none. The *Downloads* page
adds a `Paperboy` row to its check card with the same text or the state of the
comparison.

The link is derived from `download.source`, so a fork is linked as it is. Only
the public GitHub host has such a page; an API endpoint or another host keeps the
version as plain text rather than inventing a URL.

The journal names a version once per server run, not once per daily check.

## Rejected

- **a `paperboy_channel` key:** the running version already answers it, and a
  hint that names an rc on a stable instance is noise.
- **a `paperboy` section or a second `source`:** the releases of Paperboy and of
  its plugins come from the same repository (ADR 0056); a separate source would
  be a second place to configure the same thing.
- **an update banner on the device or in the notices:** the panels show the
  household's content, not the operator's maintenance.

## Consequences

- One conditional request per check, one small cache file, no new configuration:
  every instance with a `download` section gets the hint.
- A dev build is never nagged, and an instance without `download` stays silent as
  before.
- The hint cannot be switched off separately; dropping the `download` section
  still turns the whole check off.
- Whoever replaces the binary by hand learns about the release without opening
  GitHub; the journals, not a banner, carry the record.
