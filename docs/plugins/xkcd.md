# xkcd plugin

Comics from [xkcd](https://xkcd.com) through its JSON API (`info.0.json`). The plugin downloads the image from the comic's `img` URL in the background and sends a change event when another comic is shown.

## Configuration

| Key | Description |
|---|---|
| `comic` | `latest` (default), `random` or a comic number |
| `refresh` | How often `latest` is checked, default `1h`, at least `1m` |
| `rotate` | How often `random` picks a new comic, default `24h` |
| `base` | Another xkcd address |

## Widget `comic`

The comic is scaled to fit the cell with its aspect ratio kept (Catmull-Rom on gray values, transparent areas turn white) and then reduced to the display's gray levels. Very wide strips and very tall comics only get smaller. Text sizes follow the page's base text.

Args: `title: true` adds the number and title above the comic, `alt: true` adds the alt text below.

The text variant shows `#number title` and the wrapped alt text.

The comics are by Randall Munroe and licensed under [CC BY-NC 2.5](https://creativecommons.org/licenses/by-nc/2.5/). Paperboy does not redistribute them, but keep the attribution and non-commercial terms in mind when you show them.
