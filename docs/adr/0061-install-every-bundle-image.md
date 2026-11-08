# 0061: Install every image of a firmware bundle

- Status: Accepted
- Date: 2026-11-08
- Builds on: ADR 0043 (firmware distribution), 0049 (plugins), 0059 (firmware bundle), 0060 (downloads)
- Amends: ADR 0060 §5 (which images a download installs)
- Answers: issue #67

## Context

ADR 0060 §5 installed "only the images of hardware types that appear in `display_types`". That sentence assumed a display type is named after the hardware, which it is not: a display type is a label the operator chooses (`hd44780` for an HD44780 panel, `wohnzimmer` if they like), while an image is named after the `hardware` a device reports in its sync (`lcd2004`, `sticky`, `e1001`, ADR 0042). A single display type without a matching image failed the whole firmware step:

```
WARN release download incomplete error="v2.2.0: no images for e1001, hd44780, sticky"
```

Nothing was installed, although the bundle carried an image for two of the three names. The configuration cannot answer the question at all: a device that has never synced has not told Paperboy its hardware, and a display type may be shared by hardware of different names.

Unpacking the bundle by hand, the way ADR 0059 describes it, installs every image anyway.

## Decision

A release check installs **every image the bundle carries** into `firmware/`, without consulting the configuration for hardware names.

- A `firmware.version` pin still narrows the installation to that image version; it only requires the bundle to carry an image of it, for any hardware type.
- A device is offered an image when its reported `hardware` matches the file name (ADR 0043), so an instance keeps a few images it may not need — a few megabytes, and the price of not guessing.
- The dashboard and the log name the installed hardware types, not display types.

## Consequences

- A display type renamed by the operator can no longer block a firmware download; a new device of an unknown hardware type finds its image already there after the next check.
- The firmware directory holds one image per hardware type of every downloaded bundle, even when no device uses that type. Deletion stays manual, as for every downloaded file (ADR 0060).
- The bundle remains the unit: what the operator would get by unpacking the zip is exactly what the download installs.
- ADR 0060 §5 keeps its wrong sentence as history; this decision replaces it.
