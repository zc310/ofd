#!/usr/bin/env python3
"""从 go.mod 锁定的 Wails 模板生成 Android 宿主；产物不写入源码目录。"""

import argparse
import os
from pathlib import Path
import platform
import shutil
import shlex
import subprocess

import cairosvg


ROOT = Path(__file__).resolve().parents[4]
SOURCE = Path(__file__).resolve().parent


def replace_once(text, old, new):
    if text.count(old) != 1:
        raise RuntimeError(f"Wails Android 模板已变化，请复核适配点: {old[:100]}")
    return text.replace(old, new, 1)


def prepare_project(template, output, version, code, abi):
    # Go 模块缓存为只读；只复制内容，不能把只读权限传播到生成工程。
    output.mkdir(parents=True, exist_ok=True)
    output.chmod(0o755)
    for source in template.rglob('*'):
        target = output / source.relative_to(template)
        if source.is_dir():
            target.mkdir(parents=True, exist_ok=True)
            target.chmod(0o755)
        else:
            if target.exists():
                target.chmod(0o644)
            shutil.copyfile(source, target)
    main = output / "app/src/main"
    icon = main / 'res/mipmap-nodpi/ofd_reader.png'
    icon.parent.mkdir(parents=True, exist_ok=True)
    cairosvg.svg2png(bytestring=(ROOT / 'cmd/ofd-viewer/share/io.github.zc310.ofd.svg').read_bytes(),
                    write_to=str(icon), output_width=512, output_height=512)
    shutil.copy2(SOURCE / "AndroidManifest.xml", main / "AndroidManifest.xml")
    java = main / "java/com/wails/app"
    shutil.copy2(SOURCE / "ReaderBridge.java", java / "ReaderBridge.java")
    activity = (java / "MainActivity.java").read_text()
    activity = replace_once(activity, "private WailsBridge bridge;", "private WailsBridge bridge;\n    private ReaderBridge readerBridge;")
    activity = replace_once(activity, "bridge.initialize();", "readerBridge = new ReaderBridge(this);\n        bridge.initialize();")
    activity = replace_once(activity, "setupWebView();", """setupWebView();
        if (Build.VERSION.SDK_INT >= 33) {
            getOnBackInvokedDispatcher().registerOnBackInvokedCallback(
                android.window.OnBackInvokedDispatcher.PRIORITY_DEFAULT, this::onBackPressed);
        }""")
    activity = replace_once(activity, "super.onActivityResult(requestCode, resultCode, data);", """super.onActivityResult(requestCode, resultCode, data);
        if (readerBridge != null && readerBridge.onActivityResult(requestCode, resultCode, data)) return;""")
    activity = replace_once(activity, "protected void onPause() {", """protected void onPause() {
        if (webView != null) webView.evaluateJavascript(
            "window.dispatchEvent(new Event('ofd-android-pause'))", null);""")
    activity = replace_once(activity, "protected void onDestroy() {", """protected void onDestroy() {
        if (readerBridge != null) readerBridge.dispose();""")
    activity = replace_once(activity, """if (webView != null && webView.canGoBack()) {
            webView.goBack();
        } else {
            super.onBackPressed();
        }""", """if (webView == null) { moveTaskToBack(true); return; }
        webView.evaluateJavascript("Boolean(window.ofdAndroidBack && window.ofdAndroidBack())",
            result -> { if (!"true".equals(result)) moveTaskToBack(true); });""")
    # 阅读器仅接受有界 content URI 副本；不用框架无界导入逻辑。
    marker = "    private String copyUriToCache(Uri uri) {"
    if activity.count(marker) != 1:
        raise RuntimeError('Wails Android 文件导入模板已变化，请复核适配点')
    start = activity.index(marker)
    end = activity.index("\n    /**", start)
    activity = activity[:start] + """    private String copyUriToCache(Uri uri) {
        return readerBridge.importDocument(uri);
    }
""" + activity[end:]
    (java / "MainActivity.java").write_text(activity)
    gradle = (output / "app/build.gradle").read_text()
    gradle = replace_once(gradle, 'applicationId = "com.wails.app"', 'applicationId = "io.github.zc310.ofd.wails"')
    gradle = replace_once(gradle, 'minSdk = 21', 'minSdk = 24')
    gradle = replace_once(gradle, 'versionCode = 1', f'versionCode = {code}')
    gradle = replace_once(gradle, 'versionName = "1.0"', f'versionName = "{version}"')
    gradle = replace_once(gradle, "abiFilters 'arm64-v8a', 'x86_64'", f"abiFilters '{abi}'")
    # 发布包不得静默使用调试证书；无发布密钥时输出未签名包。
    gradle = replace_once(gradle, 'signingConfig = hasKeystore ? signingConfigs.release : signingConfigs.debug',
                          'if (hasKeystore) signingConfig = signingConfigs.release')
    gradle = replace_once(gradle, '''// A real keystore can be provided via environment variables; without
        // one, release builds are signed with the debug keystore so they can
        // be installed for testing (not suitable for Play Store uploads).''',
                          '// 发布签名由环境变量提供；缺少密钥时输出未签名包。')
    (output / "app/build.gradle").write_text(gradle)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--go', default='go')
    parser.add_argument('--version', required=True)
    parser.add_argument('--version-code', type=int, default=1)
    parser.add_argument('--arch', choices=['arm64', 'amd64'], default='arm64')
    parser.add_argument('--format', choices=['native', 'apk', 'aab'], default='native')
    parser.add_argument('--release', action='store_true')
    parser.add_argument('--gradle', default=os.environ.get('WAILS_ANDROID_GRADLE', ''),
                        help='可选的本机 Gradle 命令（含参数）；默认使用模板 wrapper')
    parser.add_argument('--build-dir', type=Path, required=True)
    parser.add_argument('--out', type=Path, required=True)
    args = parser.parse_args()
    if not args.version or any(c not in '0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ.-+' for c in args.version):
        parser.error('版本名仅允许字母、数字和 .-+')
    if not 1 <= args.version_code <= 2100000000:
        parser.error('version-code 必须为 1-2100000000')
    sdk = os.environ.get('ANDROID_HOME') or os.environ.get('ANDROID_SDK_ROOT')
    if not sdk or not Path(sdk).is_dir():
        parser.error('请设置 ANDROID_HOME 或 ANDROID_SDK_ROOT')
    ndk = os.environ.get('ANDROID_NDK_HOME') or os.environ.get('ANDROID_NDK_ROOT')
    if not ndk:
        parser.error('请设置 ANDROID_NDK_HOME，建议 NDK r28 或更新版本')
    host = {'Linux': 'linux-x86_64', 'Darwin': 'darwin-x86_64'}.get(platform.system())
    if not host:
        parser.error('此脚本支持 Linux/macOS 构建主机')
    toolchain = Path(ndk) / 'toolchains/llvm/prebuilt' / host / 'bin'
    triple, abi = ('aarch64-linux-android', 'arm64-v8a') if args.arch == 'arm64' else ('x86_64-linux-android', 'x86_64')
    cc = toolchain / f'{triple}24-clang'
    if not cc.is_file():
        parser.error(f'找不到 Android C 编译器: {cc}')
    env = dict(os.environ, GOOS='android', GOARCH=args.arch, CGO_ENABLED='1', CC=str(cc), CXX=str(cc) + '++')
    module = subprocess.check_output([args.go, 'list', '-m', '-f', '{{.Dir}}', 'github.com/wailsapp/wails/v3'], cwd=ROOT, text=True).strip()
    output = args.build_dir.resolve() / args.arch
    prepare_project(Path(module) / 'internal/commands/build_assets/android', output, args.version, args.version_code, abi)
    library = output / 'app/src/main/jniLibs' / abi / 'libwails.so'
    library.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run([args.go, 'build', '-trimpath', '-buildmode=c-shared', '-ldflags',
                    f'-s -w -X github.com/zc310/ofd/internal/version.Version={args.version}',
                    '-o', str(library), './cmd/ofd-viewer-wails'], cwd=ROOT, env=env, check=True)
    if args.format == 'native':
        print(library)
        return
    build_type = 'release' if args.release else 'debug'
    task = ('assemble' if args.format == 'apk' else 'bundle') + build_type.capitalize()
    command = shlex.split(args.gradle) if args.gradle else ['sh', './gradlew']
    subprocess.run([*command, '--no-daemon', task], cwd=output,
                   env=dict(os.environ, ANDROID_HOME=sdk, ANDROID_SDK_ROOT=sdk), check=True)
    directory = output / 'app/build/outputs' / ('apk' if args.format == 'apk' else 'bundle') / build_type
    artifacts = list(directory.glob(f'*.{args.format}'))
    if len(artifacts) != 1:
        raise RuntimeError(f'无法唯一确定 Android 产物: {directory}')
    args.out.mkdir(parents=True, exist_ok=True)
    target = args.out / f'ofd-viewer-wails-android-{args.arch}-{build_type}.{args.format}'
    shutil.copy2(artifacts[0], target)
    print(target)


if __name__ == '__main__':
    main()
