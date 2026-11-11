import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';

const native = readFileSync(new URL('../prototype/native-page.js', import.meta.url), 'utf8');
const { NativePageClient, mapPermissions } = await import(`data:text/javascript;base64,${Buffer.from(native).toString('base64')}`);
const prefix = `/reader/${'a'.repeat(64)}/pages/`, nextPrefix = `/reader/${'b'.repeat(64)}/pages/`;
const opened = { name: '加密.ofd', size: 100, last_modified: 0, page_prefix: prefix, pages: [{ width: 30, height: 20 }], permissions: [] };
const challenge = { challenge: 'opaque-unlock-token', name: '加密.ofd', code: 'credentials_required' };
const calls = [], prompts = [];
let handler;
globalThis.wails = { Call: { ByName: (method, ...args) => { calls.push([method, ...args]); return handler(method, ...args); } }, Events: { On() {} } };
const context = vm.createContext({ NativePageClient, mapPermissions, wails: globalThis.wails,
  window: {}, document: { createElement: () => ({}), body: { append() {} } } });
const source = readFileSync(new URL('../desktop.js', import.meta.url), 'utf8')
  .replace("import { NativePageClient, mapPermissions } from './prototype/native-page.js';", '')
  .replace("await import('/wails/runtime.js');", '');
vm.runInContext(source, context);
const engine = context.window.ofdReaderEngineFactory();
let attempts = 0;
handler = async (method, ...args) => {
  if (method.endsWith('.Open')) return challenge;
  if (method.endsWith('.CancelUnlock')) return undefined;
  assert.equal(method, 'main.Desktop.Unlock');
  assert.equal(args[0], challenge.challenge);
  assert.equal(args[1].user_name, 'User');
  assert.equal(args[1].password, attempts ? '' : 'wrong');
  return attempts++ ? opened : { ...challenge, code: 'decrypt_failed' };
};
const credentials = [];
const result = await engine.selectDocument({ unlock: async (name, error) => {
  prompts.push([name, error.code]);
  const value = { method: 'password', userName: 'User', password: attempts ? '' : 'wrong' };
  credentials.push(value); return value;
} });
assert.equal(result.pageCount, 1);
assert.deepEqual(prompts.map(item => item[1]), ['credentials_required', 'decrypt_failed']);
assert.ok(credentials.every(value => !Object.hasOwn(value, 'password')), '清除临时凭据引用');
assert.equal(calls.at(-1)[0], 'main.Desktop.CancelUnlock');
const oldClient = engine.client;
handler = async method => method.endsWith('.Open') ? challenge : undefined;
assert.equal(await engine.selectDocument({ unlock: async () => null }), null);
assert.equal(engine.client, oldClient, '取消解锁保留旧文档');
let finishUnlock;
handler = (method) => {
  if (method.endsWith('.Open')) return Promise.resolve(challenge);
  if (method.endsWith('.Unlock')) return new Promise(resolve => { finishUnlock = resolve; });
  return Promise.resolve();
};
const pendingOpen = engine.selectDocument({ unlock: async () => ({ method: 'password', password: 'secret' }) });
await new Promise(resolve => setImmediate(resolve));
await engine.close(); finishUnlock(opened);
assert.equal(await pendingOpen, null, '关闭后解锁完成不能复活文档');
assert.equal(engine.client, undefined);
assert.ok(calls.some(([method, prefixValue]) => method.endsWith('.Close') && prefixValue === prefix));

// 关闭发生在凭据输入期间：迟到凭据不能发给原生端，也不能留在临时对象中。
let finishPrompt;
const promptCredentials = { method: 'privateKey', password: 'secret', privateKeyPassword: 'key-secret' };
const beforeUnlock = calls.filter(([method]) => method.endsWith('.Unlock')).length;
handler = async method => method.endsWith('.Open') ? challenge : undefined;
const waitingPrompt = engine.selectDocument({ unlock: () => new Promise(resolve => { finishPrompt = resolve; }) });
await new Promise(resolve => setImmediate(resolve));
await engine.close(); finishPrompt(promptCredentials);
assert.equal(await waitingPrompt, null);
assert.equal(Object.hasOwn(promptCredentials, 'password'), false);
assert.equal(Object.hasOwn(promptCredentials, 'privateKeyPassword'), false);
assert.equal(calls.filter(([method]) => method.endsWith('.Unlock')).length, beforeUnlock);

handler = async method => method.endsWith('.Open') ? opened : undefined;
await engine.selectDocument();
handler = async (method, prefixValue, scope, id) => {
  assert.equal(method, 'main.Desktop.SelectVersion'); assert.equal(prefixValue, prefix);
  assert.equal(scope, 0); assert.equal(id, 'v1');
  return { page_prefix: nextPrefix, pages: [{ width: 60, height: 40 }], permissions: [{ export: false }] };
};
const switchRequest = engine.selectVersion(0, 'v1');
switchRequest.cancel();
const version = await switchRequest;
assert.equal(version.pages[0].width, 60);
assert.equal(engine.client.prefix, nextPrefix, '成功版本必须更换原生资源地址');
handler = async () => { throw new Error('版本禁止访问'); };
await assert.rejects(engine.selectVersion(0, 'denied'), /禁止访问/);
assert.equal(engine.client.prefix, nextPrefix, '失败不能覆盖当前客户端');
await assert.rejects(engine.client.selectVersion(-1, ''), /参数无效/);
await assert.rejects(engine.client.printPages([0, 0]), /不重复/);
handler = async (method, prefixValue, indices) => {
  assert.equal(method, 'main.Desktop.PrintPages'); assert.equal(prefixValue, nextPrefix); assert.deepEqual(indices, [1, 0]); return true;
};
assert.equal(await engine.printPages([1, 0]), true);
console.log('原生加密解锁重试、取消、关闭隔离、历史版本资源轮换及打印绑定测试通过');
