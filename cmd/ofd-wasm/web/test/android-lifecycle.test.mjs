import assert from 'node:assert/strict';
import { sliceFunction, load } from './harness.mjs';

const state = {
  Event,
  dialogs: [],
  document: { querySelectorAll: () => state.dialogs, body: { classList: { contains: () => false } } },
  sealPopoverElement: { hidden: true }, infoPanel: { hidden: true }, activeSidebarTab: 'thumbnails',
  viewPanel: { hidden: true }, zoomMenu: { hidden: true }, documentMenu: { hidden: true },
  sidebarMoreMenu: { hidden: true }, recentPanel: { hidden: true },
  sidebarShown: () => state.sidebar,
  toggleSidebar: () => { state.sidebar = false; },
  handleDocumentKeyDown: event => { assert.equal(event.key, 'Escape'); state.escapes++; },
  setRecentPanelOpen: value => { state.recentPanel.hidden = !value; },
  stopActiveMedia: () => state.cleaned.push('playback'),
  closeDesktopMediaPreview: () => state.cleaned.push('image'),
  closeDesktopAttachmentPreview: () => state.cleaned.push('text'),
  escapes: 0, cleaned: [], sidebar: false,
};
const api = load(state, ['handleAndroidBack', 'suspendAndroidMedia', 'openPrintDialog', 'configureAboutDialog']
  .map(sliceFunction).join('\n'), { expose: { back: 'handleAndroidBack', suspend: 'suspendAndroidMedia',
    print: 'openPrintDialog', about: 'configureAboutDialog' } });
assert.equal(api.back(), false, '空界面允许宿主退到后台');
state.sidebar = true;
assert.equal(api.back(), true);
assert.equal(state.sidebar, false, '返回键收起侧栏');
state.infoPanel.hidden = false;
assert.equal(api.back(), true);
assert.equal(state.escapes, 1);
let closed = 0, cancelled = 0;
state.dialogs = [{ dispatchEvent() { throw new Error('不应先关闭底层弹窗'); } },
  { dispatchEvent(event) { assert.equal(event.type, 'cancel'); cancelled++; return false; }, close() { closed++; } }];
assert.equal(api.back(), true);
assert.equal(cancelled, 1);
assert.equal(closed, 0, '已处理 cancel 的预览自行清理，不重复关闭');
state.dialogs[1].dispatchEvent = () => true;
api.back();
assert.equal(closed, 1, '未处理 cancel 的原生 dialog 正常关闭');
api.suspend();
assert.deepEqual(state.cleaned, ['playback', 'image', 'text'], '进入后台取消媒体和预览，防止迟到结果恢复播放');
state.engine = { nativeAndroid: true, nativeDesktop: true };
state.setStatus = value => { state.status = value; };
api.print();
assert.match(state.status, /暂不支持原生打印/);
state.fields = new Map();
state.aboutDialog = { querySelector: key => {
  if (!state.fields.has(key)) state.fields.set(key, {});
  return state.fields.get(key);
} };
api.about();
assert.match(state.fields.get('#about-title').textContent, /Android/);
assert.match(state.fields.get('#about-description').textContent, /原生 Go/);
console.log('Android 返回键优先级、弹窗取消、后台媒体清理、打印限制和关于说明测试通过');
