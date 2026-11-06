# Weather icons

The PNGs in this directory are taken unchanged from [OpenMoji](https://openmoji.org)
(the black style, 618x618 PNG) and are licensed under
[CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/). The weather
plugin embeds them and reduces each picture to a mask of opaque dark pixels
(`pkg/render.NewMask`), so a single file works on 1-bit and 4-gray panels.

| File | Emoji | Code point | Source |
|---|---|---|---|
| `sun.png` | ☀️ | U+2600 | <https://github.com/hfg-gmuend/openmoji/blob/master/black/618x618/2600.png> |
| `cloud.png` | ☁️ | U+2601 | <https://github.com/hfg-gmuend/openmoji/blob/master/black/618x618/2601.png> |
| `rain.png` | 🌧️ | U+1F327 | <https://github.com/hfg-gmuend/openmoji/blob/master/black/618x618/1F327.png> |
| `snow.png` | 🌨️ | U+1F328 | <https://github.com/hfg-gmuend/openmoji/blob/master/black/618x618/1F328.png> |
| `thunder.png` | 🌩️ | U+1F329 | <https://github.com/hfg-gmuend/openmoji/blob/master/black/618x618/1F329.png> |
| `tshirt.png` | 👕 | U+1F455 | <https://github.com/hfg-gmuend/openmoji/blob/master/black/618x618/1F455.png> |
| `pullover.png` | 🧥 | U+1F9E5 | <https://github.com/hfg-gmuend/openmoji/blob/master/black/618x618/1F9E5.png> |
