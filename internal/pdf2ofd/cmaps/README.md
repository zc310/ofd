# Adobe CMap resources

`adobe.zip` contains 150 horizontal/vertical Encoding CMaps (CMapType 1) and three CID-to-Unicode
tables for Adobe-CNS1, Adobe-Japan1 and Adobe-Korea1, from poppler-data
0.4.12. Each original resource includes Adobe's copyright and BSD redistribution
terms. The archive retains those notices verbatim and is embedded in the converter;
conversion needs no system CMap installation or network access.

Adobe-GB1 CID-to-Unicode data is shared through
[`internal/cidmap`](../../cidmap/README.md).
`GB1Text` retains the original `Adobe-GB1-UCS2` text-extraction results, including
variation sequences; `GB1GlyphUnicode` preserves the separate font-repair mapping.
The GB1 Encoding CMaps remain here because PDF character-code decoding is separate
from CID-to-Unicode lookup. `Adobe-GB1-UCS2` is deliberately excluded from the archive
and its manifest to avoid duplicating the shared data. Its original SHA-256 is
`368b40ec05568faf323cfbbddf23c60d566bbc88e366981eb6541b026381a3b8`;
source hashes, generation instructions and Adobe notices are maintained in that package.

Upstream data:

- https://github.com/adobe-type-tools/cmap-resources
- https://github.com/adobe-type-tools/mapping-resources-pdf

`SHA256SUMS` records the hashes of the original, uncompressed resources. To rebuild
the deterministic archive from an installed poppler-data distribution:

```sh
python3 internal/pdf2ofd/cmaps/generate.py /usr/share/poppler/cMap
```

This directory contains mapping data and a packaging script. Reader source code
is not included. The runtime parses only the requested maps, caches immutable
tables, keeps inherited maps separate, and does not expand large code ranges.
GB1 lookups use the shared table directly and do not build a second CID-to-Unicode
cache. Regression tests fingerprint all 65,536 CID inputs against the original parser.
