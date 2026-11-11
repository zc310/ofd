import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';

const native = readFileSync(new URL('../prototype/native-page.js', import.meta.url), 'utf8');
const { NativePageClient, mapPermissions } = await import(`data:text/javascript;base64,${Buffer.from(native).toString('base64')}`);
const prefix = `/reader/${'a'.repeat(64)}/pages/`;
let selected = null;
const calls = [];
const events = new Map();
globalThis.wails = { Events: { On: (name, callback) => events.set(name, callback) }, Call: { ByName: async (method, ...args) => {
  calls.push([method, ...args]);
  if (['Open', 'OpenRecent', 'OpenDropped', 'OpenLaunch'].some(name => method.endsWith(`.${name}`))) return selected;
  if (method.endsWith('.PendingLaunch')) return true;
  if (method.endsWith('.RecentDocuments')) return [{ id: 'recent1', name: '测试.ofd', last_opened: 42 }];
  if (method.endsWith('.ClearRecentDocuments')) return null;
  if (method.endsWith('.Close')) return null;
  if (method.endsWith('.CustomTags')) return [];
  throw new Error(method);
} } };
const appended = [];
const dispatched = [];
const context = vm.createContext({ NativePageClient, mapPermissions, wails: globalThis.wails,
  CustomEvent: class { constructor(type, options) { this.type = type; this.detail = options.detail; } },
  window: { dispatchEvent: event => dispatched.push(event) },
  document: { createElement: () => ({}), body: { append: script => appended.push(script.src) } } });
const source = readFileSync(new URL('../desktop.js', import.meta.url), 'utf8')
  .replace("import { NativePageClient, mapPermissions } from './prototype/native-page.js';", '')
  .replace("await import('/wails/runtime.js');", '');
vm.runInContext(source, context);
assert.deepEqual(appended, ['viewer.js']);
const engine = context.window.ofdReaderEngineFactory();
assert.equal(engine.nativeAndroid, false, '默认桌面宿主不启用 Android 分支');
context.ofdReaderAndroid = true;
assert.equal(context.window.ofdReaderEngineFactory().nativeAndroid, true, 'Android 能力由原生注入，不按 User-Agent 猜测');
context.ofdReaderAndroid = false;
assert.equal(await engine.selectDocument(), null, '取消选择不改变文档');
selected = { name: '测试.ofd', size: 123, last_modified: 456, page_prefix: prefix,
  pages: [{ index: 0, width: 210, height: 297 }],
  permissions: [{ scope: 0, pages: [0], accessible: true, export: true }] };
