#!/usr/bin/env python3
"""打包 Wails 阅读器；只使用临时目录，不清理或覆盖工作区源码。"""

import argparse
from pathlib import Path
import plistlib
import shutil
import tempfile
import zipfile


def package(platform, binary, output, version, icon=None):
    root = Path(__file__).resolve().parents[3]
    output = Path(output).resolve()
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="ofd-viewer-wails-", dir=output.parent) as directory:
        stage = Path(directory)
        if platform == "darwin":
            contents = stage / "OFD Reader.app" / "Contents"
            executable = contents / "MacOS" / "ofd-viewer-wails"
            executable.parent.mkdir(parents=True)
            info = {
                "CFBundleName": "OFD Reader",
                "CFBundleDisplayName": "OFD Reader",
                "CFBundleIdentifier": "io.github.zc310.ofd.wails",
                "CFBundleExecutable": "ofd-viewer-wails",
                "CFBundlePackageType": "APPL",
                "CFBundleShortVersionString": version,
                "CFBundleVersion": version,
                "NSHighResolutionCapable": True,
                "CFBundleDocumentTypes": [{
                    "CFBundleTypeName": "OFD document",
                    "CFBundleTypeRole": "Viewer",
                    "LSHandlerRank": "Alternate",
                    "LSItemContentTypes": ["io.github.zc310.ofd.document"],
                    "CFBundleTypeExtensions": ["ofd"],
                }],
                "UTImportedTypeDeclarations": [{
                    "UTTypeIdentifier": "io.github.zc310.ofd.document",
                    "UTTypeDescription": "OFD document",
                    "UTTypeConformsTo": ["public.data"],
                    "UTTypeTagSpecification": {
                        "public.filename-extension": ["ofd"],
                        "public.mime-type": "application/ofd",
                    },
                }],
            }
            (contents / "Info.plist").write_bytes(plistlib.dumps(info))
            docs = contents / "Resources"
        else:
            docs = stage / "ofd-viewer-wails"
            docs.mkdir()
            executable = docs / ("ofd-viewer-wails.exe" if platform == "windows" else "ofd-viewer-wails")
            if platform == "windows":
                shutil.copy2(icon if icon is not None else root / "cmd/ofd-viewer-wails/packaging/icon.ico", docs / "icon.ico")
            if platform == "linux":
                for name in ["io.github.zc310.ofd.wails.desktop"]:
                    shutil.copy2(root / "cmd/ofd-viewer-wails/packaging" / name, docs / name)
                shutil.copy2(root / "cmd/ofd-wasm/web/icon.svg", docs / "io.github.zc310.ofd.wails.svg")
                shutil.copy2(root / "cmd/ofd-viewer/share/io.github.zc310.ofd.xml", docs / "io.github.zc310.ofd.wails.xml")
        shutil.copy2(binary, executable)
        executable.chmod(0o755)
        docs.mkdir(parents=True, exist_ok=True)
        for name in ["LICENSE", "NOTICE", "THIRD-PARTY-LICENSES"]:
            shutil.copy2(root / name, docs / name)
        shutil.copy2(root / "cmd/ofd-viewer-wails/README.md", docs / "README.md")
        shutil.copy2(root / "internal/cidmap/NOTICE", docs / "ADOBE-GB1-NOTICE")
        # 最后原子提交 ZIP，失败不留下半个发行包。
        archive = stage / "package.zip"
        with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED) as target:
            for file in sorted(stage.rglob("*")):
                if file.is_file() and file != archive:
                    target.write(file, file.relative_to(stage))
        archive.replace(output)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--platform", choices=["linux", "windows", "darwin"], required=True)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--version", required=True)
    args = parser.parse_args()
    package(args.platform, args.binary, args.output, args.version)
