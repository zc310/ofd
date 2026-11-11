import assert from 'node:assert/strict';
import { load, sliceFunction } from './harness.mjs';

const pending = [], statuses = [], permissions = [];
let cancellations = 0;
const state = {
  pageInfos: [{}, {}], documentGeneration: 1, documentActionBusy: false, documentActionCancelRequested: false,
  layerUpdates: new Map(), exportRequest: undefined,
  engine: { nativeDesktop: true, printPages(indices) {
    const request = new Promise((resolve, reject) => pending.push({ indices, resolve, reject }));
    request.cancel = () => { cancellations++; pending.at(-1).reject(Object.assign(new Error('取消'), { name: 'AbortError' })); };
    return request;
  } },
  requirePagePermission(action, indices) { permissions.push([action, indices]); if (state.denied) throw new Error('禁止打印'); },
  setStatus: text => statuses.push(text),
  setDocumentActionBusy: value => { state.documentActionBusy = value; },
  isCancelledError: error => error.name === 'AbortError',
  textRequests: new Set(), pageRequests: new Set(), cancelRequests() {},
  window: { open() { throw new Error('桌面打印不打开浏览器弹窗'); } },
};
load(state, ['printSelectedPages', 'cancelDocumentAction'].map(sliceFunction).join('\n'),
  { expose: { print: 'printSelectedPages', cancel: 'cancelDocumentAction' } });
const first = state.print([1, 0]);
assert.equal(state.documentActionBusy, true);
assert.deepEqual(permissions[0], ['print', [1, 0]]);
pending[0].resolve(true); await first;
assert.match(statuses.at(-1), /打印预览/);
assert.equal(state.documentActionBusy, false);
state.denied = true; await state.print([0]);
assert.equal(pending.length, 1, '权限拒绝不能准备打印内容');
state.denied = false;
const cancelled = state.print([0]); state.cancel(); await cancelled;
assert.equal(cancellations, 1);
assert.match(statuses.at(-1), /已取消/);
const stale = state.print([0]); state.documentGeneration++;
const before = statuses.length; pending[2].resolve(true); await stale;
assert.equal(statuses.length, before, '旧文档结果不能覆盖状态');
console.log('桌面打印权限、系统预览入口、取消和文档切换测试通过');
