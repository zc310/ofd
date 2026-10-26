// 用真实入口验证禁止权限在产生剪贴板、文件或打印窗口之前生效。
import { sliceFunction, sliceFunctionUntil, load, createSuite } from './harness.mjs';

const suite = createSuite('文档操作权限');
const helpers = ['setDocumentPermissions', 'pagePermissionAllowed', 'requirePagePermission', 'blockRestrictedCopy', 'blockRestrictedPageSave'];
const actions = ['copyCurrentPageText', 'copyDocumentText', 'printSelectedPages', 'openPrintDialog', 'openExportDialog', 'exportDocumentPages', 'startExport', 'loadText', 'buildTextLayer'];
function baseState() {
  const state = {
    pagePermissions: new Map(), documentPermissionSummary: {}, pageInfos: [{}, {}], current: 0,
    documentActionBusy: false, documentGeneration: 1, pageCards: [], textCache: new Map(),
    clipboard: 0, windows: 0, saves: 0, textLoads: 0, statuses: [], layerUpdates: new Set(),
    engine: { text: () => { state.textLoads++; throw new Error('不应读取'); } },
    setStatus: text => state.statuses.push(text), showCopyFeedback: text => state.statuses.push(text),
    setDocumentActionBusy: () => { throw new Error('不应开始受限操作'); },
    chooseSaveFile: () => { state.saves++; }, exportIndexes: () => [0, 1],
    exportRange: { value: 'all' }, exportError: {},
    window: { open: () => { state.windows++; }, getSelection: () => state.selection },
    selection: null,
  };
  return load(state, [...helpers, ...actions].map(sliceFunction).join('\n'), {
    expose: Object.fromEntries([...helpers, ...actions].map(name => [name, name])),
  });
}

suite.group('按文档体检查所选页面，混合选择整体拒绝');
{
  const state = baseState();
  state.setDocumentPermissions([{ scope: 0, pages: [0], export: false, printable: false }, { scope: 1, pages: [1], export: true, printable: true }]);
  for (const action of ['copy', 'export', 'print']) {
    suite.check(!state.pagePermissionAllowed(action, [0]), `${action} 拒绝受限页`);
    suite.check(state.pagePermissionAllowed(action, [1]), `${action} 允许另一文档体`);
    suite.check(!state.pagePermissionAllowed(action, [1, 0]), `${action} 拒绝混合选择`);
    suite.check(!state.pagePermissionAllowed(action, [99]), `${action} 未知页面拒绝`);
  }
  suite.check(state.documentPermissionSummary.export && state.documentPermissionSummary.print && !state.documentPermissionSummary.copy, '可选页导出/打印，禁止复制全文');
  await state.copyCurrentPageText();
  await state.copyDocumentText();
  await state.printSelectedPages([0, 1]);
  await state.startExport();
  let denied = false;
  try { await state.exportDocumentPages([0], 96, 'png'); } catch (error) { denied = error.code === 'permission_denied'; }
  suite.check(denied && !state.windows && !state.saves && !state.textLoads, '直接入口在读取和输出前拒绝');
  await state.loadText(0);
  suite.check(state.textLoads === 0, '受限页不请求可选择文字层');
  state.setDocumentPermissions([]);
  suite.check(!state.documentPermissionSummary.print && !state.documentPermissionSummary.export, '清理文档同步清理旧权限');
  state.setDocumentPermissions([{ pages: [0, 1], export: true, printable: true }]);
  suite.check(state.documentPermissionSummary.copy && state.pagePermissionAllowed('print', [0, 1]), '新版本允许后恢复操作');
}

suite.group('复制事件检查跨页选择，输入框保持正常');
{
  const state = baseState();
  state.setDocumentPermissions([{ pages: [0], export: false }, { pages: [1], export: true }]);
  state.pageCards = [{ dataset: { index: '0' } }, { dataset: { index: '1' } }];
  let blocked = 0;
  const event = { target: {}, preventDefault: () => blocked++ };
  state.selection = { rangeCount: 1, getRangeAt: () => ({ intersectsNode: card => card.dataset.index === '0' }) };
  suite.check(state.blockRestrictedCopy(event) && blocked === 1, '跨入受限页阻止系统复制');
  state.selection = { rangeCount: 1, getRangeAt: () => ({ intersectsNode: card => card.dataset.index === '1' }) };
  suite.check(!state.blockRestrictedCopy(event) && blocked === 1, '仅选择允许页可复制');
  event.target = { matches: () => true };
  suite.check(!state.blockRestrictedCopy(event), '搜索输入框的复制不受影响');
  state.blockRestrictedPageSave({ target: { closest: () => state.pageCards[0] }, preventDefault: () => blocked++ });
  suite.check(blocked === 2, '受限页面阻止右键保存和拖出');
}

suite.group('菜单和快捷键使用相同限制');
{
  const state = baseState();
  state.setDocumentPermissions([{ pages: [0, 1], export: false, printable: false }]);
  state.openPrintDialog(); state.openExportDialog();
  suite.check(state.statuses.some(text => text.includes('禁止打印')) && state.statuses.some(text => text.includes('禁止导出')), '直接打开菜单也检查权限');
  let prevented = 0;
  const keyState = { ...state, HTMLElement: class {}, openPrintDialog: state.openPrintDialog, copyCurrentPageText: state.copyCurrentPageText };
  load(keyState, sliceFunction('handleDocumentKeyDown'), { expose: { keydown: 'handleDocumentKeyDown' } });
  const keydown = keyState.keydown;
  keydown({ key: 'p', ctrlKey: true, preventDefault: () => prevented++ });
  keydown({ key: 'c', metaKey: true, shiftKey: true, preventDefault: () => prevented++ });
  suite.check(prevented === 2 && !state.windows && !state.textLoads, '打印和复制快捷键不绕过权限');
}

