import assert from 'node:assert/strict';
import { load, sliceFunction } from './harness.mjs';

let resolve, reject;
const pending = () => new Promise((yes, no) => { resolve = yes; reject = no; });
const state = {
  engine: { nativeDesktop: true }, documentGeneration: 1, sealPopoverRequest: 0, sealPopoverElement: { hidden: true }, opened: [], statuses: [],
  loadDesktopSignatureSummaries: pending,
  indexSealStamps: infos => { state.indexed = infos; }, applySealHotspots: () => { state.applied = true; },
  openSealPopover: (info, button) => state.opened.push({ info, button }), setStatus: value => state.statuses.push(value),
  document: { createElement: () => ({ style: {}, isConnected: true, setAttribute() {}, addEventListener(name, fn) { this[name] = fn; } }) },
};
load(state, ['loadDesktopSealHotspots', 'openDesktopSealPopover', 'buildSealHotspot', 'closeSealPopover'].map(sliceFunction).join('\n'),
  { expose: { preload: 'loadDesktopSealHotspots', make: 'buildSealHotspot', close: 'closeSealPopover' } });
const info = { scope: 0, id: 's1', stamps: [{ page: 0, visible_boundary: { x: 1, y: 2, width: 3, height: 4 } }] };
const preload = state.preload(1);
resolve([info]);
await preload;
assert.deepEqual(state.indexed, [info]);
assert.equal(state.applied, true);
const button = state.make({ info, stamp: info.stamps[0], stampIndex: 0 }, { width: 10, height: 20 });
assert.equal(button.style.left, '10%');
button.click();
assert.equal(button.disabled, true);
resolve([{ ...info, verified: true }]);
await new Promise(done => setImmediate(done));
assert.equal(state.opened[0].info.verified, true, '点击展示重新读取的详情');
assert.equal(button.disabled, false);
button.click();
reject(new Error('权限已过期'));
await new Promise(done => setImmediate(done));
assert.match(state.statuses[0], /权限已过期/);
assert.equal(state.opened.length, 1, '权限拒绝时不能打开旧详情');
button.click();
state.close();
resolve([info]);
await new Promise(done => setImmediate(done));
assert.equal(state.opened.length, 1, '关闭或点击空白后未完成请求不能打开弹层');
const another = state.make({ info: { ...info, id: 's2' }, stamp: info.stamps[0] }, { width: 10, height: 20 });
const requests = [];
state.loadDesktopSignatureSummaries = () => new Promise(yes => requests.push(yes));
button.click();
another.click();
requests[1]([{ ...info, id: 's2' }]);
await new Promise(done => setImmediate(done));
requests[0]([info]);
await new Promise(done => setImmediate(done));
assert.equal(state.opened.length, 2);
assert.equal(state.opened[1].info.id, 's2', '较早点击的迟到结果不能覆盖最新弹层');
state.loadDesktopSignatureSummaries = pending;
button.click();
state.documentGeneration++;
resolve([info]);
await new Promise(done => setImmediate(done));
assert.equal(state.opened.length, 2, '切换文档后不能打开旧签章');
const old = state.preload(1);
resolve([info]);
state.applied = false;
await old;
assert.equal(state.applied, false, '旧文档的清单不能创建热区');
console.log('桌面签章热区、权限与文档切换测试通过');
