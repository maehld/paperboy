# 0084: The overview links, the editors act

- Status: Accepted
- Date: 2026-11-11

## Context

The overview at `/ui/editor` listed pages, plans, devices and display types
with a mix of controls: *Open* was a link, *Rename* and *Delete* were buttons
that acted on the file from the list, and a *New* object unfolded a form inline
in the header. The list was therefore not only a list but also a place that
changed files, with confirmations that repeated what the object's editor already
knew (the plans that name a page, the devices of a plan). A prose hint under each
list explained the file layout again.

## Decision

The overview is a list of links. A row opens the editor of its object: the name
in the first cell is the link, and a click on the row follows it. *Rename* and
*Delete* live in the action bar of the editor only, which already names the
references that keep the old name or keep pointing at the deleted file (ADR 0073,
0079).

Two actions stay in the list because their files have no detail page: the Delete
of a page variant that neither the row nor the display type switch reaches, and
*Forget* for unclaimed hardware (ADR 0080). *Claim* makes the whole unclaimed row
open the new device editor, but asks first, because a row click leaves the list.

New objects are created through a modal `<dialog>` with the name field (a page
also chooses the display type); the form still submits as a plain GET into the
editor, which stays the only page that writes the file. The prose hints under the
lists go; an empty list still says which files it would show.

## Consequences

One rule for every row and one place that changes a file. The operator has to
open an object to rename or delete it - one click more, but the editor shows the
references before the file goes. A page variant that no editor can reach keeps
its Delete in the list, so the rule has a documented exception. The dialog needs
`<dialog>` support, which every browser Paperboy targets has had for years. The
`data-*`-driven list script shrinks: rows, dialogs and Forget are the only
handlers left.
