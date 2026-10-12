# Adobe CMap resources

`adobe.zip` contains 150 horizontal/vertical Encoding CMaps (CMapType 1) and four CID-to-Unicode
tables for Adobe-GB1, Adobe-CNS1, Adobe-Japan1 and Adobe-Korea1, from poppler-data
0.4.12. Each original resource includes Adobe's copyright and BSD redistribution
terms. The archive retains those notices verbatim and is embedded in the converter;
conversion needs no system CMap installation or network access.

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
