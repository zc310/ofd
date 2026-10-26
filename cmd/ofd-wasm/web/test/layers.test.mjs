import { sliceFunction, load, createSuite, viewerSource } from './harness.mjs';

const suite = createSuite('按页图层控制');
const deferred = () => { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; };
const element = tag => ({
  tagName: tag, children: [], handlers: {}, dataset: {}, textContent: '',
  append(...nodes) { this.children.push(...nodes); },
  replaceChildren(...nodes) { this.children = nodes; },
  attributes: {},
  setAttribute(name, value) { this.attributes[name] = value; },
  querySelectorAll() { return this.children.flatMap(walk).filter(node => node.type === 'checkbox'); },
  addEventListener(event, handler) { this.handlers[event] = handler; },
});
const walk = node => [node, ...node.children.flatMap(walk)];
const layerList = [{ id: '20', type: 'Body', objects: 2, visible: true }, { id: '30', type: 'Foreground', objects: 1, visible: false }];
const code = ['pageLayerRevision', 'renderLayers', 'loadLayerBranch', 'buildLayerItems', 'syncLayerControls', 'changePageLayers'].map(sliceFunction).join('\n');

function makeState() {
  const state = {
    document: { createElement: element }, layersElement: element('aside'),
    layerRevisions: new Map(), layerUpdates: new Set(), layerListRequest: 0,
    layerExpandedPages: new Set(), layerTreeCache: new Map(), layerTreeStart: 0, layerTreeCurrent: -1, layerTreePageLimit: 100,
    documentGeneration: 1, current: 0, pageInfos: [{}, {}],
    documentActionBusy: false, opening: false, versionSwitching: false,
    activeSidebarTab: 'layers', searchRequest: undefined, searchGeneration: 0,
    searchResults: [{ page: 0 }], activeSearchResult: 0, searchInput: { value: 'old search' },
    pageCards: [{}, {}], thumbnailButtons: [{}, {}],
    invalidated: [], loaded: [], textLoaded: [], linksLoaded: [], thumbnailsLoaded: [], writes: [], statuses: [],
    outlineEmptyMessage: text => ({ ...element('p'), textContent: text }),
    engine: { layers: async () => layerList.map(item => ({ ...item })), setHiddenLayers: async (page, ids) => state.writes.push({ page, ids }) },
    invalidateLayerPage: page => { state.invalidated.push(page); state.layerTreeCache.delete(page); },
    loadPage: page => state.loaded.push(page), loadText: page => state.textLoaded.push(page),
    loadPageLinks: page => state.linksLoaded.push(page), loadThumbnail: page => state.thumbnailsLoaded.push(page),
    updateSearchStatus: value => { state.searchStatus = value; },
    buildTextLayer() {}, updateNavigation() {}, setStatus: text => state.statuses.push(text),
    goTo: page => { state.current = page; },
  };
  return load(state, code, { expose: { renderLayers: 'renderLayers', changePageLayers: 'changePageLayers', pageLayerRevision: 'pageLayerRevision' } });
}

{
  const state = makeState();
  await state.renderLayers();
  const nodes = walk(state.layersElement);
  const originalTree = state.layersElement.children.find(node => node.className === 'layer-tree');
  const boxes = nodes.filter(node => node.type === 'checkbox');
  suite.check(boxes.length === 2 && boxes[0].checked && !boxes[1].checked, '按实际图层显示各自状态');
  suite.check(nodes.some(node => node.textContent === '正文层 · ID 20'), '展示类型和文件中的图层 ID');
  suite.check(nodes.some(node => node.textContent.includes('导出和打印使用当前显示图层')), '说明导出打印的行为');
  boxes[0].checked = false; boxes[0].handlers.change();
  await new Promise(resolve => setImmediate(resolve));
  suite.check(JSON.stringify(state.writes[0]) === JSON.stringify({ page: 0, ids: ['20', '30'] }), '关闭一个图层时保留其他隐藏状态');
  suite.check(state.loaded.join() === '0' && state.textLoaded.join() === '0' && state.linksLoaded.join() === '0' && state.thumbnailsLoaded.join() === '0', '仅重绘目标页并刷新文字、链接和缩略图');
  suite.check(state.pageLayerRevision(0) > 0 && state.pageLayerRevision(1) === 0, '每页具有独立的内容版本');
  suite.check(!state.searchResults.length && state.searchStatus === '按 Enter 搜索', '旧搜索索引失效后提示重新搜索');
  suite.check(state.layersElement.children.find(node => node.className === 'layer-tree') === originalTree && walk(state.layersElement).includes(boxes[0]), '勾选后保留原树和复选框节点');
  boxes[1].checked = true; boxes[1].handlers.change();
  await new Promise(resolve => setImmediate(resolve));
  suite.check(JSON.stringify(state.writes[1]) === JSON.stringify({page:0,ids:['20']}), '连续操作原有复选框时保留最新的其他层状态');
  await state.renderLayers();
  const cachedBoxes = walk(state.layersElement).filter(node => node.type === 'checkbox');
  suite.check(!cachedBoxes[0].checked && cachedBoxes[1].checked, '重新展开面板时沿用更新后的缓存状态');
}

