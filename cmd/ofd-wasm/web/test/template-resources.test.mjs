import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
import { sliceFunction, load } from './harness.mjs';

// 正文和不同模板的同 ID 资源不能共用字体统计或媒体缓存键。
const state = load({}, [sliceFunction('fontUsageKey'), sliceFunction('mediaActionKey')].join('\n'), {
  expose: { fontKey: 'fontUsageKey', mediaKey: 'mediaActionKey' },
});
for (const key of ['fontKey', 'mediaKey']) {
  const keys = [0, 10, 20].map(template_id => state[key]({ scope: 0, id: 9, media_id: 9, template_id }));
  assert.equal(new Set(keys).size, 3, `${key} 必须隔离模板`);
}

// 驱动真实 Worker，确认第五参数不丢失，也不把缺失用途自动提升为 preview。
const source = readFileSync(new URL('../worker.js', import.meta.url), 'utf8');
const calls = [];
const replies = [];
const self = {
  ofd: { mediaData: (...args) => { calls.push(args); return new Uint8Array([1]); } },
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
for (const [id, purpose] of [[1, 'preview'], [2, undefined]]) {
  self.onmessage({ data: { id, command: 'mediaData', scope: 0, mediaID: 7, maxBytes: 0, purpose, templateID: 10 } });
  for (let attempt = 0; attempt < 200 && !replies.some(reply => reply.id === id); attempt++) {
    await new Promise(resolve => setTimeout(resolve, 1));
  }
  assert.ok(replies.some(reply => reply.id === id && reply.ok));
  assert.deepEqual(calls[id - 1], [0, 7, 0, purpose, 10]);
}
