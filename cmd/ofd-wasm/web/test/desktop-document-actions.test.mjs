import assert from 'node:assert/strict';
import { load, sliceFunction, viewerSource } from './harness.mjs';

const selected = [], shown = [], statuses = [];
let finish;
const state = {
  engine: { nativeDesktop: true, selectDocument: selection => {
    selected.push(selection);
    return new Promise(resolve => { finish = resolve; });
  } },
  nativeSelecting: false, opening: false,
  versionSwitching: false, documentActionBusy: false,
  requestUnlockCredentials() {},
  openSelectedFile: async (file, options) => shown.push({ file, options }),
  refreshRecentFiles: async () => {}, setStatus: text => statuses.push(text),
};
load(state, sliceFunction('openNativeDocument'), { expose: { open: 'openNativeDocument' } });
const opening = state.open({ recentID: 'r1' });
await state.open({ dropToken: 'drop1' });
assert.equal(selected.length, 1, '原生打开入口共享并发保护');
assert.match(statuses.at(-1), /等待/);
finish({ name: '文档.ofd', size: 12, lastModified: 34 });
await opening;
assert.equal(shown[0].file.name, '文档.ofd');
assert.equal(state.nativeSelecting, false);
const cancelled = state.open(); finish(null); await cancelled;
assert.equal(shown.length, 1, '取消选择保留当前文档');
state.engine.selectDocument = async () => { throw new Error('文件不存在'); };
await state.open({ recentID: 'missing' });
assert.match(statuses.at(-1), /文件不存在/);
assert.equal(state.nativeSelecting, false, '失败后允许重试');

let resolveSave, cancelledSaves = 0;
Object.assign(state, {
  documentGeneration: 1, desktopAttachmentSaveRequests: new Set(),
});
state.engine.saveAttachment = () => {
  const request = new Promise(resolve => { resolveSave = resolve; });
  request.cancel = () => { cancelledSaves++; };
  return request;
};
load(state, ['saveDesktopResource', 'saveDesktopFile', 'cancelDesktopAttachmentSaves'].map(sliceFunction).join('\n'),
  { expose: { save: 'saveDesktopResource', cancelSaves: 'cancelDesktopAttachmentSaves' } });
const first = state.save({ scope: 0, id: 'a' }); resolveSave(true); await first;
assert.equal(statuses.at(-1), '附件已保存');
const second = state.save({ scope: 0, id: 'a' }); resolveSave(false); await second;
assert.match(statuses.at(-1), /取消/);
const stale = state.save({ scope: 0, id: 'a' });
state.documentGeneration++;
state.cancelSaves();
const before = statuses.length;
resolveSave(true); await stale;
assert.equal(statuses.length, before, '旧文档保存结果不能污染新文档状态');
assert.equal(cancelledSaves, 1);
state.engine.saveMedia = (scope, id) => {
  assert.equal(scope, 0); assert.equal(id, 200);
  return state.engine.saveAttachment();
};
const media = state.save({ scope: 0, id: 200 }, 'media');
await state.save({ scope: 0, id: 'a' });
assert.match(statuses.at(-1), /当前保存操作/);
resolveSave(true); await media;
assert.equal(statuses.at(-1), '资源已保存');
assert.match(viewerSource(), /data-file-drop-target/);
console.log('桌面统一打开入口和附件保存生命周期测试通过');