{
  const state = makeState(); const first = deferred(); const second = deferred();
  state.engine.layers = index => index === 0 ? first.promise : second.promise;
  const old = state.renderLayers(); state.current = 1; state.layerExpandedPages.delete(0); const latest = state.renderLayers();
  second.resolve([{ id: '99', type: 'Custom', objects: 0, visible: true }]); await latest;
  first.resolve(layerList); await old;
  const nodes = walk(state.layersElement);
  suite.check(nodes.some(node => node.textContent === '自定义层 · ID 99') && !nodes.some(node => node.textContent === '正文层 · ID 20'), '旧页面的图层响应不能覆盖新页面');
}

{
  const state = makeState(); const calls = [];
  state.engine.layers = async index => {calls.push(index); return layerList.map(layer => ({...layer}));};
  await state.renderLayers();
  const tree = state.layersElement.children.find(node => node.className === 'layer-tree');
  suite.check(tree.children.length === 2 && tree.children[0].dataset.page === '0' && tree.children[1].dataset.page === '1', '页面是父节点，图层是页面的子节点');
  suite.check(calls.join() === '0' && tree.children[1].children[1].hidden, '初始只展开并加载当前页');
  tree.children[1].children[0].children[0].handlers.click();
  await new Promise(resolve => setImmediate(resolve));
  suite.check(calls.join() === '0,1' && !tree.children[1].children[1].hidden, '展开其他页面时才加载该页图层');
  const boxes = walk(tree.children[1]).filter(node => node.type === 'checkbox');
  boxes[0].checked = false; boxes[0].handlers.change();
  await new Promise(resolve => setImmediate(resolve));
  suite.check(state.writes[0].page === 1 && state.loaded.join() === '1', '其他页的子节点操作只修改所属页');
  suite.check(state.layerExpandedPages.has(0) && state.layerExpandedPages.has(1), '更新图层后保留各页展开状态');
  suite.check(calls.join() === '0,1', '成功切换不重新读取图层列表');
  const rebuilt = state.layersElement.children.find(node => node.className === 'layer-tree');
  rebuilt.children[0].children[0].children[0].handlers.click();
  await state.renderLayers();
  suite.check(!state.layerExpandedPages.has(0), '刷新面板不重新展开手动折叠的当前页');
  const currentTree = state.layersElement.children.find(node => node.className === 'layer-tree');
  currentTree.children[1].children[0].children[1].handlers.click({preventDefault() {}});
  suite.check(state.current === 1, '点击页名定位，无需额外定位按钮');
  suite.check(!walk(currentTree).some(node => node.textContent === '全部显示' || node.textContent === '全部隐藏' || node.textContent === '定位'), '树中移除重复操作按钮');
}

