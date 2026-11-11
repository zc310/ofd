import { readFileSync } from 'node:fs';
import { sliceFunction, load, createSuite } from './harness.mjs';

const suite = createSuite('关于对话框发行版本');
const aboutCode = ['configureAboutDialog', 'openAboutDialog', 'updateAboutVersion', 'closeAboutDialog', 'openAboutProjectWebsite']
  .map(sliceFunction).join('\n');

function stateFor(engine) {
  const state = {
    engine,
    documentMenuToggle: { focus() { state.menuFocused = true; } },
    setDocumentMenuOpen(open) { state.menuOpen = open; },
    versionLink: { textContent: '加载中…' },
    aboutFields: new Map(),
    aboutDialog: {
      querySelector(selector) {
        if (!state.aboutFields.has(selector)) state.aboutFields.set(selector, { textContent: '浏览器默认内容' });
        return state.aboutFields.get(selector);
      },
      showModal() { state.shown = true; },
      close() { state.shown = false; },
      setAttribute(name) { state.dialogAttribute = name; },
      removeAttribute() { state.dialogAttribute = undefined; },
    },
    aboutClose: { focus() { state.focused = true; } },
    setStatus(message) { state.status = message; },
  };
  return load(state, aboutCode, { expose: { open: 'openAboutDialog', close: 'closeAboutDialog',
    configure: 'configureAboutDialog', project: 'openAboutProjectWebsite' } });
}

suite.group('版本来源与显示');
const immediate = stateFor({ version: '1.2.3', ready: Promise.resolve() });
immediate.open();
suite.check(immediate.versionLink.textContent === 'v1.2.3', '已就绪时显示运行时版本并添加 v 前缀');
suite.check(immediate.shown && immediate.focused, '显示对话框并聚焦关闭按钮');
suite.check(immediate.menuOpen === false && immediate.menuFocused, '先关闭右侧菜单并设置可见的焦点返回位置');

let markReady;
const ready = new Promise(resolve => { markReady = resolve; });
const delayedEngine = { version: '', ready };
const delayed = stateFor(delayedEngine);
delayed.open();
suite.check(delayed.versionLink.textContent === '加载中…', 'WASM 尚未就绪时显示加载提示');
delayedEngine.version = '4.5.6';
markReady();
await ready;
await new Promise(resolve => setImmediate(resolve));
suite.check(delayed.versionLink.textContent === 'v4.5.6', 'WASM 就绪后更新为注入版本');

suite.group('两端内容与桌面版本');
immediate.configure();
suite.check(immediate.aboutFields.size === 0, '浏览器保留 HTML 中的 WASM、回退字体和移动端说明');
const html = readFileSync(new URL('../index.html', import.meta.url), 'utf8');
suite.check(html.includes('关于 OFD WASM 阅读器') && html.includes('id="about-description"')
  && html.includes('id="about-fonts"') && html.includes('id="about-platform-feature"')
  && html.includes('id="about-technology"'), '共享 HTML 提供两端共用的关于字段');
const brand = /<div class="brand">([\s\S]*?)<\/div>/.exec(html)?.[1] || '';
const menu = /<div id="document-menu"[^>]*>([\s\S]*?)<\/div>/.exec(html)?.[1] || '';
const menuToggle = /<button id="document-menu-toggle"[^>]*>/.exec(html)?.[0] || '';
suite.check(brand.includes('id="document-name"') && !brand.includes('about-link') && !brand.includes('OFD'), '左侧只保留文档名称，不重复应用标题');
suite.check(menu.includes('<button id="about-link"') && menu.includes('aria-controls="about-dialog"'), '关于入口为右侧菜单中的可键盘操作按钮');
suite.check(menuToggle.includes('aria-label="更多"') && !menuToggle.includes('disabled'), '更多菜单不依赖文档打开状态');
let finishVersion;
const desktopEngine = { nativeDesktop: true, ready: Promise.resolve(),
  loadVersion: () => new Promise(resolve => { finishVersion = value => { desktopEngine.version = value; resolve(value); }; }) };
const desktop = stateFor(desktopEngine);
desktop.configure();
suite.check(desktop.aboutFields.get('#about-title').textContent.includes('Wails 桌面版'), '桌面标题不再声称是 WASM 阅读器');
suite.check(desktop.aboutFields.get('#about-description').textContent.includes('原生 Go 引擎'), '桌面说明展示原生解析和渲染');
suite.check(desktop.aboutFields.get('#about-fonts').textContent.includes('查找系统字体'), '桌面使用系统字体回退说明');
suite.check(desktop.aboutFields.get('#about-platform-feature').textContent.includes('原生文件对话框'), '桌面展示原生文件功能而非移动端特性');
suite.check(desktop.aboutFields.get('#about-technology').textContent === 'Go + Wails 3 + 系统 WebView + JavaScript', '桌面技术栈不包含 WebAssembly');
desktop.open();
suite.check(desktop.shown && desktop.versionLink.textContent === '加载中…', '桌面版本未读到时仍可立即打开对话框');
await Promise.resolve();
finishVersion('7.8.9');
await new Promise(resolve => setImmediate(resolve));
suite.check(desktop.versionLink.textContent === 'v7.8.9', '桌面从原生端读取发行版本');
desktop.close();
suite.check(!desktop.shown, '两端共用关闭逻辑');
desktop.open();
suite.check(desktop.versionLink.textContent === 'v7.8.9', '重复打开使用已取得版本');

suite.group('版本失败与旧 WebView 回退');
let attempts = 0;
const retryEngine = { nativeDesktop: true, loadVersion: async () => {
  if (!attempts++) throw new Error('读取失败');
  retryEngine.version = '3.2.1';
} };
const retry = stateFor(retryEngine);
retry.open();
await new Promise(resolve => setImmediate(resolve));
suite.check(retry.versionLink.textContent === '版本读取失败' && retry.shown, '读取失败有明确提示，不阻止关闭对话框');
retry.open();
await new Promise(resolve => setImmediate(resolve));
suite.check(retry.versionLink.textContent === 'v3.2.1', '再次打开可重试失败的版本读取');
const noModal = stateFor({ version: '1.0.0' });
delete noModal.aboutDialog.showModal;
delete noModal.aboutDialog.close;
noModal.open();
suite.check(noModal.dialogAttribute === 'open', '不支持 showModal 时仍可展示');
noModal.close();
suite.check(noModal.dialogAttribute === undefined, '不支持 close 时可移除 open 属性');

suite.group('项目链接');
let prevented = false, projects = 0;
const click = { preventDefault() { prevented = true; } };
await immediate.project(click);
suite.check(!prevented, '浏览器保持原有新标签页链接行为');
desktopEngine.openProjectWebsite = async () => { projects++; };
await desktop.project(click);
suite.check(prevented && projects === 1, '桌面阻止 WebView 导航，交给原生适配器打开主页');
desktopEngine.openProjectWebsite = async () => { throw new Error('系统浏览器不可用'); };
await desktop.project(click);
suite.check(desktop.status === '项目主页打开失败：系统浏览器不可用', '系统浏览器失败时显示诊断');

process.exitCode = suite.report() ? 1 : 0;
