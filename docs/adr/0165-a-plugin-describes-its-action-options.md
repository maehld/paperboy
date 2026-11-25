# 0165: A plugin describes its action options

- Status: Accepted, implemented
- Date: 2026-11-25
- Builds on: [ADR 0162](0162-widget-actions-and-the-plugins-choices.md) (a widget action and the plugin's choices), [ADR 0164](0164-a-widget-action-carries-its-own-arguments.md) (the action block), [ADR 0057](0057-plugin-actions-from-the-plan.md) (plugin actions)
- Amends: [ADR 0162](0162-widget-actions-and-the-plugins-choices.md) (its point 8 and its `ActionOptions` alternative)
- Answers: issue #445

## Context

ADR 0162 gave a page cell one action of its plugin. Protocol 9 describes the
action and its arguments, and Paperboy derives the choices from them:

- The action arguments come from the action block ([ADR 0164](0164-a-widget-action-carries-its-own-arguments.md)).
- The one remaining required enum argument is the choice. Paperboy makes one
  `actions` call per enum value.
- The `actions` answer carries a title and a `disabled` reason per call.

That worked for one dimension, but it has four limits:

1. Version 9 has no `active` flag. evcc marks the current charge mode as
   disabled. The tile is hatched, so the operator cannot tell "is active" from
   "cannot run".
2. A second required enum argument disables the action. Two dimensions of
   choice do not fit.
3. `describe` has no language, timezone or device and is cached. Only the
   per-call title follows the language. An option set that depends on the
   device or the current data cannot change per request.
4. A plugin decides which values make sense from its own state. myskoda's
   `start` and `stop` depend on whether the car charges. Version 9 expresses
   that again as a disabled reason.

The page must not repeat the plugin's schema or choices. The plugin owns both.

## Decision

1. **Version 10 adds the `action_options` request.** Paperboy sends one base
   call per widget action. The call carries the arguments the action block
   fixes. The plugin answers one option set per call, in the request's order.

2. **An option carries the complete call, a title, `active` and `disabled`.**

   ```json
   {"title":"Lademodus","options":[
     {"args":{"index":0,"mode":"pv"},"title":"PV","active":true},
     {"args":{"index":0,"mode":"now"},"title":"Jetzt","disabled":"Kein Kabel"}]}
   ```

   - Paperboy runs `args` with the unchanged `action` request.
   - `title` follows the request's language.
   - `active` marks the current option. Paperboy draws it as the selected tile
     and does not run it. At most one option is active.
   - `disabled` is the reason an option cannot run. Paperboy hatches the tile.
   - A set-level `disabled` keeps the cell with the reason and no options.

3. **The plugin flattens its dimensions.** The option list is one list in
   display order. Paperboy does not group or combine options. A call that fixes
   an argument lets the plugin return only the options that remain.

4. **The server drops the enum guess.** A plugin from protocol 10 answers the
   options. Paperboy no longer makes one `actions` call per enum value and no
   longer reads the current option from a disabled reason.

5. **Protocol 10 is additive.** `describe` and `actions` stay. A plugin below
   version 10 keeps the protocol 9 path: one `actions` call per enum value and
   the current value as a disabled option. A third-party plugin therefore keeps
   working without a rebuild.

6. **`sdk/plugin` gains an optional interface.** `ActionOptioner` answers the
   request. A plugin that does not implement it answers an error, and the
   server asks only a plugin that reports version 10 or newer.

7. **The active tile is inverted.** The action view already owns the tile look
   ([ADR 0162](0162-widget-actions-and-the-plugins-choices.md), design
   language). On a touch device an active option is a black tile with a white
   label; a device without a digitiser draws its selection there instead
   ([ADR 0163](0163-the-action-mode-works-with-the-keys.md)). A disabled option
   keeps the hatch. An active option neither runs nor closes the mode. The key
   selection skips active and disabled options.

8. **The shipped action plugins use it.** evcc returns one option per charge
   mode with the current mode active. myskoda returns `start` and `stop` with
   the target state that holds active.

## Alternatives

- **Add `active` to `ActionState` only.** It would separate active from
  disabled, but the server would still guess the choice from one enum
  argument. A second dimension, a device-dependent set and a full argument
  stay impossible.
- **Let the plugin draw the action view.** Every action plugin would draw its
  own tiles and hit areas, and the view would diverge. Paperboy draws it from
  the plugin's data instead.
- **Keep the version 9 path only.** It costs no protocol version. But every
  plugin after the next rebuild reports 10 anyway, and the limits stay.

## Consequences

- A plugin answers the complete option list in its own language and with its
  own state. The operator sees which option is current and which cannot run.
- More than one dimension of choice fits. A plugin can also offer a fixed call
  as a single option.
- Paperboy keeps two paths until third-party plugins no longer report below
  10. The fallback stays simple and tested.
- The `sdk/plugin` change raises every plugin version
  ([ADR 0107](0107-plugin-builds-reach-installations.md)), also for plugins
  without an action.
- A plugin that reports 10 and fails `action_options` keeps its cell with the
  reason. It never costs the page.
