# 0090: The download cache keeps one file, the inventory is pruned by hand

- Status: Accepted, implemented
- Date: 2026-11-12
- Builds on: ADR 0043 (firmware distribution), 0060 (downloads), 0061 (every bundle image), 0062 (plugin updates), 0075 (a version per plugin), 0089 (release channels)
- Amends: ADR 0060 §4 (the layout of `state/download`, and the learned entry lists outlive the zip)
- Answers: issue #162

## Context

Nothing was ever cleaned. `plugins/` and `firmware/` gain a file per version — never overwritten (ADR 0060 §2), versioned names (ADR 0075 §2, 0043) — and `state/download/` keeps every zip it fetched as `<tag>-<asset>`, plus a `cache.json` whose `bundles` and `plugin_zips` maps gain an entry per tag. Channels (ADR 0089) make both grow faster.

The two directories are not the same thing:

- `plugins/` and `firmware/` are the **inventory**. A missing file costs a plugin or a device update. The operator sees these files, drops builds into them and deletes them by hand (ADR 0060 §2, 0075 §Consequences).
- `state/download/` is a **cache**. Losing it costs one release check and one download.

Treating them alike is what makes cleanup feel risky.

## Decision

### 1. The cache keeps one zip per asset

`state/download/` holds one file per asset name: the plugin zip of this platform and the firmware bundle. When a new one has been downloaded, verified and installed, the previous file of the same asset is deleted. The order matters — download, verify against `SHA256SUMS`, extract, install, and only then remove the old file — so a failed check leaves a working zip behind.

No clock and no size budget: the rule is "the next one replaces the previous one", which needs no configuration and cannot fire on an offline day.

### 2. The learned entry lists live on in `cache.json`

`cache.json` stays: the ETag of the release list, the list itself, and per tag which entries its zip or bundle carried. It is a few kilobytes and it is what makes a check that changes nothing cost one conditional request (ADR 0060 §4).

Because a pin is answered from those lists, `Bundle` and `PluginZip` stop requiring the zip to be on disk when the entry list is known; the zip is fetched only to install from it. That is what makes rule 1 free: the lookup never needs a file that may already be gone.

### 3. The cache is namespaced by train

`state/download/plugins/` and `state/download/firmware/`, each with its own `cache.json`. One repository serves both trains today, but a `v1.4.0` tag of a future plugins repository would collide with the firmware one in the flat layout (ADR 0056 leaves that split open). Files of the old flat layout are no longer read; they are cache and may be deleted.

### 4. The inventory is pruned by hand, with a preview

`plugins/` and `firmware/` are never cleaned automatically. The releases page gets a prune action that first lists what it would delete. It keeps:

- the version the resolver picks (the highest, or the pin),
- every version a pin names in `paperboy.yaml` or a device file,
- every version a device reports or is being offered right now,
- programs whose name does not follow the version pattern — a hand-built or unversioned file (ADR 0075 §2),
- the newest two versions as a safety net.

A file that Windows refuses to delete because its program is running is logged and stays for the next click. The result names what went, and nothing else in the instance directory is touched.

### 5. A failed check deletes nothing

No releases, no deletion — neither in the cache nor in the inventory. Cleanup happens after a successful check only.

## Alternatives

- **A time to live for the cache.** Simpler to explain, but a pin would fetch its zip again after every expiry, and the "one per asset" rule needs no clock at all.
- **A size cap.** Needs a budget, and on a small disk it deletes something unexpected. Rejected.
- **Automatic pruning of the inventory** (`keep: N` without a click). Deferred, not rejected: the rule table above is what it would use, but a wrong guess costs a device update or a plugin, so it waits until the dialog has been used a few times.
- **Never delete, only warn** on the releases page: leaves the growth in place, which is the problem.

## Consequences

- Disk usage stays flat: a check replaces zips instead of accumulating them, and a channel switch (ADR 0089) does not pile up artifacts.
- A pin to a release older than the newest one re-downloads that zip when it installs; the version lookup itself stays free.
- The inventory still grows on its own, so the releases page should show the count and size of `plugins/` and `firmware/` — growth has to be visible before someone clicks.
- The abandoned flat cache directory needs one line in the release notes; nothing else refers to it.
- The prune dialog is the only place where Paperboy deletes a file the operator did not create, so it stays a button with a preview, also after an automatic variant exists.
