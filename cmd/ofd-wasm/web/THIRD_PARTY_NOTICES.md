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

## Adobe-GB1 mapping data

The WASM font repair engine embeds Adobe character collection mappings generated
from poppler-data. The font and PDF text mappings retain their distinct results.
Source hashes and regeneration instructions are in `internal/cidmap` in the source
distribution.

Adobe-GB1 CID-to-Unicode mapping
Copyright 1990-2018 Adobe Systems Incorporated.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

  Redistributions of source code must retain the above copyright notice, this
  list of conditions and the following disclaimer.

  Redistributions in binary form must reproduce the above copyright notice,
  this list of conditions and the following disclaimer in the documentation
  and/or other materials provided with the distribution.

  Neither the name of Adobe Systems Incorporated nor the names of its
  contributors may be used to endorse or promote products derived from this
  software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE FOR
ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
(INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES;
LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON
ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

Adobe-GB1-UCS2 CMap, version 9.000
Copyright 1990-2020 Adobe. All rights reserved.

Redistribution and use in source and binary forms, with or
without modification, are permitted provided that the
following conditions are met:

Redistributions of source code must retain the above
copyright notice, this list of conditions and the following
disclaimer.

Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following
disclaimer in the documentation and/or other materials
provided with the distribution.

Neither the name of Adobe nor the names of its contributors
may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND
CONTRIBUTORS "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES,
INCLUDING, BUT NOT LIMITED TO, THE IMPLIED WARRANTIES OF
MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR
CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT
NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES;
LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN
CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR
OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
