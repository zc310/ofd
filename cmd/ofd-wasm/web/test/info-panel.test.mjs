import assert from 'node:assert/strict';
import { createSuite, load, sliceFunction, sliceFunctionUntil } from './harness.mjs';

const suite = createSuite('文档信息面板');
const tick = () => new Promise(resolve => setImmediate(resolve));
const element = () => ({ innerHTML: '', hidden: false, scrollTop: 0 });
const state = {
  documentGeneration: 1,
  infoRenderRequest: 0,
  infoRenderedGeneration: -1,
  current: 0,
  pageInfos: [{}],
  infoBasicContent: element(),
  infoStats: element(),
  infoTabPanels: { basic: element(), security: element(), fonts: element() },
  loadDocumentInfo: async () => ({
    title: '测试文档',
    fonts: [],
    fonts_error: '第 8 页路径数据解析失败',
  }),
  escapeHTML: value => String(value).replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;'),
  engine: { stats: async () => ({ fonts: 1, attachments: 0, media: 0, annotation_pages: 0, signatures: 0 }) },
};
load(state, ['resetDocumentInfo', 'documentSecurityHTML', 'updateDocumentInfo'].map(sliceFunction).join('\n'), { expose: { update: 'updateDocumentInfo', securityHTML: 'documentSecurityHTML', reset: 'resetDocumentInfo' } });
state.update();
await tick();
await tick();
assert.match(state.infoBasicContent.innerHTML, /测试文档/);
assert.match(state.infoTabPanels.fonts.innerHTML, /字体列表读取失败：第 8 页路径数据解析失败/);
assert.match(state.infoTabPanels.security.innerHTML, /权限信息暂不可用/);
assert.match(state.infoStats.innerHTML, /资源统计/);
assert.doesNotMatch(state.infoBasicContent.innerHTML, /字体列表读取失败|文件加密/);
suite.check(true, '基本信息、安全、字体独立显示，字体失败不影响其他页签');

const defaults = { scope: 0, pages: [0], declared: false, export: true, printable: true, print_copies: -1, edit: true, annot: true, signature: true, watermark: true, print_screen: true };
{
  const html = state.securityHTML({ security: { encrypted: false, state: 'none' }, permissions: [defaults] });
  assert.match(html, /未加密/);
  assert.match(html, /未声明，默认允许/);
  assert.match(html, /不限/);
  assert.match(html, /未在此阅读器中强制执行/);
  assert.doesNotMatch(html, /已解锁/);
  suite.check(true, '普通文档展示默认权限与执行范围');
}
{
  const html = state.securityHTML({ security: { encrypted: true, state: 'unlocked' }, permissions: [{ ...defaults, declared: true, export: false, printable: false, print_copies: 0 }] });
  assert.match(html, /已加密（GM\/T 0099-2020）/);
  assert.match(html, /已解锁/);
  for (const label of ['打印', '复制文字', '导出内容']) assert.ok(html.includes(`${label}</span><span class="info-value">禁止`));
  assert.doesNotMatch(html, /打印份数<\/span>/);
  suite.check(true, '加密受限文档显示已解锁与三项禁止状态');
}
{
  const html = state.securityHTML({ security: { encrypted: false }, permissions: [
    { ...defaults, declared: true, pages: [0, 1], export: false, print_copies: 2, start_date: '<img src=x onerror=alert(1)>', end_date: '2027-01-01' },
    { ...defaults, scope: 1, pages: [2] },
  ] });
  assert.match(html, /文档体 1 · 第 1–2 页/);
  assert.match(html, /文档体 2 · 第 3 页/);
  assert.match(html, /2 份（声明上限）/);
  assert.match(html, /&lt;img/);
  assert.doesNotMatch(html, /<img/);
  assert.match(html, /2027-01-01/);
  assert.match(html, /<details class="info-security-document" open><summary>文档体 1/);
  assert.match(html, /<details class="info-security-document"><summary>文档体 2/);
  state.current = 2;
  const currentPageHTML = state.securityHTML({ permissions: [{ ...defaults }, { ...defaults, scope: 1, pages: [2] }] });
  assert.match(currentPageHTML, /<details class="info-security-document" open><summary>文档体 2/);
  state.current = 0;
  suite.check(true, '混合权限按文档体展示份数、有效期并转义文档内容');
}
{
  const html = state.securityHTML({});
  assert.match(html, /未知/);
  assert.match(html, /权限信息暂不可用/);
  assert.doesNotMatch(html, /默认允许/);
  suite.check(true, '缺少数据时显示未知，不误报允许');
}
{
  let resolve;
  state.loadDocumentInfo = () => new Promise(done => { resolve = done; });
  state.update();
  state.documentGeneration++;
  state.infoBasicContent.innerHTML = '新文档';
  resolve({ security: { encrypted: true, state: 'unlocked' }, permissions: [defaults] });
  await tick();
  assert.equal(state.infoBasicContent.innerHTML, '新文档');
  suite.check(true, '切换文件或版本后迟到响应不覆盖新安全信息');
}
{
  let calls = 0;
  const cached = {
    documentGeneration: 1, documentInfoGeneration: 0, documentInfo: null,
    engine: { info: async () => { calls++; return { permissions: [{ ...defaults, export: calls === 1 }] }; } },
  };
  load(cached, sliceFunction('loadDocumentInfo'), { expose: { info: 'loadDocumentInfo' } });
  assert.equal((await cached.info()).permissions[0].export, true);
  await cached.info();
  assert.equal(calls, 1);
  cached.documentGeneration++;
  assert.equal((await cached.info()).permissions[0].export, false);
  assert.equal(calls, 2);
  suite.check(true, '版本生命周期改变后重新查询安全信息，不复用旧权限缓存');
}

