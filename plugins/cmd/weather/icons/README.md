# Weather icons

The PNGs in this directory are taken unchanged from [OpenMoji](https://openmoji.org)
(the black style, 618x618 PNG) and are licensed under
[CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/). The weather
plugin embeds them and reduces each picture to a mask of opaque dark pixels
(`pkg/render.NewMask`), so a single file works on 1-bit and 4-gray panels.

| File | Emoji | Code point | Use |
|---|---|---|---|
| `sun.png` | ☀️ | U+2600 | clear |
| `cloud.png` | ☁️ | U+2601 | cloudy |
| `rain.png` | 🌧️ | U+1F327 | rain |
| `snow.png` | 🌨️ | U+1F328 | snow |
| `thunder.png` | 🌩️ | U+1F329 | thunder |
| `tshirt.png` | 👕 | U+1F455 | T-shirt, short sleeves |
| `pullover.png` | 🧥 | U+1F9E5 | long-sleeve shirt and pullover |
| `jacket.png` | 🥼 | U+1F97C | jacket, winter coat, rain jacket |
| `trousers.png` | 👖 | U+1F456 | long trousers |
| `shorts.png` | 🩳 | U+1FA73 | shorts |
| `scarf.png` | 🧣 | U+1F9E3 | scarf |
| `hat.png` | 🧢 | U+1F9E2 | hat |
| `gloves.png` | 🧤 | U+1F9E4 | gloves |

Every file comes from
`https://github.com/hfg-gmuend/openmoji/blob/master/black/618x618/<code point without U+>.png`.
