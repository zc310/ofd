# Third-Party Notices

## Google Material Symbols

`material-symbols-outlined-subset.woff2` is a subset of Google Material Symbols
Outlined. It contains only the icon ligatures listed in `icons.json`, which is
the single source of truth for the icon set: the font is generated from that
file, and `test/icons.test.mjs` fails if the code uses an icon missing from it.
Regenerate the font after editing `icons.json` (see `Makefile` target
`wasm-icon-font`), then recompute the Service Worker `CACHE_NAME`.

- Source: <https://fonts.google.com/icons>
- License: Apache License 2.0
- License text: <https://www.apache.org/licenses/LICENSE-2.0>

## Smiley Sans (得意黑)

Smiley Sans is used by the web reader as a remote fallback font when an OFD
document does not provide a suitable embedded font. The reader uses the
original font files and does not include a modified copy in this repository.

- Source: <https://github.com/atelier-anchor/smiley-sans>
- Copyright: 2022--2024 atelierAnchor <https://atelier-anchor.com>
- License: SIL Open Font License 1.1
- License text: <https://scripts.sil.org/OFL>
- Reserved Font Names: `Smiley`, `得意黑`
