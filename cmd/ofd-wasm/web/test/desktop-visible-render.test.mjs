import { sliceFunction, load, createSuite } from './harness.mjs';

const suite = createSuite('桌面可见页面优先渲染');
const timers = new Map(), loaded = [], cleared = [];
let nextTimer = 0;
const state = {
  engine: { nativeDesktop: true }, pageCards: [{}, {}], pageCardRequests: new Map(), pageRenderTimers: new Map(),
  documentGeneration: 1, loadPage: index => loaded.push(index),
  setTimeout: (callback, delay) => { const id = ++nextTimer; timers.set(id, { callback, delay }); return id; },
  clearTimeout: id => { cleared.push(id); timers.delete(id); },
};
load(state, sliceFunction('schedulePageRender'), { expose: { schedule: 'schedulePageRender' } });
state.schedule(1, 300);
const timer = state.pageRenderTimers.get(1);
state.schedule(1, 0);
suite.check(cleared.includes(timer) && !state.pageRenderTimers.has(1) && loaded.join() === '1', '进入视口时撤销等待并立即渲染');
state.schedule(1, 300);
const activeTimer = state.pageRenderTimers.get(1);
state.documentGeneration++;
timers.get(activeTimer).callback();
suite.check(loaded.length === 1, '文档切换后延迟任务不能渲染旧页面');

const scheduled = [];
const image = { hidden: true, src: '' };
const cards = Array.from({ length: 3 }, () => ({ querySelector: () => image, classList: { contains: () => false, add() {} } }));
const view = {
  engine: { nativeDesktop: true }, window: { innerHeight: 100 }, pageVirtualTrack: {},
  pageSpreads: [0, 1, 2].map(index => ({ offset: index * 100, height: 100, pages: [index], element: {} })),
  pageInfos: [{}, {}, {}], pageSpreadsGeneration: 1, documentGeneration: 1, current: 0,
  mountedPageSpreads: new Set([0, 1, 2]), pageCards: cards, pageCardRequests: new Map(),
  updatePageVirtualTranslate() {}, pageTrackScrollY: () => 100, pageTrackContentFromScroll: value => value,
  pageSpreadRange: () => [0, 3], mountPageSpread() {}, unmountPageSpread() {}, pageImageIsReady: () => false,
  schedulePageRender: (index, delay) => scheduled.push([index, delay]),
};
load(view, sliceFunction('updatePageVirtualWindow'), { expose: { update: 'updatePageVirtualWindow' } });
view.update(false);
suite.check(scheduled.find(item => item[0] === 1)?.[1] === 0, '真正可见的非当前页立即开始');
suite.check(scheduled.find(item => item[0] === 2)?.[1] === 300, '缓冲区的离屏页面仍延迟预加载');
view.engine.nativeDesktop = false;
scheduled.length = 0;
view.update(false);
suite.check(scheduled.find(item => item[0] === 1)?.[1] === 300, '浏览器原有延迟策略不变');
process.exit(suite.report() ? 1 : 0);
