# 0113: A page rename moves its variants

- Status: Accepted
- Date: 2026-11-15
- Related: ADR 0079 (renaming), ADR 0110 (pages serve their kind and name their variants), ADR 0112 (a missing page file)

## Context

ADR 0110 gives a page one page file and a map of variant files. It also decided that a rename touches one file: the page file moves, the variants stay under the old name. ADR 0112 removed the assign dialog, so nothing moves a variant to another page name any more.

The variants of a renamed page are therefore orphans. They are read-only, and the pages list does not link them. The page cannot be put together again without hand work on the files.

A move of several files also fails halfway today. `moveFiles` renames the files in order and rolls back when a rename fails, but it does not check the targets first. A missing directory or a taken target is noticed after the first file has already moved.

## Decision

1. **One page rename moves the whole set.** The page file `pages/<name>.yaml` and every variant file `pages/<name>.<label>.yaml` move in one step. The labels and the `variants` map stay unchanged, so no page file is rewritten. The answer names every moved file.
2. **A taken target refuses the rename.** When any target file exists, the request answers 409 before anything moves.
3. **Every move is preflighted.** Before the first `os.Rename`, each source must exist and be readable, each target must not exist, and each target directory must be writable. Writability is probed by creating and removing a temporary file in the target directory. A failed check returns an error that names the file, and nothing has moved.
4. **The rollback stays.** A rename that fails after the preflight rolls back the files it already moved, as before.

## Alternatives

- **Leave the variants behind and keep the assign dialog.** ADR 0112 removed the dialog, and the orphans stay unreachable without it. Rejected.
- **Rewrite the variants map on a rename.** The labels do not change, so the map needs no write. A write would add a backup and a failure path for nothing.
- **Preflight only in the page rename.** The variant rename and move already check one target. One check in `moveFiles` gives every caller the same guarantee with less code.

## Consequences

- A renamed page keeps all its files; no orphan appears after a rename.
- A permission problem or a taken target refuses the whole rename instead of moving part of the set.
- Each file keeps its own backup under `state/editor/`, unchanged.
- `docs/design/04-operator-ui.md` and `docs/runtime.md` describe the rename of the whole set and the preflight.