{
  const state = makeState(); const pending = deferred();
  await state.renderLayers();
  const originalTree = state.layersElement.children.find(node => node.className === 'layer-tree');
  const boxes = walk(state.layersElement).filter(node => node.type === 'checkbox');
  state.engine.setHiddenLayers = () => pending.promise;
  boxes[0].checked = false; boxes[0].handlers.change();
  suite.check(boxes[0].attributes['aria-disabled'] === 'true' && !boxes[0].disabled, '切换中标记忙碌但保持输入焦点可用');
  boxes[1].checked = true; boxes[1].handlers.change();
  suite.check(!boxes[1].checked, '切换中阻止额外勾选并恢复其原状态');
  boxes[0].checked = true; boxes[0].handlers.change();
  suite.check(!boxes[0].checked, '重复操作正在切换的复选框时保留待生效状态');
  pending.reject(new Error('失败样本'));
  await new Promise(resolve => setImmediate(resolve));
  suite.check(boxes[0].checked && !boxes[1].checked && boxes[0].attributes['aria-disabled'] === 'false', '失败后原位恢复引擎实际勾选状态');
  suite.check(state.layersElement.children.find(node => node.className === 'layer-tree') === originalTree, '失败恢复也不重建图层树');
}

{
  const state = makeState(); state.pageInfos = new Array(100000).fill({}); state.current = 54321;
  const calls = []; state.engine.layers = async page => {calls.push(page); return [];};
  await state.renderLayers();
  const tree = state.layersElement.children.find(node => node.className === 'layer-tree');
  suite.check(tree.children.length === 100 && tree.children[0].dataset.page === '54300', '大文档按组展示页面节点，DOM 有界');
  suite.check(calls.join() === '54321', '大文档不扫描折叠页的图层');
  const next = walk(state.layersElement).find(node => node.textContent === '下一组'); next.handlers.click();
  await new Promise(resolve => setImmediate(resolve));
  suite.check(state.layerTreeStart === 54400 && calls.join() === '54321', '浏览下一组不会提前读取各页内容');
  walk(state.layersElement).find(node => node.textContent === '当前页').handlers.click({preventDefault() {}});
  await new Promise(resolve => setImmediate(resolve));
  suite.check(state.layerTreeStart === 54300, '可返回当前页所在分组');
}

{
  const state = makeState();
  state.engine.layers = async () => [
    {key:'20',id:'20',kind:'layer',type:'Body',objects:2,visible:true},
    {key:'template:20',id:'20',kind:'template',type:'Background',objects:3,visible:true},
    {key:'template:21',id:'21',kind:'template',type:'Foreground',name:'页眉',objects:1,visible:true},
  ];
  await state.renderLayers();
  const nodes = walk(state.layersElement);
  suite.check(nodes.some(node => node.textContent === '背景模板 · ID 20') && nodes.some(node => node.textContent === '前景模板 · 页眉 · ID 21'), '树中分别展示背景模板、前景模板与正文层');
  const boxes = nodes.filter(node => node.type === 'checkbox');
  boxes[1].checked = false; boxes[1].handlers.change();
  await new Promise(resolve => setImmediate(resolve));
  suite.check(JSON.stringify(state.writes[0]) === JSON.stringify({page:0,ids:['template:20']}), '正文与模板 ID 相同时按独立 Key 控制');
  const branch = state.layersElement.children.find(node => node.className === 'layer-tree').children[0].children[1];
  suite.check(!walk(branch).some(node => node.tagName === 'button'), '展开页只展示复选框，无成排按钮');
}

{
  const state = makeState(); const pending = deferred();
  state.engine.layers = () => pending.promise;
  const request = state.renderLayers();
  const tree = state.layersElement.children.find(node => node.className === 'layer-tree');
  tree.children[0].children[0].children[0].handlers.click();
  pending.resolve(layerList); await request;
  suite.check(!walk(tree.children[0]).some(node => node.type === 'checkbox'), '折叠后迟到响应不能重建子节点');
}

{
  const state = makeState(); const pending = deferred(); state.engine.setHiddenLayers = () => pending.promise;
  const updating = state.changePageLayers(0, ['20']);
  await state.changePageLayers(1, ['30']);
  suite.check(state.layerUpdates.size === 1 && state.layerUpdates.has(0), '更新期间避免交叉修改');
  state.documentGeneration++; pending.resolve(true); await updating;
  suite.check(!state.loaded.length && !state.textLoaded.length, '切换文档后旧更新不能刷新新文档');
}

