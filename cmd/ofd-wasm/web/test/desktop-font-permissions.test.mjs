import assert from 'node:assert/strict';
import { load, sliceFunction } from './harness.mjs';

let deny = false, checks = 0, pending;
const state = {
  engine: { nativeDesktop: true, checkPermission: (action, pages) => {
    checks++;
    assert.equal(action, 'access');
    assert.deepEqual(pages, [0, 1]);
    if (pending) return new Promise(resolve => { pending.resolve = resolve; });
    return deny ? Promise.reject(new Error('权限已过期')) : Promise.resolve();
  } },
  documentGeneration: 1, fontUsageRevision: 0, layerUpdates: new Set(), pageInfos: [{}, {}],
  documentFontUsage: new Map([['font', { pages: [1], scanned: 2 }]]), documentFontUsageGeneration: 1,
  documentFontUsageMeta: { scanned: 1, truncated: false }, fontUsageBatchComplete: true,
  fontUsageBatch: null, fontUsageRequests: new Set(), fontUsageKey: () => 'font',
  fontUsageLabel: () => '1 页', setStatus() {},
  document: { createElement: () => ({ append() {}, addEventListener() {} }) },
  outlineEmptyMessage: text => text,
};
load(state, ['trackFontUsage', 'checkCachedFontUsageAccess', 'loadFontUsageAll', 'toggleFontUsage'].map(sliceFunction).join('\n'),
  { expose: { batch: 'loadFontUsageAll', toggle: 'toggleFontUsage' } });
assert.equal(await state.batch(), state.documentFontUsage);
assert.equal(checks, 1, '批量结果也检查覆盖更广的单字体缓存');
deny = true;
await assert.rejects(state.batch(), /权限已过期/);
const button = { disabled: false, textContent: '', classList: { add() {}, remove() {} } };
const wrap = { children: [], querySelector: () => null, append(item) { this.children.push(item); } };
await state.toggle({}, wrap, button);
assert.equal(button.textContent, '查找失败');
assert.equal(wrap.children.length, 0, '权限过期不能展开缓存页码');
deny = false;
pending = {};
const old = state.batch();
state.documentGeneration++;
pending.resolve();
assert.equal(await old, null, '切换文档后不能使用旧缓存');
console.log('桌面字体使用缓存权限检查测试通过');
