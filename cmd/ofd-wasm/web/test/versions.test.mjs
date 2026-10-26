// 驱动真实版本切换函数：独立入口、失败恢复与旧文档响应隔离。
import { sliceFunction, load, createSuite } from './harness.mjs';

const suite = createSuite('文档版本');
const code = ['buildVersionItem', 'switchDocumentVersion'].map(sliceFunction).join('\n');

function baseState() {
  const state = {
    selectedVersionID: '', selectedVersionScope: -1, versionSwitching: false,
    versionPageFilter: null, opening: false, documentGeneration: 1,
    pageInfos: [{ index: 0 }, { index: 1 }], current: 1, currentDocumentKey: 'doc',
    openRequest: undefined, activeSidebarTab: 'versions', infoPanel: { hidden: true },
    calls: [], builds: 0, clears: 0,
    pagePermissions: new Map([[0, { pages: [0, 1], export: false, printable: false }]]),
    setDocumentPermissions: permissions => { state.appliedPermissions = permissions; },
    engine: { selectVersion: async (scope, id) => {
      state.calls.push([scope, id]);
      return { pages: id ? [{ index: 0, width: 99 }] : [{ index: 0 }, { index: 1 }], permissions: [{ export: !!id, printable: true }] };
    } },
    clearDocumentView: () => { state.clears++; },
    clearInjectedFonts: () => { state.fontsCleared = true; },
    loadEmbeddedFonts: generation => { state.fontLoadGeneration = generation; },
    saveReadingPosition: () => {}, restorePageRotation: () => {},
    readReadingPositions: () => ({}), readSidebarScroll: () => ({}), sidebarScroll: {},
    renderVersions: () => {}, buildPages: () => { state.builds++; },
    setStatus: text => { state.status = text; },
    isCancelledError: error => error.name === 'AbortError',
    loadOutline: () => { state.outlineReloaded = true; },
    fetchPageLinks: () => { state.linksReloaded = true; },
    loadSignatures: () => { state.signaturesReloaded = true; return Promise.resolve([]); },
    document: { createElement: () => {
      const button = { disabled: false, textContent: '', classList: { add() {} } };
      button.addEventListener = (_, fn) => { button.click = fn; };
      return button;
    } },
  };
  return load(state, code, { expose: { buildVersionItem: 'buildVersionItem', switchVersion: 'switchDocumentVersion' } });
}

suite.group('独立历史页面无需出现在主文档页面清单中');
{
  const state = baseState();
  const item = { scope: 0, id: 'v1', doc_root: '/Old/Document.xml', pages: [] };
  const button = state.buildVersionItem(item);
  suite.check(!button.disabled, '独立入口可以选择');
  await state.switchVersion(item);
  suite.check(state.calls[0][1] === 'v1' && state.pageInfos.length === 1 && state.pageInfos[0].width === 99, '加载历史页面尺寸');
  suite.check(state.selectedVersionID === 'v1' && state.current === 0, '记录版本并从首页阅读');
  suite.check(state.appliedPermissions[0].export === true, '版本切换同步更新权限');
  suite.check(state.fontsCleared && state.fontLoadGeneration === state.documentGeneration && state.clears === 1, '替换字体并清理旧视图');
  suite.check(state.outlineReloaded && state.linksReloaded && state.signaturesReloaded, '重新读取导航、动作和签章');
  suite.check(state.currentDocumentKey === 'doc:version:0:v1', '阅读记录按版本隔离');
  state.readReadingPositions = () => ({ doc: { page: 1 } });
  await state.switchVersion(item);
  suite.check(state.calls[1][1] === '' && state.pageInfos.length === 2, '再次点击加载主文档');
  suite.check(state.selectedVersionID === '' && state.currentDocumentKey === 'doc', '恢复主文档阅读记录');
  suite.check(state.appliedPermissions[0].export === false, '恢复主文档权限');
  suite.check(state.current === 1, '恢复主文档原阅读页码');
}

suite.group('相同版本 ID 以文档体区分');
{
  const state = baseState();
  state.selectedVersionID = 'v1'; state.selectedVersionScope = 0;
  await state.switchVersion({ scope: 1, id: 'v1' });
  suite.check(state.calls[0][0] === 1 && state.calls[0][1] === 'v1', '选择另一文档体的版本');
}

suite.group('失败保留原版本与页面');
{
  const state = baseState();
  const pages = state.pageInfos;
  state.selectedVersionID = 'v1'; state.selectedVersionScope = 0;
  state.engine.selectVersion = async () => { throw new Error('缺少资源'); };
  await state.switchVersion({ scope: 0, id: 'bad' });
  suite.check(state.pageInfos === pages && state.selectedVersionID === 'v1' && state.current === 1, '保留原版本');
  suite.check(state.builds === 1 && !state.versionSwitching && state.status.includes('缺少资源'), '恢复页面并说明失败原因');
  suite.check(state.appliedPermissions[0].export === false, '失败恢复原权限');
}

suite.group('切换期间的新文档使旧响应失效');
{
  const state = baseState();
  let resolve;
  state.engine.selectVersion = () => new Promise(done => { resolve = done; });
  const pending = state.switchVersion({ scope: 0, id: 'v1' });
  suite.check(!await state.switchVersion({ scope: 0, id: 'v2' }), '切换期间不重复发起请求');
  state.documentGeneration++;
  resolve({ pages: [] });
  await pending;
  suite.check(state.selectedVersionID === '' && state.builds === 0 && !state.fontsCleared, '旧响应不覆盖新文档');
}

suite.group('入口不可用和打开期间禁止操作');
{
  const state = baseState();
  const button = state.buildVersionItem({ scope: 0, id: 'bad', pages: [0] });
  button.click();
  suite.check(button.disabled && state.calls.length === 0, '缺少入口不能靠同名页面筛选');
  state.opening = true;
  suite.check(state.buildVersionItem({ scope: 0, id: 'v1', doc_root: '/Old/Document.xml' }).disabled, '打开期间禁用按钮');
}

process.exitCode = suite.report() ? 1 : 0;
