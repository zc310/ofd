import { sliceFunction, load, createSuite, viewerSource } from './harness.mjs';

const suite = createSuite('桌面离屏页面缓存');
const removed = [], cancelled = [];
const card = {};
const spread = { pages: [0], element: { remove() { removed.push('dom'); } } };
const state = {
  engine: { nativeDesktop: true }, pageSpreads: [spread], mountedPageSpreads: new Set([0]),
  pageCards: [card], pageTextTimers: new Map(), pageInfoRequests: new Map(),
  pageCardRequests: new Map(), textCache: new Map([[0, ['text']]]), renderedPages: new Set([0]),
  failedPages: new Set(), documentGeneration: 1,
  pageCache: { clearIndex: (...args) => removed.push(args) },
  thumbnailCache: { clearIndex: (...args) => removed.push(args) },
  cancelPageRequest: index => cancelled.push(`page:${index}`),
  cancelTextRequest: index => cancelled.push(`text:${index}`),
  resizeObserver: null, rehomeEmbeddedMedia() {},
};
load(state, sliceFunction('unmountPageSpread'), { expose: { unmount: 'unmountPageSpread' } });
state.unmount(0);
suite.check(removed.length === 1 && removed[0] === 'dom', '桌面保留有界图片缓存并移除页面 DOM');
suite.check(cancelled.join(',') === 'page:0,text:0' && state.textCache.size === 0, '离屏仍取消未完成请求并释放文字');
suite.check(!state.mountedPageSpreads.has(0) && !state.pageCards[0], '离屏页面不保留挂载节点');
state.engine.nativeDesktop = false;
spread.element = { remove() {} };
state.pageCards[0] = card;
state.unmount(0);
suite.check(removed.some(item => Array.isArray(item) && item[0] === 'page') &&
  removed.some(item => Array.isArray(item) && item[0] === 'thumbnail'), '浏览器仍按原策略清除离屏图片');
const source = viewerSource();
suite.check(source.includes('const pageCache = new BlobURLCache(128 << 20') &&
  source.includes('const thumbnailCache = new BlobURLCache(32 << 20'), '桌面复用现有图片缓存预算');
process.exit(suite.report() ? 1 : 0);
