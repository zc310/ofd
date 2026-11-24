// 浏览器与 Wails 共用真实 viewer.js 导航函数；历史 API、布局和文档引擎使用可观察替身。
import { load, sliceFunction, sliceListenerHandler, bind } from './harness.mjs';

const names = [
  'goTo', 'goToDestination', 'applyDestinationJump', 'captureLinkHistoryLocation',
  'resetLinkNavigationHistory', 'recordLinkHistoryJump', 'restoreLinkHistoryLocation', 'handleLinkHistoryPopState',
  'pushDesktopLinkHistory', 'recordDesktopLinkHistoryJump', 'desktopLinkHistoryBlocked',
  'navigateDesktopLinkHistory', 'handleDesktopHistoryMouse', 'handleDesktopHistoryKey',
  'pageTrackContentFromScroll', 'pageTrackScrollFromContent',
];

export function createLinkHistoryHarness({ compressed = false, native = false } = {}) {
  const callbacks = [];
  const state = {
    engine: { nativeDesktop: native },
    documentGeneration: 1, current: 0, zoom: 2, zoomMode: 'manual', pageLayout: 'single', pageRotation: 0,
    pageEntryActionDepth: 0, opening: false, versionSwitching: false, documentActionBusy: false,
    nativeSelecting: false, desktopExternalLinkRequest: undefined,
    document: { querySelector: () => state.modal || null },
    linkNavigationHistory: undefined, linkHistoryRestoring: false, linkDestinationToken: 0, linkHistoryLimit: 128,
    pageTrackScale: compressed ? 12 : 1,
    pageInfos: Array.from({ length: 8 }, () => ({ width: 210, height: 297 })),
    pagesElement: { scrollLeft: 40 },
    rotatePageButton: { setAttribute() {} },
    pageSpreadPositionForPage: page => state.pageLayout === 'single' ? page : Math.floor(page / 2),
    pageSpreadOffset: row => row * 1000 * state.zoom,
    mountPageSpread: row => { state.mounted = row; },
    setCurrent: page => {
      state.current = page;
      if (state.linkHistoryRestoring) state.suppressedActions++;
    },
    setZoom: (value, mode) => { state.zoom = value; state.zoomMode = mode; },
    setPageLayout: layout => { state.pageLayout = layout; },
    destPagePoint: (_info, dest) => ({ x: dest.left || 0, y: dest.top || 0 }),
    fitRectZoom: () => 4, normalizeDestZoom: value => value || 0,
    scrollToDestinationX: (_page, left) => { state.pagesElement.scrollLeft = left; },
    schedulePageVirtualTranslate() {}, updatePageVirtualWindow() {}, hideLinkPreview() {},
    requestAnimationFrame: callback => { callbacks.push(callback); },
    suppressedActions: 0, statuses: [],
    setStatus: message => state.statuses.push(message),
    currentRemoteSource: undefined, pendingDownload: undefined,
    remoteDocumentSource: () => state.remoteSource,
    loadRemoteFromLocation: () => { state.remoteLoads++; },
    remoteLoads: 0,
  };
  state.window = {
    scrollY: 300,
    scrollTo: options => {
      state.window.scrollY = options.top;
      state.lastBehavior = options.behavior;
    },
  };
  state.pageVirtualTrack = { getBoundingClientRect: () => ({ top: 100 - state.window.scrollY }) };
  const entries = [{
    state: { preserved: 'other-feature' }, url: '?file=https%3A%2F%2Fexample.test%2Fdoc.ofd',
    restoration: 'auto', scrollY: state.window.scrollY,
  }];
  let cursor = 0;
  state.window.history = {
    get scrollRestoration() { return entries[cursor].restoration; },
    set scrollRestoration(value) { entries[cursor].restoration = value; },
    get state() { return entries[cursor].state; },
    replaceState(data, _title, url) {
      entries[cursor] = { ...entries[cursor], state: structuredClone(data), url: url ?? entries[cursor].url };
    },
    pushState(data, _title, url) {
      entries.splice(cursor + 1);
      entries[cursor].scrollY = state.window.scrollY;
      entries.push({
        state: structuredClone(data), url: url ?? entries[cursor].url,
        restoration: entries[cursor].restoration, scrollY: state.window.scrollY,
      });
      cursor++;
    },
  };
  load(state, names.map(sliceFunction).join('\n'), { expose: {
    jump: 'goToDestination', capture: 'captureLinkHistoryLocation', pop: 'handleLinkHistoryPopState',
    reset: 'resetLinkNavigationHistory', normalPage: 'goTo', restore: 'restoreLinkHistoryLocation',
    handleLinkHistoryPopState: 'handleLinkHistoryPopState',
    nativeMove: 'navigateDesktopLinkHistory', mouse: 'handleDesktopHistoryMouse', key: 'handleDesktopHistoryKey',
  } });
  const popListener = bind(state, sliceListenerHandler("window.addEventListener('popstate'", 'popstate'));
  const move = delta => {
    const target = cursor + delta;
    if (target < 0 || target >= entries.length) return;
    cursor = target;
    popListener({ state: entries[cursor].state });
    // 浏览器在 popstate 之后执行该历史条目声明的原生滚动恢复。
    if (entries[cursor].restoration === 'auto') state.window.scrollY = entries[cursor].scrollY;
  };
  return { state, entries, move, callbacks, cursor: () => cursor };
}