suite.group('操作按钮反映当前页和整份文档的权限');
{
  const state = baseState();
  state.setDocumentPermissions([{ pages: [0], export: false, printable: false }, { pages: [1], export: true, printable: true }]);
  Object.assign(state, { pageNumber: {}, pageCount: {}, previous: {}, next: {}, printPage: {}, exportDocument: {}, copyPageText: {}, copyAllTextButton: {}, pageLayoutIsDouble: () => false });
  const marker = "copyAllTextButton.disabled = documentActionBusy || layerUpdates.size > 0 || !documentPermissionSummary.copy;";
  load(state, sliceFunctionUntil('updateNavigation', marker), { expose: { update: 'updateNavigation' } });
  state.update();
  suite.check(state.copyPageText.disabled && state.copyAllTextButton.disabled && !state.printPage.disabled && !state.exportDocument.disabled, '当前页禁止复制，仍可选择另一文档体打印和导出');
  state.current = 1; state.update();
  suite.check(!state.copyPageText.disabled && state.copyAllTextButton.disabled, '翻页只恢复允许页的复制');
}

suite.group('允许操作仍经 WASM 检查，拒绝后不会写入剪贴板');
{
  const state = {
    pagePermissions: new Map(), documentPermissionSummary: {}, pageInfos: [{}], current: 0,
    documentGeneration: 1, documentActionBusy: false, documentActionCancelRequested: false,
    checks: [], writes: [], statuses: [],
    engine: { checkPermission: async (action, indices) => { state.checks.push([action, indices]); } },
    setDocumentActionBusy: value => { state.documentActionBusy = value; },
    throwIfDocumentActionCancelled: generation => { if (generation !== state.documentGeneration) throw new Error('文档已切换'); },
    loadText: async () => {}, pageText: () => 'allowed',
    collectDocumentText: async () => ({ text: 'allowed all', failed: 0 }),
    copyTextToClipboard: async text => { state.writes.push(text); },
    setStatus: text => state.statuses.push(text), showCopyFeedback: text => state.statuses.push(text),
  };
  load(state, [...helpers, 'copyCurrentPageText', 'copyDocumentText'].map(sliceFunction).join('\n'), {
    expose: { set: 'setDocumentPermissions', copyPage: 'copyCurrentPageText', copyAll: 'copyDocumentText' },
  });
  state.set([{ pages: [0], export: true, printable: true }]);
  await state.copyPage(); await state.copyAll();
  suite.check(state.checks.length === 2 && state.writes.join('|') === 'allowed|allowed all', '允许时复制当前页与全文');
  state.engine.checkPermission = async () => { const error = new Error('引擎禁止复制'); error.code = 'permission_denied'; throw error; };
  await state.copyPage(); await state.copyAll();
  suite.check(state.writes.length === 2 && state.statuses.includes('引擎禁止复制'), '引擎拒绝时不使用页面缓存绕过');
  state.engine.checkPermission = async () => { state.documentGeneration++; };
  await state.copyPage();
  suite.check(state.writes.length === 2, '检查期间切换文档不复制旧缓存');
}

suite.group('受限页面保留搜索高亮，不创建可选择的文字节点');
{
  const layer = { parentElement: {}, children: [], replaceChildren() { this.children = []; }, append(node) { this.children.push(node); } };
  const state = {
    pageCards: [{ querySelector: () => layer }], pageInfos: [{ width: 100, height: 100 }],
    textCache: new Map([[0, [{ text: '不应出现在 DOM' }]]]), textLayerVisible: true,
    pagePermissionAllowed: () => false, searchResults: [{ page: 0, rects: [{ x: 1, y: 2, width: 3, height: 4 }] }], activeSearchResult: 0,
    mergeHighlightRects: rects => rects,
    document: { createElement: () => ({ style: {}, classList: { add() {} }, setAttribute() {} }) },
  };
  load(state, sliceFunction('buildTextLayer'), { expose: { build: 'buildTextLayer' } });
  state.build(0);
  suite.check(layer.children.length === 1 && layer.children[0].className === 'search-highlight' && !layer.children[0].textContent, '缓存文字不进入 DOM，命中坐标仍可高亮');
}

suite.group('打印正份额提示与缺省份额');
{
  const state = {
    printForm: { elements: { namedItem: () => ({ value: 'all' }) } }, printCustomRange: {}, printError: {}, printPermissionNote: {},
    pagePermissions: new Map([[0, { print_copies: -1 }], [1, { print_copies: 2 }]]), selectedPrintIndexes: () => [0, 1],
  };
  load(state, sliceFunction('updatePrintRangeControl'), { expose: { update: 'updatePrintRangeControl' } });
  state.update();
  suite.check(state.printPermissionNote.textContent.includes('2 份'), '选页显示正打印份额');
  state.selectedPrintIndexes = () => [0]; state.update();
  suite.check(state.printPermissionNote.textContent === '', '不限份额不显示错误限制');
}

process.exitCode = suite.report() ? 1 : 0;
