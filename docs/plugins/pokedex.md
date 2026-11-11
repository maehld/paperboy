# pokedex plugin

Pokémon from [PokéAPI](https://pokeapi.co), no API key needed.

## Configuration

| Key | Description |
|---|---|
| `pokemon` | Name or national dex number |
| `mode` | `fixed` (the default when `pokemon` is set) or `random` |
| `rotate` | How often a random Pokémon changes, default `24h`, at least `1h`; aligned to local midnight and stable across restarts |
| `generations` | List of generations 1 to 9, e.g. `[1, 2, 3]`; limits the random pick |
| `base` | Another PokéAPI address |

## Widget `card`

The card shows the artwork, the name in the request language (from the species names, with English as fallback), the number, types, height and weight, the six base stats as bars and the flavor text in the request language. There are image and text variants.

Arguments:

| Key | Description |
|---|---|
| `seed` | Any label, e.g. the name of a device. Every seed draws its own random Pokémon, all of them from the same rotation interval |

Without a seed every use of the plugin shows the same Pokémon. With a seed the pick depends on the rotation interval and the seed, so it stays stable across restarts and still changes at the same moment for all seeds.

The argument lives in the page, and Paperboy composes a page per display type rather than per device. Two devices of the same type therefore show the same card until they use different pages:

```yaml
cells:
  - { at: [0, 0], span: [6, 8], widget: pokedex/card, args: { seed: kueche } }
```

Each distinct seed of a request costs one Pokémon (a handful of requests to PokéAPI) per rotation.

The plugin downloads the artwork at runtime and reduces it to the display's gray levels with error diffusion; it is never stored or shipped. It caches responses in memory, sends requests one at a time with a pause between them, and reports a change when the Pokémon rotates.

Pokémon and their artwork belong to Nintendo, Game Freak and The Pokémon Company. The plugin only fetches them for private display.
