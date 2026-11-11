import assert from 'node:assert/strict';
import { load, sliceFunction } from './harness.mjs';

const observers = [], active = [], started = [];
const state = {
  thumbnailJobs: new Map(), thumbnailQueue: [], thumbnailActive: 0, thumbnailRevision: 0,
  mediaPreviewRequests: new Set(), mediaObjectURLs: [], mediaObserver: null,
  closeDesktopMediaPreview() {}, URL: { revokeObjectURL() {} },
  IntersectionObserver: class {
    constructor(callback, options) { this.callback = callback; this.options = options; this.observed = []; observers.push(this); }
    observe(element) { this.observed.push(element); }
    unobserve() {} disconnect() { this.disconnected = true; }
  },
};
load(state, ['observeThumbnails', 'pumpThumbnails', 'revokeMediaURLs'].map(sliceFunction).join('\n'),
  { expose: { observe: 'observeThumbnails', revoke: 'revokeMediaURLs' } });
const flush = () => new Promise(resolve => setImmediate(resolve));
const root = {};
const nodes = Array.from({ length: 5 }, () => ({ isConnected: true }));
const jobs = nodes.map((element, index) => [element, () => {
  started.push(index);
  return new Promise(resolve => active.push(resolve));
}]);
state.observe(root, jobs);
assert.equal(started.length, 0, '构建全部卡片不发起任何图片请求');
assert.equal(observers[0].options.root, root);
observers[0].callback([{ target: nodes[0], isIntersecting: false }]); await flush();
assert.equal(started.length, 0, '不可见不加载');
observers[0].callback(nodes.map(target => ({ target, isIntersecting: true })));
await flush();
assert.deepEqual(started, [0, 1], '资源和印章最多同时两个任务');
observers[0].callback([{ target: nodes[2], isIntersecting: false }]);
active[0](); await flush();
assert.deepEqual(started, [0, 1, 3], '排队期间滚出视区的缩略图不读取');
observers[0].callback([{ target: nodes[2], isIntersecting: true }]);
state.revoke();
assert.equal(observers[0].disconnected, true);
assert.equal(state.thumbnailQueue.length, 0);
observers[0].callback([{ target: nodes[4], isIntersecting: true }]);
active[1](); active[2](); await flush();
assert.deepEqual(started, [0, 1, 3], '切换后旧队列和迟到观察事件都不能启动');
state.IntersectionObserver = undefined;
nodes[3].isConnected = false;
state.observe(root, jobs.slice(3)); await flush();
assert.deepEqual(started, [0, 1, 3, 4], '无观察器时仍受并发和 DOM 生命周期保护');
active[3](); await flush();
assert.equal(state.thumbnailActive, 0);
console.log('资源与印章共用可见区域加载、有界并发及旧队列取消测试通过');
