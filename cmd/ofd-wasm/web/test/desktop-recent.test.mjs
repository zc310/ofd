import assert from 'node:assert/strict';
import { load, sliceFunction } from './harness.mjs';

function node() {
  return { children: [], dataset: {}, append(...items) { this.children.push(...items); },
    replaceChildren() { this.children = []; }, addEventListener(type, callback) { this[type] = callback; } };
}
const opened = [], pending = [], statuses = [];
const state = {
  engine: { nativeDesktop: true, recentDocuments: () => new Promise(resolve => pending.push(resolve)), clearRecentDocuments: async () => {} },
  recentFiles: [], recentRefreshRequest: 0,
  recentList: node(), recentEmpty: {}, document: { createElement: node },
  formatFileSize: size => `${size}`, formatRecentDate: time => `${time}`,
  setStatus: text => statuses.push(text), setRecentPanelOpen() {}, openNativeDocument: async value => opened.push(value),
};
load(state, ['readRecentFiles', 'refreshRecentFiles', 'openRecentFile', 'clearRecentFiles'].map(sliceFunction).join('\n'),
  { expose: { refresh: 'refreshRecentFiles', open: 'openRecentFile', clear: 'clearRecentFiles' } });
const first = state.refresh();
const second = state.refresh();
pending[1]([{ id: 'r1', name: '最新.ofd', size: 12, lastOpened: 34 }]); await second;
pending[0]([{ id: 'old', name: '旧记录.ofd' }]); await first;
assert.equal(state.recentList.children[0].children[0].textContent, '最新.ofd', '迟到清单不能覆盖新结果');
await state.open('r1');
assert.equal(opened[0].recentID, 'r1');
await state.open('/arbitrary/path');
assert.equal(opened.length, 1, '不接受清单之外的标识');
const clearing = state.clear();
await Promise.resolve();
pending[2]([]); await clearing;
assert.equal(state.recentList.children.length, 0);
assert.equal(state.recentEmpty.hidden, false);
console.log('桌面最近文档共用列表、乱序和清除测试通过');
