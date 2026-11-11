"""验证安装包布局与系统文件关联，不在测试机安装程序或更改默认应用。"""

from pathlib import Path
import importlib.util
import json
import os
import plistlib
import shutil
import struct
import subprocess
import sys
import tempfile
import unittest
import zipfile

from package import package
from generate_icon import generate_icon


class PackageTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        # 不依赖工作区已有 ICO；图像测试在临时目录中验证实际生成产物。
        cls.icon = None
        if all(importlib.util.find_spec(name) is not None for name in ["cairosvg", "PIL"]):
            directory = tempfile.TemporaryDirectory()
            cls.addClassCleanup(directory.cleanup)
            cls.icon = Path(directory.name) / "icon.ico"
            generate_icon(cls.icon)

    def require_icon(self):
        if self.icon is None:
            self.skipTest("生成 Windows 图标需要 CairoSVG 和 Pillow")
        return self.icon

    def test_build_and_linux_package_use_viewer_wails_name(self):
        directory = Path(__file__).resolve().parent
        targets = (directory / "targets.mk").read_text()
        self.assertIn('WAILS_BINARY = $(BIN_DIR)/ofd-viewer-wails$(BIN_SUFFIX)', targets)
        self.assertIn('WAILS_PACKAGE = $(DIST_DIR)/ofd-viewer-wails-$(PLATFORM).zip', targets)
        self.assertIn('./cmd/ofd-viewer-wails', targets)
        self.assertIn('/ofd-viewer-wails-$(PLATFORM)-setup.exe', targets)
        config = (directory / "nfpm.yaml").read_text()
        self.assertIn('name: zc310-ofd-viewer-wails\n', config)
        self.assertIn('dst: /usr/bin/ofd-viewer-wails\n', config)
        self.assertIn('dst: /usr/share/doc/zc310-ofd-viewer-wails/README.md\n', config)

    @unittest.skipUnless(shutil.which("make"), "未安装 make，跳过图标构建依赖检查")
    def test_make_generates_windows_icon_only_when_missing_or_outdated(self):
        targets = Path(__file__).with_name("targets.mk").resolve()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "cmd/ofd-viewer/share/io.github.zc310.ofd.svg"
            script = root / "cmd/ofd-viewer-wails/packaging/generate_icon.py"
            icon = script.with_name("icon.ico")
            source.parent.mkdir(parents=True)
            script.parent.mkdir(parents=True)
            source.write_text("<svg/>")
            # 用桩隔离 Go 编译与图像依赖，只验证 Make 的执行顺序及增量规则。
            script.write_text('from pathlib import Path\nPath(__file__).with_name("icon.ico").write_bytes(b"generated")\n')

            def make(*arguments):
                result = subprocess.run(
                    ["make", "--no-print-directory", "-f", str(targets), "GO=true",
                     "GOHOSTOS=linux", f"WAILS_PYTHON={sys.executable}", *arguments],
                    cwd=root, capture_output=True, text=True,
                )
                self.assertEqual(result.returncode, 0, result.stderr)
                return result.stdout

            for target in ["build-wails", "package-wails", "package-wails-installer"]:
                plan = make("-n", "GOOS=windows", target)
                self.assertLess(plan.index("generate_icon.py"), plan.index(" build "))
            self.assertNotIn("generate_icon.py", make("-n", "GOOS=linux", "build-wails"))

            name = str(icon.relative_to(root))
            self.assertIn("generate_icon.py", make(name))
            self.assertEqual(icon.read_bytes(), b"generated")
            self.assertNotIn("generate_icon.py", make(name))
            for dependency in [source, script]:
                with self.subTest(dependency=dependency.name):
                    # 显式调整时间戳，不等待时钟或写入未来时间。
                    stamp = dependency.stat().st_mtime - 10
                    os.utime(icon, (stamp, stamp))
                    self.assertIn("generate_icon.py", make(name))
                    self.assertNotIn("generate_icon.py", make(name))

    def test_platform_archives_include_binary_documentation_and_associations(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary = root / "binary"
            binary.write_bytes(b"test executable")
            for platform in ["linux", "windows", "darwin"]:
                with self.subTest(platform=platform):
                    if platform == "windows":
                        self.require_icon()
                    output = root / f"{platform}.zip"
                    package(platform, binary, output, "9.8.7", icon=self.icon)
                    with zipfile.ZipFile(output) as archive:
                        names = archive.namelist()
                        prefix = "OFD Reader.app/Contents/Resources/" if platform == "darwin" else "ofd-viewer-wails/"
                        for name in ["README.md", "LICENSE", "NOTICE", "THIRD-PARTY-LICENSES", "ADOBE-GB1-NOTICE"]:
                            self.assertIn(prefix + name, names)
                        executable = {
                            "linux": "ofd-viewer-wails/ofd-viewer-wails",
                            "windows": "ofd-viewer-wails/ofd-viewer-wails.exe",
                            "darwin": "OFD Reader.app/Contents/MacOS/ofd-viewer-wails",
                        }[platform]
                        self.assertEqual(archive.read(executable), binary.read_bytes())
                        self.assertTrue(archive.getinfo(executable).external_attr >> 16 & 0o111)
                        if platform == "windows":
                            self.assertEqual(archive.read(prefix + "icon.ico"), self.icon.read_bytes())
                        if platform == "darwin":
                            info = plistlib.loads(archive.read("OFD Reader.app/Contents/Info.plist"))
                            self.assertEqual(info["CFBundleShortVersionString"], "9.8.7")
                            self.assertEqual(info["CFBundleIdentifier"], "io.github.zc310.ofd.wails")
                            self.assertEqual(info["CFBundleExecutable"], "ofd-viewer-wails")
                            self.assertEqual(info["CFBundleDocumentTypes"][0]["CFBundleTypeRole"], "Viewer")
                            self.assertEqual(info["CFBundleDocumentTypes"][0]["CFBundleTypeExtensions"], ["ofd"])
                        if platform == "linux":
                            desktop = archive.read(prefix + "io.github.zc310.ofd.wails.desktop").decode()
                            self.assertIn("Exec=ofd-viewer-wails -- %f\n", desktop)
                            self.assertIn("MimeType=application/ofd;", desktop)
                            self.assertIn(prefix + "io.github.zc310.ofd.wails.xml", names)
                            self.assertIn(prefix + "io.github.zc310.ofd.wails.svg", names)

    def test_windows_installer_preserves_existing_default_and_quotes_file_paths(self):
        source = Path(__file__).with_name("windows.nsi").read_text()
        self.assertIn('RequestExecutionLevel user', source)
        self.assertIn('Software\\Classes\\.ofd\\OpenWithProgids', source)
        self.assertIn('$\\"$INSTDIR\\ofd-viewer-wails.exe$\\" -- $\\"%1$\\"', source)
        self.assertNotIn('WriteRegStr HKCU "Software\\Classes\\.ofd"', source)
        self.assertNotIn('DeleteRegKey HKCU "Software\\Classes\\.ofd"', source)
        self.assertIn('DeleteRegValue HKCU "Software\\RegisteredApplications"', source)

    def test_windows_install_directory_is_namespaced_and_preserves_upgrade_location(self):
        source = Path(__file__).with_name("windows.nsi").read_text()
        self.assertIn('InstallDir "$LOCALAPPDATA\\Programs\\zc310\\ofd-viewer-wails"', source)
        self.assertIn('InstallDirRegKey HKCU "Software\\zc310\\OFD Reader Wails" "InstallDir"', source)
        self.assertIn('WriteRegStr HKCU "Software\\zc310\\OFD Reader Wails" "InstallDir" "$INSTDIR"', source)
        self.assertIn('ReadRegStr $0 HKCU "Software\\zc310\\OFD Reader Wails" "InstallDir"', source)
        self.assertIn('DeleteRegKey HKCU "Software\\zc310\\OFD Reader Wails"', source)
        self.assertIn('WriteRegStr HKCU "Software\\zc310\\OFD Reader Wails\\Capabilities\\FileAssociations" ".ofd" "OFD.Wails.Document"', source)
        self.assertIn('WriteRegStr HKCU "Software\\RegisteredApplications" "OFD Reader (Wails)" "Software\\zc310\\OFD Reader Wails\\Capabilities"', source)
        self.assertNotIn('Software\\OFD Reader Wails', source)
        self.assertNotIn('DeleteRegKey HKCU "Software\\zc310"', source)
        self.assertIn('RMDir "$INSTDIR"', source)
        self.assertNotIn('RMDir /r', source)
        self.assertNotIn('RMDir "$LOCALAPPDATA\\Programs\\zc310"', source)

    def test_windows_shortcut_installer_and_associations_use_installed_icon(self):
        source = Path(__file__).with_name("windows.nsi").read_text()
        self.assertIn('!define MUI_ICON "icon.ico"', source)
        self.assertIn('!define MUI_UNICON "icon.ico"', source)
        self.assertIn('  File "icon.ico"', source)
        self.assertIn('CreateShortcut "$SMPROGRAMS\\OFD Reader (Wails).lnk" "$INSTDIR\\ofd-viewer-wails.exe" "" "$INSTDIR\\icon.ico" 0', source)
        for key in ["DefaultIcon", "ApplicationIcon", "DisplayIcon"]:
            self.assertTrue(any(key in line and '$INSTDIR\\icon.ico' in line for line in source.splitlines()), key)
        self.assertIn('Delete "$INSTDIR\\icon.ico"', source)
        config = json.loads(Path(__file__).with_name("windows-resources.json").read_text())
        # Wails 窗口固定使用图标资源 ID 3，与 EXE 的首个图标保持一致。
        self.assertEqual(config["RT_GROUP_ICON"], {"#3": {"0000": "icon.ico"}})

    def test_windows_icon_contains_transparent_small_and_high_dpi_frames(self):
        icon = self.require_icon().read_bytes()
        reserved, kind, count = struct.unpack_from("<HHH", icon)
        self.assertEqual((reserved, kind, count), (0, 1, 7))
        sizes = set()
        end = 6 + count * 16
        for index in range(count):
            width, height, _, _, planes, depth, length, offset = struct.unpack_from("<BBBBHHII", icon, 6 + index * 16)
            size = width or 256
            sizes.add(size)
            self.assertEqual(height or 256, size)
            self.assertEqual((planes, depth), (1, 32))
            self.assertEqual(offset, end)
            end = offset + length
            payload = icon[offset:end]
            if size == 256:
                self.assertTrue(payload.startswith(b"\x89PNG\r\n\x1a\n"))
                self.assertEqual(struct.unpack_from(">II", payload, 16), (256, 256))
            else:
                # 小图标使用 NSIS 能读取的传统 DIB，并保留逐像素透明度。
                self.assertEqual(struct.unpack_from("<Iii", payload), (40, size, size * 2))
                alpha = payload[43:40 + size * size * 4:4]
                # 铺满画布的圆角在 16px 超采样后可能留下一级透明度误差。
                self.assertLessEqual(min(alpha), 1)
                self.assertIn(255, alpha)
        self.assertEqual(end, len(icon))
        self.assertEqual(sizes, {16, 24, 32, 48, 64, 128, 256})

    def test_windows_icon_uses_viewer_badges_instead_of_document_artwork(self):
        icon = self.require_icon().read_bytes()
        count = struct.unpack_from("<H", icon, 4)[0]
        for index in range(count):
            width, _, _, _, _, _, length, offset = struct.unpack_from("<BBBBHHII", icon, 6 + index * 16)
            if width != 64:
                continue
            payload = icon[offset:offset + length]

            def pixel(x, y):
                # 传统 DIB 的 BGRA 像素从底行开始存储，无需图像库即可验证品牌配色。
                start = 40 + ((63 - y) * 64 + x) * 4
                blue, green, red, alpha = payload[start:start + 4]
                return red, green, blue, alpha

            self.assertEqual(pixel(8, 32), (30, 136, 229, 255))
            self.assertEqual(pixel(33, 23), (229, 57, 53, 255))
            self.assertEqual(pixel(55, 33), (67, 160, 71, 255))
            self.assertEqual(pixel(32, 8), (250, 250, 250, 255))
            self.assertEqual(pixel(0, 0)[3], 0)
            return
        self.fail("ICO 缺少用于验证阅读器标识的 64px 图像")

    @unittest.skipUnless(os.environ.get("WAILS_WINDOWS_EXE"), "未指定 Windows 构建产物，跳过 PE 资源检查")
    def test_built_windows_executable_contains_wails_icon_group(self):
        data = Path(os.environ["WAILS_WINDOWS_EXE"]).read_bytes()
        pe = struct.unpack_from("<I", data, 0x3C)[0]
        self.assertEqual(data[pe:pe + 4], b"PE\0\0")
        sections = struct.unpack_from("<H", data, pe + 6)[0]
        optional = pe + 24
        optional_size = struct.unpack_from("<H", data, pe + 20)[0]
        magic = struct.unpack_from("<H", data, optional)[0]
        self.assertIn(magic, [0x10B, 0x20B])
        directories = optional + (112 if magic == 0x20B else 96)
        resource_rva = struct.unpack_from("<I", data, directories + 16)[0]
        self.assertNotEqual(resource_rva, 0, "EXE 缺少资源目录，快捷方式会显示默认图标")

        def file_offset(rva):
            for index in range(sections):
                start = optional + optional_size + index * 40
                virtual_size, address, raw_size, raw_offset = struct.unpack_from("<IIII", data, start + 8)
                if address <= rva < address + max(virtual_size, raw_size):
                    return raw_offset + rva - address
            self.fail(f"无效资源 RVA: {rva}")

        base = file_offset(resource_rva)

        def entries(relative):
            named, numbered = struct.unpack_from("<HH", data, base + relative + 12)
            return dict(struct.unpack_from("<II", data, base + relative + 16 + index * 8) for index in range(named + numbered))

        def resource(kind, identifier):
            node = 0
            for key in [kind, identifier]:
                entry = entries(node)[key]
                self.assertTrue(entry & 0x80000000)
                node = entry & 0x7FFFFFFF
            entry = next(iter(entries(node).values()))
            self.assertFalse(entry & 0x80000000)
            rva, length = struct.unpack_from("<II", data, base + entry)
            offset = file_offset(rva)
            return data[offset:offset + length]

        group = resource(14, 3)
        reserved, kind, count = struct.unpack_from("<HHH", group)
        self.assertEqual((reserved, kind, count), (0, 1, 7))
        sizes = set()
        for index in range(count):
            width, height, _, _, _, _, length, identifier = struct.unpack_from("<BBBBHHIH", group, 6 + index * 14)
            sizes.add(width or 256)
            self.assertEqual(width, height)
            self.assertEqual(len(resource(3, identifier)), length)
        self.assertEqual(sizes, {16, 24, 32, 48, 64, 128, 256})


if __name__ == "__main__":
    unittest.main()