function tabState() {
  const state = {
    activeInfoTab: 'basic', infoPanel: { hidden: true },
    infoToggle: { setAttribute() {} },
    infoTabPanels: { basic: element(), security: element(), fonts: element() },
    documentGeneration: 1, infoRenderedGeneration: 1,
    updates: 0, infoRenderRequest: 0, infoStats: element(), infoBasicContent: element(),
    escapeHTML: value => value,
    updateDocumentInfo: () => { state.updates++; },
    infoTabButtons: ['basic', 'security', 'fonts'].map(tab => ({
      dataset: { infoTab: tab }, attrs: {}, focused: false,
      setAttribute(name, value) { this.attrs[name] = value; }, focus() { this.focused = true; },
    })),
  };
  load(state, ['setInfoTab', 'handleInfoTabKeyDown', 'setInfoPanelOpen', 'resetDocumentInfo'].map(sliceFunction).join('\n'), {
    expose: { tab: 'setInfoTab', keydown: 'handleInfoTabKeyDown', open: 'setInfoPanelOpen', reset: 'resetDocumentInfo' },
  });
  state.tab('basic');
  return state;
}
{
  const state = tabState();
  state.infoTabPanels.fonts.scrollTop = 120;
  state.infoTabPanels.security.innerHTML = '<details open>权限</details>';
  state.tab('fonts');
  assert.equal(state.activeInfoTab, 'fonts');
  assert.equal(state.infoTabPanels.fonts.hidden, false);
  assert.equal(state.infoTabPanels.basic.hidden, true);
  assert.equal(state.infoTabPanels.security.hidden, true);
  assert.deepEqual(state.infoTabButtons.map(button => button.attrs['aria-selected']), ['false', 'false', 'true']);
  assert.deepEqual(state.infoTabButtons.map(button => button.tabIndex), [-1, -1, 0]);
  state.open(true); state.open(false); state.open(true);
  assert.equal(state.activeInfoTab, 'fonts');
  assert.equal(state.infoTabPanels.fonts.scrollTop, 120);
  assert.equal(state.infoTabPanels.security.innerHTML, '<details open>权限</details>');
  assert.equal(state.updates, 0);
  state.tab('__proto__');
  assert.equal(state.activeInfoTab, 'fonts');
  suite.check(true, 'Tab 切换同步可见性与 ARIA，重开保留页签、滚动及折叠状态');
}
{
  const state = tabState();
  let prevented = 0, stopped = 0;
  const press = (tabIndex, key) => state.keydown({ target: state.infoTabButtons[tabIndex], key, preventDefault: () => prevented++, stopPropagation: () => stopped++ });
  press(0, 'ArrowLeft');
  assert.equal(state.activeInfoTab, 'fonts');
  assert.equal(state.infoTabButtons[2].focused, true);
  press(2, 'ArrowRight'); assert.equal(state.activeInfoTab, 'basic');
  press(0, 'End'); assert.equal(state.activeInfoTab, 'fonts');
  press(2, 'Home'); assert.equal(state.activeInfoTab, 'basic');
  press(0, 'Tab');
  assert.equal(prevented, 4);
  assert.equal(stopped, 4);
  suite.check(true, '方向键循环切换，Home/End 定位，阻止冒泡触发文档翻页');
}
{
  const state = tabState();
  state.tab('security');
  state.documentGeneration++;
  state.reset('正在读取文档信息…');
  state.open(true);
  assert.equal(state.activeInfoTab, 'security');
  assert.equal(state.updates, 1);
  assert.equal(state.infoRenderedGeneration, -1);
  assert.equal(state.infoStats.innerHTML, '');
  assert.match(state.infoTabPanels.security.innerHTML, /正在读取/);
  // 新文件的真实打开函数在清理之后重置 Tab，版本切换复用清理但不重置 Tab。
  const marker = "setInfoTab('basic');";
  const prefix = sliceFunctionUntil('openSelectedFile', marker);
  assert.ok(prefix.indexOf('clearDocumentView();') < prefix.indexOf(marker));
  assert.doesNotMatch(sliceFunction('switchDocumentVersion'), /setInfoTab\('basic'/);
  state.tab('basic');
  assert.equal(state.activeInfoTab, 'basic');
  suite.check(true, '新文件回到基本信息，版本切换保留 Tab 并清理旧内容');
}
{
  const pending = [];
  state.loadDocumentInfo = async () => ({ title: '最新信息', permissions: [defaults], fonts: [] });
  state.engine.stats = () => new Promise(resolve => pending.push(resolve));
  state.update(); await tick();
  state.update(); await tick();
  const security = state.infoTabPanels.security.innerHTML;
  pending[1]({ fonts: 9 }); await tick();
  assert.match(state.infoStats.innerHTML, />9</);
  pending[0]({ fonts: 1 }); await tick();
  assert.match(state.infoStats.innerHTML, />9</);
  assert.equal(state.infoTabPanels.security.innerHTML, security);
  suite.check(true, '资源统计只更新基本页签，旧请求不会重复追加或覆盖新统计');
  state.pageInfos = [];
  state.update();
  assert.match(state.infoBasicContent.innerHTML, /未打开文档/);
  assert.match(state.infoTabPanels.security.innerHTML, /未打开文档/);
  assert.match(state.infoTabPanels.fonts.innerHTML, /未打开文档/);
  assert.equal(state.infoStats.innerHTML, '');
  suite.check(true, '关闭后清空所有页签中的旧文档信息');
}
process.exitCode = suite.report() ? 1 : 0;
