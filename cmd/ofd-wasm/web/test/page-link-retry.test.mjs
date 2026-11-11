import assert from 'node:assert/strict';
import { load, sliceFunction } from './harness.mjs';

let calls = 0, resolve;
const state = {
  pageLinks: new Map(), pageLinkRequests: new Map(), layerUpdates: new Set(), documentGeneration: 1, revision: 0,
  pageLayerRevision: () => state.revision,
  engine: { pageLinksForPage: () => {
    calls++;
    return calls === 1 ? Promise.reject(new Error('暂时无法读取')) : new Promise(done => { resolve = done; });
  } },
  applyPageLinks: () => {},
};
load(state, sliceFunction('loadPageLinks'), { expose: { read: 'loadPageLinks' } });
state.read(0);
await new Promise(done => setImmediate(done));
assert.equal(state.pageLinks.has(0), false, '失败不能缓存为空链接列表');
state.read(0);
assert.equal(calls, 2, '后续可以重新读取');
resolve([{ boundary: { x: 1, y: 2, width: 3, height: 4 } }]);
await new Promise(done => setImmediate(done));
assert.equal(state.pageLinks.get(0).length, 1);
state.pageLinks.delete(0);
let oldResolve, oldCancelled = 0, nextResolve;
state.engine.pageLinksForPage = () => {
  const promise = new Promise(done => { if (!oldResolve) oldResolve = done; else nextResolve = done; });
  promise.cancel = () => { oldCancelled++; };
  return promise;
};
state.read(0);
state.pageLinkRequests.get(0).cancel();
state.pageLinkRequests.delete(0);
state.pageLinks.delete(0);
state.read(0);
oldResolve([{ boundary: { x: 0, y: 0, width: 1, height: 1 } }]);
await new Promise(done => setImmediate(done));
assert.equal(oldCancelled, 1);
assert.equal(state.pageLinks.get(0), null, '旧响应不能覆盖新请求');
nextResolve([]);
await new Promise(done => setImmediate(done));
assert.deepEqual(state.pageLinks.get(0), []);
assert.equal(state.pageLinkRequests.size, 0);
state.engine.nativeDesktop = true;
state.engine.media = () => { throw new Error('桌面链接不应预取媒体'); };
state.engine.pageEntryActionsForPage = () => { throw new Error('桌面不支持页面进入动作'); };
state.linkRequest = undefined;
state.pageEntryActions = [{ page: 0 }];
state.mediaCatalog = [{}];
state.cancelRequests = requests => { for (const request of requests.values()) request.cancel?.(); requests.clear(); };
load(state, ['fetchPageLinks', 'loadPageEntryActions'].map(sliceFunction).join('\n'),
  { expose: { resetLinks: 'fetchPageLinks', entry: 'loadPageEntryActions' } });
state.resetLinks(state.documentGeneration);
state.entry(0);
assert.deepEqual(state.pageEntryActions, []);
assert.deepEqual(state.mediaCatalog, []);
console.log('页面链接失败后可重试测试通过');