const result = await engine.selectDocument();
assert.equal(result.name, '测试.ofd');
assert.equal(result.pages[0].width, 210);
assert.equal(result.size, 123);
assert.equal(result.lastModified, 456);
assert.equal(result.permissions[0].export, true);
assert.equal(calls.some(([name]) => name.endsWith('.Permissions')), false, '打开使用原子快照，不额外请求权限');
await engine.selectDocument({ recentID: 'recent1' });
assert.deepEqual(calls.at(-1), ['main.Desktop.OpenRecent', 'recent1']);
await engine.selectDocument({ dropToken: 'drop1' });
assert.deepEqual(calls.at(-1), ['main.Desktop.OpenDropped', 'drop1']);
assert.equal(await engine.nextLaunch(), true);
await engine.selectDocument({ launch: true });
assert.deepEqual(calls.at(-1), ['main.Desktop.OpenLaunch'], '系统打开入口不向原生端传入网页路径');
assert.equal((await engine.recentDocuments())[0].id, 'recent1');
assert.equal((await engine.recentDocuments())[0].lastOpened, 42);
await engine.clearRecentDocuments();
assert.equal(calls.at(-1)[0], 'main.Desktop.ClearRecentDocuments');
events.get('ofd-file-drop')({ data: { token: 'drop1' } });
assert.equal(dispatched[0].type, 'ofd-native-drop');
assert.equal(dispatched[0].detail.token, 'drop1');
const originalFetch = globalThis.fetch;
try {
  const urls = [];
  globalThis.fetch = async function (url) {
    assert.equal(this, globalThis, 'WebKit 的 fetch 必须绑定 Window');
    urls.push(url);
    return { ok: true, arrayBuffer: async () => new Uint8Array([1, 2]).buffer };
  };
  // 新选择文档时客户端捕获的是 Window.fetch；覆盖 fetch 后重新创建以模拟 WebView。
  await engine.selectDocument();
  assert.equal((await engine.renderPage(0, { dpi: 96, format: 'png', background: '#00000000' })).byteLength, 2);
  const thumbnails = await engine.renderPages([0, 1], { dpi: 30, format: 'png', background: '#00000000' });
  assert.equal(thumbnails.length, 2);
  assert.match(urls[0], /\/pages\/0\?dpi=96/);
  assert.match(urls[2], /\/pages\/1\?dpi=30/);
} finally {
  globalThis.fetch = originalFetch;
}
// 缩略图共享一个后台槽位，正文不必等待其队列；取消后未启动的请求不得继续执行。
const pending = [];
const controlled = context.window.ofdReaderEngineFactory();
controlled.client = { renderPage(index) {
  let resolve, reject;
  const request = new Promise((res, rej) => { resolve = res; reject = rej; });
  request.cancel = () => { const error = new Error('取消'); error.name = 'AbortError'; reject(error); };
  pending.push({ index, resolve });
  return request;
} };
const batch = controlled.renderPages([1, 2, 3], {});
await Promise.resolve();
assert.deepEqual(pending.map(item => item.index), [1], '只启动一个缩略图');
const foreground = controlled.renderPage(0, {});
assert.deepEqual(pending.map(item => item.index), [1, 0], '正文不排在缩略图队列后');
pending[1].resolve('正文');
assert.equal(await foreground, '正文');
batch.cancel();
await assert.rejects(batch, error => error.name === 'AbortError');
await new Promise(resolve => setImmediate(resolve));
assert.deepEqual(pending.map(item => item.index), [1, 0], '取消的缩略图不能在后台启动');
{
  const lifecycle = context.window.ofdReaderEngineFactory();
  const started = [];
  let cancellations = 0;
  lifecycle.client = { renderPage(index) {
    let reject;
    const request = new Promise((_, fail) => { reject = fail; });
    request.cancel = () => { cancellations++; reject(new Error('取消底层渲染')); };
    started.push(index);
    return request;
  }, call: async () => undefined };
  const old = lifecycle.renderPages([4, 5, 6], {});
  const oldResult = assert.rejects(old, error => error.name === 'AbortError');
  await Promise.resolve();
  selected = null;
  await lifecycle.selectDocument();
  assert.equal(cancellations, 0, '取消文件选择不能关闭当前文档');
  await lifecycle.close();
  await oldResult;
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(cancellations, 1, '关闭文档取消正在渲染的缩略图');
  assert.deepEqual(started, [4], '关闭文档后排队的缩略图不能启动');
  assert.equal(lifecycle.thumbnailQueue.length, 0);
}
{
  const lifecycle = context.window.ofdReaderEngineFactory();
  let cancelled = 0;
  lifecycle.client = { renderPage() {
    let reject;
    const request = new Promise((_, fail) => { reject = fail; });
    request.cancel = () => { cancelled++; reject(new Error('取消底层渲染')); };
    return request;
  } };
  const old = lifecycle.renderPages([0, 1], {});
  const oldResult = assert.rejects(old, error => error.name === 'AbortError');
  await Promise.resolve();
  selected = { name: '新文档.ofd', size: 1, last_modified: 0, page_prefix: prefix, pages: [], permissions: [] };
  await lifecycle.selectDocument();
  await oldResult;
  assert.equal(cancelled, 1, '成功切换文档取消旧缩略图');
  assert.equal(lifecycle.thumbnailQueue.length, 0);
}
selected = { name: '测试.ofd', size: 123, last_modified: 456, page_prefix: prefix, pages: [], permissions: [] };
{
  const lifecycle = context.window.ofdReaderEngineFactory();
  const original = globalThis.wails.Call.ByName;
  let finishOpen;
  const closeCalls = [];
  globalThis.wails.Call.ByName = (method, ...args) => {
    if (method.endsWith('.Open')) return new Promise(resolve => { finishOpen = resolve; });
    if (method.endsWith('.Close')) { closeCalls.push(args[0]); return Promise.resolve(); }
    throw new Error(method);
  };
  try {
    const pendingOpen = lifecycle.selectDocument();
    await assert.rejects(lifecycle.selectDocument(), /文件选择正在进行/);
    await lifecycle.close();
    finishOpen(selected);
    assert.equal(await pendingOpen, null, '关闭后迟到的打开结果不能复活文档');
    assert.deepEqual(closeCalls, [prefix], '迟到会话按自身地址释放');
    assert.equal(lifecycle.client, undefined);
    const retry = lifecycle.selectDocument();
    finishOpen(null);
    assert.equal(await retry, null, '取消后允许再次选择');
    const current = { marker: '当前文档' };
    lifecycle.client = current;
    globalThis.wails.Call.ByName = () => Promise.reject(new Error('打开失败'));
    await assert.rejects(lifecycle.selectDocument(), /打开失败/);
    assert.equal(lifecycle.client, current, '打开失败保留原有客户端');
    globalThis.wails.Call.ByName = () => Promise.resolve(null);
    assert.equal(await lifecycle.selectDocument(), null, '打开失败后允许重试');
  } finally {
    globalThis.wails.Call.ByName = original;
  }
}
assert.equal(await engine.selectDocument().then(() => true), true);
assert.deepEqual(await engine.customTags(), []);
await engine.close();
assert.deepEqual(calls.at(-1), ['main.Desktop.Close', prefix]);
console.log('桌面引擎与共享界面入口测试通过');
