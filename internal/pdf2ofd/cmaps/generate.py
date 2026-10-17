#!/usr/bin/env python3
"""打包 Adobe Encoding CMap 与 CID→Unicode 数据，并保留原始许可声明。

Adobe-GB1-UCS2 由 internal/cidmap 共用，不在本压缩包中重复存储。

用法：python3 generate.py /usr/share/poppler/cMap
输入为 poppler-data 分发的 Adobe cmap-resources 与 mapping-resources-pdf。
只复制映射数据，不包含阅读器实现。
"""

import hashlib
import pathlib
import re
import sys
import zipfile

root = pathlib.Path(sys.argv[1])
output = pathlib.Path(__file__).parent
collections = ("Adobe-GB1", "Adobe-CNS1", "Adobe-Japan1", "Adobe-Korea1")
resources = {}
for collection in collections:
    for path in sorted((root / collection).iterdir()):
        if path.name == "Adobe-GB1-UCS2":
            continue
        data = path.read_bytes()
        forward = (path.name.endswith(("-H", "-V")) or path.name in ("H", "V")) and re.search(rb"/CMapType\s+1\s+def", data)
        if forward or path.name == collection + "-UCS2":
            resources[path.name] = data

# 继承资源必须完整；缺失时终止打包，避免生成不完整的映射。
for name, data in resources.items():
    for base in re.findall(rb"/(\S+)\s+usecmap", data):
        if base.decode() not in resources:
            raise ValueError(f"{name}: missing inherited resource {base!r}")

with zipfile.ZipFile(output / "adobe.zip", "w", compression=zipfile.ZIP_DEFLATED,
                     compresslevel=9) as archive:
    for name, data in sorted(resources.items()):
        info = zipfile.ZipInfo(name, date_time=(2000, 1, 1, 0, 0, 0))
        info.compress_type = zipfile.ZIP_DEFLATED
        info.external_attr = 0o100644 << 16
        archive.writestr(info, data, compresslevel=9)

manifest = "".join(f"{hashlib.sha256(data).hexdigest()}  {name}\n"
                   for name, data in sorted(resources.items()))
(output / "SHA256SUMS").write_text(manifest)
print(f"Packed {len(resources)} resources, {(output / 'adobe.zip').stat().st_size} bytes")
