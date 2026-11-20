[← Paperboy](../README.md)

# Glossary

One term per thing. If a document needs a second word for the same thing, the word is wrong.

| Term | Meaning | Not called |
|---|---|---|
| **Paperboy** | The service: one binary, one instance directory, one listener | display service, displayd, server |
| **instance** | The directory that `-config` selects, with `paperboy.yaml`, the content and the state | data directory, config directory |
| **plugin** | A long-running program that draws widgets and answers actions | provider, backend, service |
| **widget** | One drawing unit of a plugin, identified as `<plugin>/<widget>` | module, fragment, block |
| **cell** | One box of a page: a widget on an image page, a value or literal text on a text page | tile, slot |
| **text cell** | One box of a text page: a value or literal text with a column, span, alignment and overflow | row, line, label |
| **value** | One piece of data a plugin declares by name, for example `evcc/pv`; a text cell shows it | reading, datum, key |
| **reading** | One measured number a device reports in its sync, named by Paperboy's catalog, for example `temperature` | value, sensor, measurement |
| **enum value** | A value that answers one declared key, e.g. `dexcom/state` with `normal`, `low`, `high` and `urgent` | state, choice |
| **enum label** | The display text of one enum key on a text cell (`labels`); without it the key shows | key text, caption |
| **overflow** | A text box policy: `strip`, `wrap` or `scroll` | clip, fitting |
| **page** | An image grid of widgets or a text grid of value and text boxes, with a size from its display type | screen, template, view |
| **page file** | `pages/<page>.yaml`: the layout for its kind and the `variants` map. It serves every display type of its kind. | shared page, main page, base page |
| **variant** | `pages/<page>.<label>.yaml`: one layout that the page file names for a display type. It carries no title. | display version, page |
| **display type** | The kind of panel that pages fit: kind, size, gray levels, ppi or charset, power, rotation | display, panel, profile, device type |
| **orphan** | A variant whose page file is missing, and a page that exists only as variants | stray file, leftover |
| **device** | One panel with Paperboy's firmware, identified by MAC and one file under `devices/` | display, node, client, target |
| **plan** | The rules that select the pages of one device over time | playlist, schedule |
| **rule** | One entry of a plan: time window, days, value conditions, pages, `on_refresh`, refresh, `alert` | plan, entry |
| **on_refresh** | What a timed refresh of a rule shows: `cycle`, `first` or `current`; a rule change always starts at the first page | rotate, paging mode |
| **alert** | A signal through the device: a rule's `once` when the rule starts and `refresh` at the start and on every timed refresh, or a manual request that waits for the next sync; the device decides between beep, LED and backlight | alarm, tone, beep |
| **alert_methods** | The methods a device may use for an alert: a list of `beep` and `blink`; omitted or empty = silent | outputs, signals |
| **fallback** | The pages of a device whose no rule matches | default, base |
| **condition** | A value condition of a rule: a `plugin/value` must answer one of the keys in `is` | predicate, expression, data blocker |
| **action** | Something a plugin can do on request, offered in the device menu | command, task, operation |
| **menu** | The action overlay on a device, opened with `home` | overlay, dialog |
| **notice page** | A full-screen page that tells the device why it got no page | hint, error page, message page |
| **stale** | A widget result that is older than its `max_age` and therefore carries the warning bar | expired, outdated |
| **levels** | The number of gray levels a display type draws: 1, 4 or 16 | gray, grayscale, bpp |
| **device key** | The secret a device gets at registration and sends as bearer token | token, password |
| **operator UI** | The web pages for the operator, behind `ui.password` | dashboard (only `GET /ui`), admin UI, frontend |
| **plugin host** | The part of Paperboy that starts plugins and caches their widgets and values | widget host, plugin manager |
| **release train** | One of `v<version>`, `plugins-v<version>`, `firmware-v<version>` | release channel |
| **channel** | `stable` or `latest`: which releases an instance installs | train |
| **bundle** | `paperboy-firmware.zip`, the archive with the image of every hardware type | package |
| **inventory** | The programs in `plugins/` and the images in `firmware/` | cache, store |

## Where the code differs

The code keeps two older names. A document uses the term of the table and names the old one where it matters:

- `{profile}` in `/pages/{profile}/{page}/{file}` and `GET /api/profiles` means the display type. The paths stay, because firmware in the field uses them.
- `hint` in `/hints/{file}`, `hintPage` and `hints` means the notice page.

## Language

English documents use the rules of ASD-STE100 as the house style. That means short sentences, active voice, imperative for steps and present tense. One term stands for one concept, and a noun cluster has at most three words. [`development.md`](development.md) lists the rules that a script checks.
