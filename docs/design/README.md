[← Paperboy](../../README.md)

# Design documents

These documents describe how Paperboy works **today**. They keep the structure of the system and the reason behind it. They do not repeat the tasks: [`../setup.md`](../setup.md), [`../content.md`](../content.md), [`../runtime.md`](../runtime.md) and [`../releases.md`](../releases.md) describe what you do, and [`../plugins/`](../plugins/README.md) documents every plugin.

Each document ends with a list of the decisions it carries. The [ADRs](../adr/README.md) keep the history: what was decided, what it replaced and why. An ADR is not the place to read the current state. A design document is not the place to read why an old approach was dropped.

| Document | Content |
|---|---|
| [`01-system.md`](01-system.md) | Goals, parts and processes, instance directory, configuration, reload, data flow |
| [`02-content.md`](02-content.md) | Pages, widgets, grid and style, text pages, plans, value conditions, actions, the device menu |
| [`03-devices.md`](03-devices.md) | Device API, device state, power model, page delivery, firmware, hardware findings |
| [`04-operator-ui.md`](04-operator-ui.md) | Overview, plan preview, virtual display, editors, page and plan API, password |
| [`05-plugins.md`](05-plugins.md) | Plugin protocol, plugin host, versions, state and blobs, value conditions, the shipped plugins |
| [`06-delivery.md`](06-delivery.md) | Release trains, assets, workflows, the download client, channels, cache, inventory |
| [`07-design-language.md`](07-design-language.md) | The shared type scale and the components of plugin widgets |

[`../glossary.md`](../glossary.md) fixes the terms that all documents use.
