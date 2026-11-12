<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/logo/paperboy-lockup-inverted.svg">
    <img src="docs/logo/paperboy-lockup.svg" alt="Paperboy" width="320">
  </picture>
</p>

# Paperboy

Paperboy is a display service for ePaper displays and text displays. It runs on your computer, composes pages and sends them to the displays in your network.

A page is a grid of cells, and each cell shows a widget. Every widget comes from a plugin: a long-running program that draws the widget in the exact size of its cell (ADR 0049). Paperboy composes the page, selects it for each device with a plan and delivers it over the device API (ADR 0042). The devices run the Paperboy firmware on ESPHome (ADR 0041) with deep sleep, so a battery device wakes up only for its next sync.

```text
Plugin (calendar, weather, …) ◀──JSON lines on stdin/stdout──▶ paperboy ◀──POST /api/device/sync── Display (Paperboy firmware)
```

## Quick start

1. Create the instance directory with the fixed layout (ADR 0037): `paperboy.yaml`, `displays/`, `pages/`, `plans/`, `devices/` and, for your own plugins, `plugins/`.
2. Configure `paperboy.yaml`: copy `cmd/paperboy/paperboy.example.yaml` to `<instance>/paperboy.yaml` and adapt the `plugins` section.
3. Add the display types: copy the files from `examples/displays`, or create them in the editor at `/ui/editor?tab=displays` (ADR 0071).
4. Add pages and a plan: copy files from `examples/pages` and `examples/plans` into the instance.
5. Add one device file under `devices/` for every display (ADR 0070). Copy `examples/devices/wohnzimmer.yaml`, or create the device in the editor. Set `plan: standard` in the device file, or assign the plan in the editor.
6. Put the plugins into `plugins/`: unpack `paperboy-plugins-<os>-<arch>.zip` in the instance directory. The zip is attached to each release; otherwise it is an artifact of the *Plugins* workflow. You can also run `./scripts/package-plugins.sh` to build the zip, or let Paperboy fetch the programs with a `download` section (see [Downloads](docs/releases.md#downloads)). The zip creates versioned programs such as `plugins/weather-v1.4.0` (ADR 0075).
7. Start the server:
   ```sh
   go run ./cmd/paperboy -config . -listen :8090
   # show a page without a device:
   curl -o page.png http://127.0.0.1:8090/api/preview/e1001/uebersicht
   ```
   You can also run Paperboy in a container (`Dockerfile`, `compose.example.yaml`). The image contains Paperboy only. Use the `linux/amd64` plugin zip in the mounted instance directory.
8. Flash the Paperboy firmware (ADR 0041, see [firmware/esphome/README.md](firmware/esphome/README.md)). The release images are in `paperboy-firmware.zip`, or Paperboy fetches them for you (see [Downloads](docs/releases.md#downloads)). Enter `http://<computer>:8090` as the Paperboy URL in the web UI of the device. The device registers with its MAC address. Paperboy rejects and logs an unknown MAC address.

The MAC address in a device file is the allow list. Paperboy shows an unknown device as unclaimed hardware in the devices tab. *Claim* asks first, then opens a new device file with the MAC prefilled (ADR 0080). Without a plan, a device shows a notice page.

## Documentation

The README gives the overview and the quick start. The details are in these pages:

| Page | Contents |
|---|---|
| [Setup](docs/setup.md) | instance directory, configuration, display types and devices |
| [Pages and plans](docs/content.md) | pages, text pages, plans, actions and the device menu |
| [Runtime](docs/runtime.md) | web UI, device API, intervals and power, battery warning, firmware updates, device logs and security |
| [Plugins](docs/plugins/README.md) | the plugin host and protocol, and the shipped plugins |
| [Releases and downloads](docs/releases.md) | release tags and assets, downloads, channels and cleanup |
| [OpenDisplay](docs/opendisplay.md) | the OpenDisplay protocol and its test program |
| [Development](docs/development.md) | build, versioning, workflows and the repository layout |

## Licence

Paperboy is published under the [MIT licence](LICENSE). Third-party material keeps its own licence:

- The OpenMoji pictograms in `cmd/weather/icons/` and `cmd/evcc/icons/` are [CC BY-SA 4.0](cmd/weather/icons/README.md); the licence travels with the plugin zip.
- The firmware in `firmware/esphome/` is a Paperboy configuration for [ESPHome](https://esphome.io). The images built from it contain the ESPHome runtime, whose C++ parts are GPLv3; the sources are in this repository. The bundled Go fonts (`pkg/render`) come with their own BSD-style licence.
- The examples and the plugin test fixtures are fictional; xkcd comics are CC BY-NC 2.5 and are fetched at runtime, never shipped.
