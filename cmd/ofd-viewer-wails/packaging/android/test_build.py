"""Android 生成工程的权限、版本和原生桥接回归。"""

import importlib.util
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
import xml.etree.ElementTree as ET


sys.dont_write_bytecode = True
SPEC = importlib.util.spec_from_file_location('android_build', Path(__file__).with_name('build.py'))
BUILD = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BUILD)


class AndroidProjectTest(unittest.TestCase):
    def test_generated_host_keeps_native_bridges_and_minimal_permissions(self):
        module = subprocess.check_output(['go', 'list', '-m', '-f', '{{.Dir}}',
                                          'github.com/wailsapp/wails/v3'], cwd=BUILD.ROOT, text=True).strip()
        template = Path(module) / 'internal/commands/build_assets/android'
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / 'android'
            for _ in range(2):
                BUILD.prepare_project(template, output, '1.2.3-test', 321, 'x86_64')
            activity = (output / 'app/src/main/java/com/wails/app/MainActivity.java').read_text()
            self.assertLess(activity.index('readerBridge = new ReaderBridge(this)'), activity.index('bridge.initialize()'))
            self.assertIn('readerBridge.onActivityResult(requestCode, resultCode, data)', activity)
            self.assertIn('window.ofdAndroidBack', activity)
            self.assertIn("ofd-android-pause", activity)
            self.assertIn('registerOnBackInvokedCallback', activity)
            self.assertIn('readerBridge.dispose()', activity)
            self.assertIn('return readerBridge.importDocument(uri)', activity)
            gradle = (output / 'app/build.gradle').read_text()
            for expected in ['minSdk = 24', 'versionCode = 321', 'versionName = "1.2.3-test"',
                             "abiFilters 'x86_64'", 'applicationId = "io.github.zc310.ofd.wails"']:
                self.assertIn(expected, gradle)
            self.assertNotIn('signingConfigs.debug', gradle)
            manifest = ET.parse(output / 'app/src/main/AndroidManifest.xml').getroot()
            android = '{http://schemas.android.com/apk/res/android}'
            tools = '{http://schemas.android.com/tools}'
            self.assertEqual({item.attrib[android + 'name'] for item in manifest.findall('uses-permission')
                              if item.attrib.get(tools + 'node') != 'remove'},
                             {'android.permission.INTERNET', 'android.permission.ACCESS_NETWORK_STATE'})
            self.assertEqual({item.attrib[android + 'name'] for item in manifest.findall('uses-permission')
                              if item.attrib.get(tools + 'node') == 'remove'},
                             {'android.permission.USE_BIOMETRIC', 'android.permission.USE_FINGERPRINT'})
            self.assertTrue((output / 'app/src/main/res/mipmap-nodpi/ofd_reader.png').is_file())
            self.assertEqual(manifest.find('application').attrib[android + 'allowBackup'], 'false')

    def test_changed_framework_template_fails_explicitly(self):
        with self.assertRaisesRegex(RuntimeError, '模板已变化'):
            BUILD.replace_once('unrelated', 'missing', 'replacement')


if __name__ == '__main__':
    unittest.main()
