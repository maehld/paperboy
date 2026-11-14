# Vendored UI code

The operator pages are server-rendered Go templates with hand-written
JavaScript and no build step (ADR 0068). One third-party file is the exception,
because the pages that used to refresh with `<meta http-equiv="refresh">` must
keep their scroll position, an opened *Details* block and their loaded images
while they update (ADR 0106).

| File | What it is |
|---|---|
| `htmx-4.0.1.min.js` | [htmx](https://htmx.org) 4.0.1, unchanged, from <https://cdn.jsdelivr.net/npm/htmx.org@4.0.1/dist/htmx.min.js> (37 448 bytes, SHA-256 `a0abb04f2f45a1f89d1d4844023245537e5e6014c87b144553e0607b4cc32130`) |
| `htmx.LICENSE` | the 0BSD licence htmx ships under |

The binary embeds the file (`go:embed`) and serves it as `/ui/htmx.js` behind
the operator password. An upgrade replaces the file, the embed line and the
version in `README.md`; it is the only place where the UI takes foreign code,
so nothing else may be added here without an ADR.
