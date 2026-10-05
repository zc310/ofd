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

## Noto Sans SC (思源黑体)

Noto Sans SC is used by the web reader as a remote fallback font when an OFD
document does not provide a suitable embedded font. The reader fetches the font
at runtime and does not include a copy in this repository. Two mirrors of the
same typeface are configured so that one stays available if the other is
unreachable:

- Primary: `@reogrid/font-sc` npm package, `NotoSansSC-Regular.ttf` (TrueType,
  used by the WASM renderer to read glyphs)
- Alternate: `@fontsource/noto-sans-sc` npm package,
  `noto-sans-sc-chinese-simplified-400-normal.woff2` (WOFF2, smaller; used
  when registering a browser `FontFace`)

Both packages redistribute unmodified upstream font binaries.

- Source: <https://github.com/notofonts/noto-cjk>
- Copyright: Google LLC
- License: SIL Open Font License 1.1
- License text: <https://scripts.sil.org/OFL>
- Reserved Font Names: `Noto`
