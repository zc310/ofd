import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { sliceFunction, load } from './harness.mjs';

const code = ['applyDocumentTitle', 'setDocumentUIHidden', 'resetDocumentPreferences', 'applyDocumentPreferences'].map(sliceFunction).join('\n');
function setup() {
  const classes = new Set();
  const state = {
    documentGeneration: 1, currentDocumentFileName: '文件.ofd', applicationTitle: '阅读器',
    documentName: {}, restoreDocumentUI: { hidden: true },
    document: { title: '阅读器', body: { classList: { toggle: (key, enabled) => enabled ? classes.add(key) : classes.delete(key) } } },
    classes, frames: [], zoomMode: 'manual', pageLayout: 'single', zoom: 1, pageLayoutSelect: {},
    pageLayoutStorageKey: 'layout', zoomModeStorageKey: 'zoom',
    hasSavedPreference: () => false, mapDocumentPageLayout: () => 'single',
    normalizeDocumentZoom: () => 0, mapDocumentZoomMode: () => '',
    headerHeight() {}, scheduleVirtualUpdate() {}, refitZoom() {},
    setViewPanelOpen() {}, setZoomMenuOpen() {}, setInfoPanelOpen() {}, setRecentPanelOpen() {}, setDocumentMenuOpen() {},
    engine: { preferences: async () => ({}) },
  };
  state.requestAnimationFrame = callback => state.frames.push(callback);
  return load(state, code, { expose: { title: 'applyDocumentTitle', ui: 'setDocumentUIHidden', reset: 'resetDocumentPreferences', apply: 'applyDocumentPreferences' } });
}

{
  const state = setup();
  state.title({ tab_display: 'DocTitle', document_title: ' <标题> ' });
  assert.equal(state.documentName.textContent, '<标题>');
  assert.equal(state.document.title, '<标题> — 阅读器');
  for (const preferences of [{}, { tab_display: 'FileName', document_title: '忽略' }, { tab_display: 'DocTitle', document_title: '  ' }]) {
    state.title(preferences);
    assert.equal(state.documentName.textContent, '文件.ofd');
    assert.equal(state.document.title, '文件.ofd — 阅读器');
  }
}
for (const [flag, className] of [['hide_toolbar', 'document-hide-toolbar'], ['hide_menubar', 'document-hide-menubar'], ['hide_window_ui', 'document-hide-window-ui']]) {
  const state = setup();
  state.ui({ [flag]: true });
  assert.deepEqual([...state.classes], [className]);
  assert.equal(state.restoreDocumentUI.hidden, false);
  state.ui();
  assert.equal(state.classes.size, 0);
  assert.equal(state.restoreDocumentUI.hidden, true);
}
{
  const state = setup();
  state.ui({ hide_toolbar: true, hide_menubar: true, hide_window_ui: true });
  assert.equal(state.classes.size, 3);
  state.ui({ hide_toolbar: false, hide_menubar: false, hide_window_ui: false });
  assert.equal(state.classes.size, 0);
  state.engine.preferences = async () => { throw new Error('无法读取偏好'); };
  await state.apply();
  assert.equal(state.classes.size, 0);
  assert.equal(state.restoreDocumentUI.hidden, true);
}
{
  const state = setup();
  state.engine.preferences = async () => ({ tab_display: 'DocTitle', document_title: '旧标题', hide_toolbar: true, hide_window_ui: true });
  await state.apply();
  assert.equal(state.documentName.textContent, '旧标题');
  state.currentDocumentFileName = '新文件.ofd';
  state.reset();
  assert.equal(state.documentName.textContent, '新文件.ofd');
  assert.equal(state.classes.size, 0);
  state.currentDocumentFileName = '';
  state.reset();
  assert.equal(state.document.title, '阅读器');
  assert.equal(state.documentName.textContent, '未打开文档');
}
{
  const state = setup();
  let resolve;
  state.engine.preferences = () => new Promise(done => { resolve = done; });
  const pending = state.apply();
  state.documentGeneration++;
  resolve({ tab_display: 'DocTitle', document_title: '过期响应', hide_toolbar: true });
  await pending;
  assert.equal(state.document.title, '阅读器');
  assert.equal(state.classes.size, 0);
}
{
  const state = setup();
  state.hasSavedPreference = () => true;
  state.pageLayout = 'double'; state.zoomMode = 'manual'; state.zoom = 2;
  await state.apply();
  assert.equal(state.pageLayout, 'double');
  assert.equal(state.zoomMode, 'manual');
  assert.equal(state.zoom, 2);
}
// 恢复入口在 header 外，移动端和隐藏界面规则不能连同它一起隐藏。
const html = readFileSync(new URL('../index.html', import.meta.url), 'utf8');
assert.ok(html.indexOf('id="restore-document-ui"') > html.indexOf('</header>'));
assert.match(html, /#restore-document-ui\[hidden\]/);
