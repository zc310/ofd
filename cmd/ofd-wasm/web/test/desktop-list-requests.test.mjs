import assert from 'node:assert/strict';
import { load, sliceFunction } from './harness.mjs';

const pending = [];
let cancelled = 0;
function request(kind) {
  const promise = new Promise((resolve, reject) => pending.push({ kind, resolve, reject }));
  promise.cancel = () => { cancelled++; };
  return promise;
}
const state = {
  engine: { attachments: () => request('attachments'), media: () => request('media') },
  documentGeneration: 1, desktopListRequests: new Map(),
};
load(state, ['loadDesktopList', 'cancelDesktopListRequests'].map(sliceFunction).join('\n'),
  { expose: { read: 'loadDesktopList', cancel: 'cancelDesktopListRequests' } });
const first = state.read('attachments');
assert.equal(state.read('attachments'), first, '筛选复用在途请求');
const media = state.read('media');
assert.equal(pending.length, 2, '不同清单分别读取');
pending[0].resolve([{ name: '附件' }]);
pending[1].resolve([]);
assert.deepEqual(await first, [{ name: '附件' }]);
await media;
const denied = state.read('attachments');
assert.equal(pending.length, 3, '完成后重新读取以检查权限');
pending[2].reject(new Error('访问被拒绝'));
await assert.rejects(denied, /访问被拒绝/);
const stale = state.read('attachments');
state.documentGeneration++;
state.cancel();
assert.equal(cancelled, 1);
const current = state.read('attachments');
pending[3].resolve([{ name: '旧附件' }]);
await assert.rejects(stale, /文档已切换/);
assert.equal(state.read('attachments'), current, '旧请求结束不能清除新请求');
pending[4].resolve([]);
await current;
const invalid = state.read('media');
pending[5].resolve(null);
await assert.rejects(invalid, /格式无效/);
console.log('桌面附件和资源清单请求生命周期测试通过');
