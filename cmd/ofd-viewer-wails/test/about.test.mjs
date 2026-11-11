import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';

let calls = 0, handler;
const links = [];
const context = vm.createContext({
  wails: { Events: { On() {} }, Call: { ByName(method, ...args) {
    assert.equal(method, 'main.Desktop.Version');
    assert.deepEqual(args, [], '读取应用版本不需要文档地址');
    calls++;
    return handler();
  } }, Browser: { OpenURL: async url => { links.push(url); } } },
  window: {}, document: { createElement: () => ({}), body: { append() {} } },
});
const source = readFileSync(new URL('../desktop.js', import.meta.url), 'utf8')
  .replace("import { NativePageClient, mapPermissions } from './prototype/native-page.js';", '')
  .replace("await import('/wails/runtime.js');", '');
vm.runInContext(source, context);
const engine = context.window.ofdReaderEngineFactory();
await engine.ready;
assert.equal(calls, 0, '版本元数据仅在关于窗口需要时读取，不阻塞阅读器启动');
let finish;
handler = () => new Promise(resolve => { finish = resolve; });
const first = engine.loadVersion();
assert.equal(engine.loadVersion(), first, '重复打开合并正在进行的版本读取');
assert.equal(calls, 1);
finish('2.3.4');
assert.equal(await first, '2.3.4');
assert.equal(engine.version, '2.3.4');
assert.equal(await engine.loadVersion(), '2.3.4');
assert.equal(calls, 1, '成功后缓存应用版本，不随文档关闭而变化');
await engine.close();
assert.equal(await engine.loadVersion(), '2.3.4');

const retry = context.window.ofdReaderEngineFactory();
handler = async () => { throw new Error('版本读取失败'); };
await assert.rejects(retry.loadVersion(), /版本读取失败/);
assert.equal(retry.versionRequest, undefined, '失败释放在途请求，允许重试');
handler = async () => '';
await assert.rejects(retry.loadVersion(), /发行版本为空/);
handler = async () => '5.6.7';
assert.equal(await retry.loadVersion(), '5.6.7');
await engine.openProjectWebsite('file:///不可信路径');
assert.deepEqual(links, ['https://github.com/zc310/ofd'], '仅打开应用内置项目主页，不接受传入地址');
console.log('桌面关于版本、失败重试与项目链接测试通过');