{
  const state = makeState(); const setting = deferred(); const reading = deferred();
  await state.renderLayers();
  state.engine.setHiddenLayers = () => setting.promise;
  const updating = state.changePageLayers(0, ['20', '30']);
  state.engine.layers = () => reading.promise;
  const loading = state.renderLayers();
  setting.resolve(true); await updating;
  reading.resolve(layerList); await loading;
  const boxes = walk(state.layersElement).filter(node => node.type === 'checkbox');
  suite.check(boxes.length === 2 && boxes.every(box => !box.checked), '切换期间重新展开的分支采用最新状态，不停留在加载提示');
}

{
  const state = makeState(); state.engine.setHiddenLayers = async () => { throw new Error('失败样本'); };
  await state.changePageLayers(0, ['20']);
  suite.check(!state.layerUpdates.size && state.statuses.some(text => text.includes('失败样本')), '失败后恢复操作并展示错误');
  suite.check(state.loaded.join() === '0', '失败后重新读取引擎的实际页面');
  state.documentActionBusy = true; state.writes = [];
  await state.changePageLayers(0, []);
  suite.check(!state.writes.length, '导出打印期间禁止改变图层');
}

// 驱动实际异步加载函数，确保旧图片、文字和链接不因迟到响应复活。
{
  const pending = deferred(); const revisions = new Map(); const cache = new Map();
  const state = {
    layerUpdates: new Set(), layerRevisions: revisions, pageCache: cache, thumbnailCache: new Map(),
    pageRequests: new Map(), thumbnailRequests: new Map(), renderFormat: 'png', zoomGeneration: 0, documentGeneration: 1,
    pageDPI: () => 96, engine: { renderPage: () => pending.promise },
    transparentRenderBackground: 'transparent', renderMimeType: () => 'image/png',
    URL: { createObjectURL: () => 'blob:old', revokeObjectURL() {} },
    showImageWhenReady() { throw new Error('不应显示旧图片'); }, markPageLoaded() { throw new Error('不应登记旧图片'); },
    isCancelledError: () => false,
  };
  load(state, ['pageLayerRevision', 'cacheKey', 'loadImage'].map(sliceFunction).join('\n'), { expose: { loadImage: 'loadImage' } });
  const request = state.loadImage(0, 'page', 1, {}); revisions.set(0, 1); pending.resolve(new Uint8Array([1]));
  suite.check(await request === null && cache.size === 0, '旧图层图片不写入缓存、不覆盖页面');
}

{
  const pending = deferred(); const revisions = new Map();
  const state = {
    layerUpdates: new Set(), layerRevisions: revisions, textCache: new Map(), textRequests: new Map(),
    pageCards: [], documentGeneration: 1, engine: { text: () => pending.promise },
    trimTextCache() {}, buildTextLayer() {}, isCancelledError: () => false,
    pagePermissionAllowed: () => true,
  };
  load(state, ['pageLayerRevision', 'loadText'].map(sliceFunction).join('\n'), { expose: { loadText: 'loadText' } });
  const request = state.loadText(0); revisions.set(0, 1); pending.resolve([{ text: 'hidden' }]); await request;
  suite.check(!state.textCache.size, '旧图层的文字响应不能恢复隐藏文字');
}

{
  const pending = deferred(); const revisions = new Map();
  const state = {
    layerUpdates: new Set(), layerRevisions: revisions, pageLinks: new Map(), documentGeneration: 1,
    engine: { pageLinksForPage: () => pending.promise }, applyPageLinks() { throw new Error('不应重建旧链接'); },
  };
  load(state, ['pageLayerRevision', 'loadPageLinks'].map(sliceFunction).join('\n'), { expose: { loadPageLinks: 'loadPageLinks' } });
  state.loadPageLinks(0); revisions.set(0, 1); state.pageLinks.delete(0);
  pending.resolve([{ boundary: { x: 0, y: 0, width: 1, height: 1 } }]); await new Promise(resolve => setImmediate(resolve));
  suite.check(!state.pageLinks.has(0), '旧图层的链接响应不能恢复隐藏热区');
}

suite.check(viewerSource().includes("sidebarMoreLayers?.addEventListener('click', () => setSidebarTab('layers'))"), '图层侧栏入口已连接');
process.exit(suite.report() ? 1 : 0);
