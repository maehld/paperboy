# 0164: A widget action carries its own arguments

- Status: Accepted, implemented
- Date: 2026-11-25
- Builds on: [ADR 0162](0162-widget-actions-and-the-plugins-choices.md) (a widget action and the plugin's choices), [ADR 0057](0057-plugin-actions-from-the-plan.md) (plugin actions)
- Answers: issue #443
- Amends: [ADR 0162](0162-widget-actions-and-the-plugins-choices.md)

## Context

ADR 0162 bound an action to a cell with `action: mode`. The server filled
every action argument that the widget also declares from the widget's args.
The one remaining required enum argument became the choice.

That works for the evcc loadpoint, where the widget and the action both name
`index`. An action argument that the widget does not declare has no place. A
fixed choice has no place. Two different values for the same name cannot
coexist. The widget draws with its arguments; the action runs with its own.

The plugin owns the action schema and the texts: `describe` names the action,
its arguments and its title; `actions` names each option and its state. The
page must not repeat those texts, because the plugin localizes them.

## Decision

1. **`action` is a block with its own args.** A cell names a block
   `{name, args}`. The scalar `action: mode` stays a shorthand for a block
   without args. One action per image cell; a text page has none.

2. **The action's args are explicit.** The action block never inherits the
   widget's args. The widget's args draw the widget; the action's args target
   the action. A name in both is two independent values.

3. **A set value fixes the call; an empty required enum is the choice.** The
   server makes one `actions` call for a fixed action, or one per enum value
   for a choice. More than one empty required enum disables the action. A
   missing required argument that is not a choice disables it with the reason.
   An optional argument with a default may stay absent; the plugin applies the
   default.

4. **The plugin labels everything.** The action title and every option title
   come from the plugin in the configured language. The page carries no title
   and no confirmation.

5. **The protocol does not change.** Protocol 9 describes the action schema and
   the choices already. The server builds one `actions` request per plugin.

6. **The editor edits the action's own args.** The inspector offers the
   plugin's actions and the argument fields of the chosen action from
   `describe`. An empty required enum reads as the device's choices. The
   inspector warns about an unknown action, a missing argument, more than one
   choice argument and a cell too small for the tiles.

## Alternatives

- **Keep the implicit inheritance.** It is ambiguous as soon as an action
  argument and a widget argument share a name with different meanings. It also
  cannot express a fixed choice.
- **A title on the page.** It would duplicate the plugin's localized action
  title. The plugin knows the domain and the language; the page does not.
- **A protocol request with a widget context and an active flag.** That is a
  later step. It is not needed to run one action with its own arguments.

## Consequences

- The evcc page writes `action: {name: mode, args: {index: 0}}` for a loadpoint
  other than the first. The action's `index` is optional with default 0, so a
  page may omit it. An old scalar `action: mode` thus targets the first
  loadpoint. The action never takes the widget's `index`.
- The editor shows a declared default as a placeholder and writes no value. The
  plugin applies the default when the argument is absent.
- A page with a scalar action whose required arguments can stay empty keeps
  working, e.g. the MyŠkoda `charging` and `climate` actions.
- The page model, the editor snippet and the JSON API carry the block. The page
  file keeps the scalar shorthand while the action has no args.
- The design documents name the action block, its arguments and the editor
  fields. The glossary says that the cell names the action with its own
  arguments.
