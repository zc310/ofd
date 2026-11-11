import assert from 'node:assert/strict';
import { load, sliceFunction } from './harness.mjs';

let resolveReport, rejectReport, cancellations = 0, calls = 0;
const state = {
  documentGeneration: 1, fontUsageRevision: 0, documentFontUsage: null, documentFontUsageGeneration: -1,
  documentFontUsageMeta: { scanned: 0, truncated: false }, fontUsageRequests: new Set(), fontUsageBatch: null,
  fontUsageBatchComplete: false, layerUpdates: new Set(), pageInfos: [{}],
  engine: { fontUsageAll: () => {
    calls++;
    const request = new Promise((resolve, reject) => { resolveReport = resolve; rejectReport = reject; });
    request.cancel = () => { cancellations++; };
    return request;
  } },
  fontUsageLimits: () => ({}), fontUsageKey: font => String(font.id),
};
load(state, ['invalidateFontUsage', 'trackFontUsage', 'checkCachedFontUsageAccess', 'loadFontUsageAll'].map(sliceFunction).join('\n'),
  { expose: { invalidate: 'invalidateFontUsage', loadUsage: 'loadFontUsageAll' } });
const previous = state.loadUsage();
state.layerUpdates.add(0);
state.invalidate();
assert.equal(cancellations, 1, '图层切换应取消旧字体扫描');
resolveReport({ fonts: [{ id: 1, pages: [0] }], scanned: 1, truncated: false });
assert.equal(await previous, null, '图层切换前的扫描结果不能进入缓存');
assert.equal(state.documentFontUsage, null);
assert.equal(await state.loadUsage(), null, '切换期间不应开始新的字体扫描');
state.layerUpdates.clear();
state.invalidate();
const current = state.loadUsage();
const concurrent = state.loadUsage();
assert.equal(calls, 2, '并发请求只启动一次新的批量扫描');
resolveReport({ fonts: [{ id: 1, pages: [] }], scanned: 1, truncated: false });
assert.deepEqual((await current).get('1').pages, []);
assert.equal(await current, await concurrent, '并发批量统计应复用同一结果');
state.invalidate();
state.documentFontUsage = new Map([['1', { pages: [0], scanned: 100, truncated: false }]]);
state.documentFontUsageGeneration = 1;
const batch = state.loadUsage();
assert.equal(typeof resolveReport, 'function', '单字体缓存不能替代批量统计');
resolveReport({ fonts: [{ id: 1, pages: [] }, { id: 2, pages: [0] }], scanned: 1, truncated: true });
const combined = await batch;
assert.deepEqual(combined.get('1').pages, [0], '较完整的单字体结果不能被较短扫描覆盖');
assert.deepEqual(combined.get('2').pages, [0]);
state.invalidate();
const failed = state.loadUsage();
rejectReport(new Error('读取失败'));
await assert.rejects(failed, /读取失败/);
const retry = state.loadUsage();
resolveReport({ fonts: [], scanned: 1, truncated: false });
assert.equal((await retry).size, 0, '失败后必须允许重试');
state.invalidate();
const old = state.loadUsage();
state.documentGeneration++;
resolveReport({ fonts: [{ id: 7, pages: [0] }], scanned: 1 });
assert.equal(await old, null, '旧文档的批量结果不能写入新文档');
console.log('图层切换后字体使用缓存失效测试通过');
