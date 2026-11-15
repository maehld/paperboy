# 0114: A rename does not ask

- Status: Accepted
- Date: 2026-11-15
- Related: ADR 0079 (renames), ADR 0113 (a page rename moves its variants)

## Context

Every editor asked before a rename: a `confirm` dialog repeated what the action does and named the plans, devices or pages that keep the old name. The text came from the server with the rename action (`renameAction.Warn`) and was long. It interrupted a one-field action, and it said the same thing every time.

ADR 0079 decided that a rename does not rewrite a reference and names the ones that keep the old name. The naming moved into a confirmation, which the operator dismissed without reading.

## Decision

1. **A rename asks for the new name only.** The `confirm` dialog and the warning text go away, and `renameAction` carries the endpoint only.
2. **Unsaved changes still ask.** A rename navigates away, so a dirty draft keeps one short question: discard the changes? The answer is the only confirmation left.
3. **A delete still names its references.** Deleting is destructive and cannot be undone in the editor, so its confirmation stays.
4. **The editors keep their reference lists.** The device and display type editors list the pages and devices that use the object, so the information is available before the rename.

## Alternatives

- **Keep a shorter text.** Any text repeats on every rename and does not change the decision.
- **Name the references only when there are any.** Then most renames stay quiet, but the dialog appears exactly in the case the operator already thought about. Rejected: the reference lists cover it.
- **Rewrite the references.** That changes ADR 0079 and the instance files behind the operator's back.

## Consequences

- A rename is one prompt and one request.
- The plans, devices and pages that keep an old name appear in the editor's reference lists, not in a dialog. A plan that names a renamed page shows the skip warning until it is updated.
- `renameAction.Warn` and the warning helpers of the page, plan, device and display type editors go away.
- `docs/design/04-operator-ui.md`, `docs/runtime.md` and `docs/setup.md` describe the rename without a confirmation.
