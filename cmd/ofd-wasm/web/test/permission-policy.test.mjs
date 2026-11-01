// 驱动真实Worker消息链，权限原因不能只停留在WASM返回对象中。
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';

const source = readFileSync(new URL('../worker.js', import.meta.url), 'utf8');
const replies = [];
const self = {
  ofd: { checkPermission: () => ({ error: '文档已过期', code: 'permission_denied', action: 'access', scope: 2, reason: 'expired' }) },
  addEventListener() {},
  postMessage: message => replies.push(message),
};
vm.runInNewContext(source, {
  self, console, setTimeout, clearTimeout, ArrayBuffer, Uint8Array, Error,
  importScripts() {},
  Go: class { importObject = {}; run() { return Promise.resolve(); } },
  fetch: async () => ({ ok: true, clone() { return this; } }),
  WebAssembly: { instantiateStreaming: async () => ({ instance: {} }) },
});
self.onmessage({ data: { id: 1, command: 'checkPermission', action: 'access', indices: [0] } });
for (let attempt = 0; attempt < 200 && !replies.some(reply => reply.id === 1); attempt++) {
  await new Promise(resolve => setTimeout(resolve, 1));
}
const denied = replies.find(reply => reply.id === 1);
assert.ok(denied && !denied.ok);
assert.equal(denied.code, 'permission_denied');
assert.equal(denied.reason, 'expired');
assert.equal(denied.action, 'access');
assert.equal(denied.scope, 2);
