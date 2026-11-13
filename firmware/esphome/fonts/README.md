# Fonts of the firmware

`Go-Mono-Bold.ttf` is Go Mono Bold, the face of the Paperboy wordmark
(`docs/logo`). The firmware prints the wordmark and the firmware version with it
(ADR 0098). Go Mono is not on Google Fonts, so the file ships with the firmware
instead of being downloaded at build time like Inter.

It is a subset of `font/gofont/ttfs/Go-Mono-Bold.ttf` from
`golang.org/x/image` v0.24.0 — the module `pkg/render` draws the same face from —
and holds the letters of `paperboy`, the digits, `v` and `.`. Regenerate it with:

```sh
pyftsubset Go-Mono-Bold.ttf \
  --text="paperboyv0123456789." \
  --name-IDs='*' \
  --output-file=Go-Mono-Bold.ttf
```

The fonts were created by the Bigelow & Holmes foundry for the Go project and
are licensed under the same BSD-3-Clause licence as the Go project, see
[`LICENSE`](LICENSE). A firmware that contains their rasterized glyphs must keep
that notice.
