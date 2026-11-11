import assert from 'node:assert/strict';
import { load, sliceFunction } from './harness.mjs';

let finish, fail;
const calls = [], statuses = [];
const title = {}, hint = {};
const state = {
  engine: { nativeDesktop: true, exportPages(indices, options) {
    calls.push({ indices, options });
    const request = new Promise((resolve, reject) => { finish = resolve; fail = reject; });
    request.cancel = () => { const error = new Error('cancel'); error.name = 'AbortError'; fail(error); };
    return request;
  } },
  exportRange: { value: 'current', options: [{ value: 'current' }, { value: 'all' }, { value: 'custom' }] },
  exportFormat: { value: 'png', options: [{ value: 'png' }, { value: 'jpg' }, { value: 'pdf' }, { value: 'txt' }],
    append(option) { state.exportFormat.options.push(option); } },
  exportDPI: { value: '150' }, exportError: {},
  exportDialog: { querySelector: selector => selector === '#export-title' ? title : hint },
  document: { createElement: () => ({}) },
  pageInfos: [{}, {}], current: 0, documentGeneration: 1, documentActionBusy: false, layerUpdates: new Set(),
  exportCustomRange: { value: '' },
  desktopAttachmentSaveRequests: new Set(), exportActive: false, exportRequest: undefined,
  closeExportDialog() {}, setStatus: text => statuses.push(text),
  exportBackgroundColor: () => '#ffffff', isCancelledError: error => error.name === 'AbortError',
  updateExportRangeControl() {},
};
state.setDocumentActionBusy = value => { state.documentActionBusy = value; };
load(state, ['configureDesktopPageExport', 'startDesktopPageExport', 'updateDesktopExportRange', 'exportIndexes', 'parsePrintRange'].map(sliceFunction).join('\n'),
  { expose: { configure: 'configureDesktopPageExport', start: 'startDesktopPageExport', range: 'updateDesktopExportRange' } });
state.configure(); state.configure();
assert.equal(title.textContent, '导出页面');
assert.equal(state.exportRange.options[1].disabled, false);
assert.equal(state.exportFormat.options.filter(option => option.value === 'svg').length, 1);
assert.equal(state.exportFormat.options.find(option => option.value === 'pdf').hidden, false);
state.exportRange.value = 'current';
const first = state.start();
await state.start();
assert.equal(calls.length, 1, '禁止重复导出');
assert.equal(calls[0].options.background, '#ffffffff');
finish(true); await first;
assert.equal(statuses.at(-1), '已导出 1 页');
assert.equal(state.documentActionBusy, false);
for (const format of ['pdf', 'txt', 'svg', 'png', 'jpg']) {
  state.exportFormat.value = format;
  state.range();
  assert.equal(state.exportRange.options[1].disabled, false);
  state.exportRange.value = 'all';
  const raster = ['png', 'jpg'].includes(format);
  state.exportDPI.value = raster ? '96' : 'not-used';
  const exporting = state.start();
  assert.equal(calls.at(-1).options.format, format);
  assert.equal(calls.at(-1).indices.length, 2);
  assert.equal(calls.at(-1).options.dpi, raster ? 96 : 150, '位图格式保留 DPI，非位图格式忽略不可编辑的 DPI');
  finish(true); await exporting;
}
state.exportFormat.value = 'pdf'; state.exportRange.value = 'custom'; state.exportCustomRange.value = '2,1';
const custom = state.start();
assert.deepEqual(Array.from(calls.at(-1).indices), [1, 0], '自定义页序按输入顺序传给原生端');
finish(true); await custom;
state.exportCustomRange.value = '3';
const callCount = calls.length;
await state.start();
assert.equal(calls.length, callCount, '非法范围不能打开原生对话框');
state.exportFormat.value = 'png'; state.exportDPI.value = '150'; state.range();
assert.equal(state.exportRange.value, 'custom', '切回图片格式仍保留所选范围');
state.exportCustomRange.value = '2,1-2';
const images = state.start();
assert.deepEqual(Array.from(calls.at(-1).indices), [1, 0], '图片导出同样保留页序并去重');
finish(true); await images;
state.exportRange.value = 'all';
state.pageInfos = Array.from({ length: 201 }, () => ({}));
const beforeLimit = calls.length;
await state.start();
assert.equal(calls.length, beforeLimit, '图片超过 200 页不调用原生端');
state.pageInfos = [{}, {}]; state.exportRange.value = 'current';
const cancel = state.start();
state.exportRequest.cancel(); await cancel;
assert.match(statuses.at(-1), /取消/);
assert.equal(state.documentActionBusy, false);
const failed = state.start();
fail(new Error('页面导出超过限制')); await failed;
assert.match(statuses.at(-1), /导出失败.*超过限制/);
assert.equal(state.documentActionBusy, false, '失败后允许重试');
const stale = state.start();
state.documentGeneration++;
const before = statuses.length;
state.documentActionBusy = false;
finish(true); await stale;
assert.equal(statuses.length, before, '旧文档结果不写入新文档状态');
console.log('桌面多页导出范围、格式切换、取消与文档切换测试通过');
