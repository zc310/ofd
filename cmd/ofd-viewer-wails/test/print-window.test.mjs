import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

// 直接执行 Go 产出的打印窗口脚本，避免测试另写一套页面逻辑。
const source = readFileSync(new URL('../print.go', import.meta.url), 'utf8');
const script = source.match(/const printScript = `([\s\S]*?)`/)[1].replace(/^import[^;]+;\s*/, '');
const calls = [], listeners = {};
let finishImage, denied = false;
const imageReady = new Promise(resolve => { finishImage = resolve; });
const button = { disabled: true, click: () => button.onclick() }, status = {};
const close = {};
const document = { images: [{ decode: () => imageReady }],
  querySelector: selector => ({ '#print': button, '#status': status, '#close': close })[selector],
  addEventListener: (name, listener) => { listeners[name] = listener; } };
const wails = { Call: { ByName: async (method, token) => {
  calls.push([method, token]);
  if (denied) throw new Error('打印权限已过期');
} } };
const run = new Function('document', 'wails', 'location', `return (async () => {${script}\n})()`);
const ready = run(document, wails, { pathname: '/reader-print/opaque-token/index.html' });
assert.equal(button.disabled, true, '全部图片加载完成前不能发起打印');
finishImage(); await ready;
assert.equal(button.disabled, false);
assert.equal(calls.length, 0, '加载预览不会自动提交打印');
await button.click();
assert.deepEqual(calls[0], ['main.Desktop.PrintPrepared', 'opaque-token']);
denied = true;
await button.click();
assert.match(status.textContent, /权限已过期/);
assert.equal(button.disabled, false, '失败后可关闭预览或重试');
denied = false;
let prevented = false;
listeners.keydown({ ctrlKey: true, key: 'p', preventDefault() { prevented = true; } });
await new Promise(resolve => setImmediate(resolve));
assert.equal(prevented, true);
assert.equal(calls.at(-1)[0], 'main.Desktop.PrintPrepared', '预览窗口快捷键也必须通过 Go 权限复核');
await close.onclick();
assert.deepEqual(calls.at(-1), ['main.Desktop.ClosePrint', 'opaque-token']);
console.log('原生打印窗口图片就绪、授权入口、快捷键及关闭脚本测试通过');
