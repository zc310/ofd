class OFDWorkerClient {
  constructor() {
    this.worker = new Worker('worker.js');
    this.nextID = 1;
    this.pending = new Map();
    this.ready = new Promise((resolve, reject) => {
      this.resolveReady = resolve;
      this.rejectReady = reject;
    });
    this.worker.onmessage = event => this.handleMessage(event.data || {});
    this.worker.onerror = error => {
      console.error('[OFD] Worker 异常', {
        message: error.message,
        filename: error.filename,
        lineno: error.lineno,
        colno: error.colno,
        error: error.error,
      });
      this.fail(error.message || 'Worker 运行失败');
    };
  }

  handleMessage(message) {
    if (message.type === 'ready') {
      this.resolveReady();
      return;
    }
    if (message.type === 'fatal') {
      this.fail(message.error);
      return;
    }
    if (message.type === 'stream-chunk') {
      const request = this.pending.get(message.id);
      if (request?.onChunk) request.onChunk(message.value, message.sequence, message.id);
      return;
    }
    const request = this.pending.get(message.id);
    if (!request) return;
    this.pending.delete(message.id);
    if (message.ok) request.resolve(message.value);
    else {
      const error = new Error(message.error);
      if (message.error === '请求已取消') error.name = 'AbortError';
      request.reject(error);
    }
  }

  fail(message) {
    const error = new Error(message || 'Worker 运行失败');
    this.rejectReady(error);
    for (const request of this.pending.values()) request.reject(error);
    this.pending.clear();
  }

  request(command, payload = {}, transfer = [], onChunk) {
    let id = 0;
    let cancelled = false;
    let rejectRequest;
    const promise = new Promise((resolve, reject) => {
      rejectRequest = reject;
      this.ready.then(() => {
        if (cancelled) {
          const error = new Error('请求已取消');
          error.name = 'AbortError';
          reject(error);
          return;
        }
        id = this.nextID++;
        this.pending.set(id, { resolve, reject, onChunk });
        this.worker.postMessage({ id, command, ...payload }, transfer);
      }).catch(reject);
    });
    promise.cancel = () => {
      if (cancelled) return;
      cancelled = true;
      const request = id && this.pending.get(id);
      if (request) {
        this.pending.delete(id);
        this.worker.postMessage({ command: 'cancel', target: id });
        const error = new Error('请求已取消');
        error.name = 'AbortError';
        request.reject(error);
      } else if (!id) {
        const error = new Error('请求已取消');
        error.name = 'AbortError';
        rejectRequest(error);
      }
    };
    return promise;
  }

  open(data, options = {}) {
    const documentData = data instanceof ArrayBuffer
      ? data
      : data.buffer.slice(data.byteOffset, data.byteOffset + data.byteLength);
    const transfer = [documentData];
    return this.request('open', {
      data: documentData,
      options,
    }, transfer);
  }

  addFallbackFont(data, family, weight = 400, italic = false) {
    const buffer = data instanceof ArrayBuffer
      ? data.slice(0)
      : data.buffer.slice(data.byteOffset, data.byteOffset + data.byteLength);
    return this.request('addFallbackFont', {
      data: buffer, family, weight, italic,
    }, [buffer]);
  }

  close() {
    return this.request('close');
  }

  info() {
    return this.request('info');
  }

  outline() {
    return this.request('outline');
  }

  preferences() {
    return this.request('preferences');
  }

  fontUsage(scope, fontID, options = {}) {
    return this.request('fontUsage', { scope, fontID, maxScan: options.maxScan, maxPages: options.maxPages });
  }

  fontUsageAll(options = {}) {
    return this.request('fontUsageAll', { maxScan: options.maxScan, maxPages: options.maxPages });
  }

  attachments() {
    return this.request('attachments');
  }

  attachmentData(scope, attachmentID, maxBytes) {
    return this.request('attachmentData', { scope, attachmentID, maxBytes });
  }

  media() {
    return this.request('media');
  }

  mediaData(scope, mediaID, maxBytes) {
    return this.request('mediaData', { scope, mediaID, maxBytes });
  }

  annotations() {
    return this.request('annotations');
  }

  signatures() {
    return this.request('signatures');
  }

  signatureSeal(scope, signatureID, stampIndex) {
    return this.request('signatureSeal', { scope, signatureID, stampIndex });
  }

  signatureCertificate(scope, signatureID, slot) {
    return this.request('signatureCertificate', { scope, signatureID, slot });
  }

  signatureValue(scope, signatureID) {
    return this.request('signatureValue', { scope, signatureID });
  }

  stats() {
    return this.request('stats');
  }

  memStats() {
    return this.request('memStats');
  }

  renderPage(index, options) {
    return this.request('renderPage', { index, options });
  }

  pageInfo(index) {
    return this.request('pageInfo', { index });
  }

  renderPages(indices, options) {
    return this.request('renderPages', { indices, options });
  }

  renderStream(indices, options, onChunk) {
    const payload = { indices, options };
    return this.request('renderStream', payload, [], onChunk);
  }

  streamAck(id, sequence, error) {
    this.worker.postMessage({
      command: 'streamAck',
      target: id,
      sequence,
      error: error ? String(error.message || error) : null,
    });
  }

  text(index) {
    return this.request('text', { index });
  }

  search(query) {
    return this.request('search', { query });
  }
}

window.addEventListener('error', event => {
  console.error('[OFD] JavaScript 异常', {
    message: event.message,
    filename: event.filename,
    lineno: event.lineno,
    colno: event.colno,
    error: event.error,
  });
});

window.addEventListener('unhandledrejection', event => {
  console.error('[OFD] 未处理的 Promise 异常', event.reason);
});

class BlobURLCache {
  constructor(maxBytes, isURLInUse = () => false) {
    this.maxBytes = maxBytes;
    this.isURLInUse = isURLInUse;
    this.bytes = 0;
    this.values = new Map();
  }

  get(key) {
    const entry = this.values.get(key);
    if (entry === undefined) return undefined;
    this.values.delete(key);
    this.values.set(key, entry);
    return entry.url;
  }

  set(key, url, size) {
    const old = this.values.get(key);
    if (old) {
      this.bytes -= old.size;
      URL.revokeObjectURL(old.url);
    }
    this.values.delete(key);
    this.values.set(key, { url, size });
    this.bytes += size;
    // 即使最新项目超过缓存预算也保留它，避免刚完成渲染的图片被分配到
    // 已经撤销的 URL。
    while (this.values.size > 1 && this.bytes > this.maxBytes) {
      const oldest = Array.from(this.values.keys()).find(key => {
        const entry = this.values.get(key);
        return !this.isURLInUse(entry.url);
      });
      if (oldest === undefined) break;
      const entry = this.values.get(oldest);
      this.bytes -= entry.size;
      URL.revokeObjectURL(entry.url);
      this.values.delete(oldest);
    }
    return url;
  }

  delete(key) {
    const entry = this.values.get(key);
    if (!entry) return;
    this.bytes -= entry.size;
    URL.revokeObjectURL(entry.url);
    this.values.delete(key);
  }

  clear() {
    for (const entry of this.values.values()) URL.revokeObjectURL(entry.url);
    this.values.clear();
    this.bytes = 0;
  }

  clearIndex(kind, index) {
    const keysToDelete = [];
    for (const [key] of this.values.entries()) {
      const parts = key.split(':');
      if (parts.length >= 3 && parts[1] === kind && parts[2] === String(index)) {
        keysToDelete.push(key);
      }
    }
    for (const key of keysToDelete) {
      const entry = this.values.get(key);
      if (!entry) continue;
      this.bytes -= entry.size;
      URL.revokeObjectURL(entry.url);
      this.values.delete(key);
    }
  }
}

const status = document.querySelector('#status');
const statusMessage = document.querySelector('#status-message');
const cancelAction = document.querySelector('#cancel-action');
const renderProgress = document.querySelector('#render-progress');
const renderProgressLabel = document.querySelector('#render-progress-label');
const startupScreen = document.querySelector('#startup-screen');
const startupMessage = document.querySelector('#startup-message');
const startupProgress = document.querySelector('#startup-progress');
const startupProgressLabel = document.querySelector('#startup-progress-label');
const file = document.querySelector('#file');
const documentName = document.querySelector('#document-name');
const cancelOpen = document.querySelector('#cancel-open');
const recentToggle = document.querySelector('#recent-toggle');
const recentPanel = document.querySelector('#recent-panel');
const recentList = document.querySelector('#recent-list');
const recentEmpty = document.querySelector('#recent-empty');
const recentClear = document.querySelector('#recent-clear');
const pagesElement = document.querySelector('#pages');
const backToTop = document.querySelector('#back-to-top');
const thumbnailsElement = document.querySelector('#thumbnails');
const readerElement = document.querySelector('.reader');
const empty = document.querySelector('#empty');
const dropHint = document.querySelector('#drop-hint');
const pageNumber = document.querySelector('#page-number');
const pageCount = document.querySelector('#page-count');
const previous = document.querySelector('#pill-previous');
const next = document.querySelector('#pill-next');
const documentMenuToggle = document.querySelector('#document-menu-toggle');
const documentMenu = document.querySelector('#document-menu');
const printPage = document.querySelector('#print-page');
const exportDocument = document.querySelector('#export-document');
const exportDialog = document.querySelector('#export-dialog');
const exportForm = document.querySelector('#export-form');
const exportCancel = document.querySelector('#export-cancel');
const exportRange = document.querySelector('#export-range');
const exportCustomRange = document.querySelector('#export-custom-range');
const exportDPI = document.querySelector('#export-dpi');
const exportFormat = document.querySelector('#export-format');
const exportBackground = document.querySelector('#export-background');
const exportBackgroundField = document.querySelector('#export-background-field');
const exportError = document.querySelector('#export-error');
const printDialog = document.querySelector('#print-dialog');
const printForm = document.querySelector('#print-form');
const printCancel = document.querySelector('#print-cancel');
const printCurrentLabel = document.querySelector('#print-current-label');
const printCustomRange = document.querySelector('#print-range-custom');
const printError = document.querySelector('#print-error');
const copyPageText = document.querySelector('#copy-page-text');
const copyAllTextButton = document.querySelector('#copy-all-text');
const zoomOut = document.querySelector('#zoom-out');
const zoomIn = document.querySelector('#zoom-in');
const zoomMenuToggle = document.querySelector('#zoom-menu-toggle');
const zoomMenu = document.querySelector('#zoom-menu');
const rotatePageButton = document.querySelector('#rotate-page');
const readingMode = document.querySelector('#reading-mode');
const zoomLabel = document.querySelector('#zoom-label');
const searchInput = document.querySelector('#search');
const searchToggle = document.querySelector('#search-toggle');
const searchPanel = document.querySelector('#search-panel');
const mobileToolbarToggle = document.querySelector('#mobile-toolbar-toggle');
const searchButton = document.querySelector('#search-button');
const searchPrevious = document.querySelector('#search-previous');
const searchNext = document.querySelector('#search-next');
const searchStatus = document.querySelector('#search-status');
const viewToggle = document.querySelector('#view-toggle');
const viewPanel = document.querySelector('#view-panel');
const showThumbnails = document.querySelector('#show-thumbnails');
const showTextLayer = document.querySelector('#show-text-layer');
const showPagePill = document.querySelector('#show-page-pill');
const pagePill = document.querySelector('#page-pill');
const pillHide = document.querySelector('#pill-hide');
const darkReading = document.querySelector('#dark-reading');
const clarityPrioritySelect = document.querySelector('#clarity-priority');
const renderFormatSelect = document.querySelector('#render-format');
const documentBackground = document.querySelector('#document-background');
const documentBackgroundColorPicker = document.querySelector('#document-background-color');
const pageLayoutSelect = document.querySelector('#page-layout');
const aboutLink = document.querySelector('#about-link');
const aboutDialog = document.querySelector('#about-dialog');
const aboutClose = document.querySelector('#about-close');
const aboutTitle = document.querySelector('#about-title');
const infoToggle = document.querySelector('#info-toggle');
const infoPanel = document.querySelector('#info-panel');
const infoClose = document.querySelector('#info-close');
const infoBody = document.querySelector('#info-body');
const sidebarElement = document.querySelector('#sidebar');
const sidebarTabsElement = document.querySelector('#sidebar-tabs');
const sidebarFilter = document.querySelector('#sidebar-filter');
const sidebarResizer = document.querySelector('#sidebar-resizer');
const sidebarTabThumbnails = document.querySelector('#sidebar-tab-thumbnails');
const sidebarTabOutline = document.querySelector('#sidebar-tab-outline');
const sidebarTabBookmarks = document.querySelector('#sidebar-tab-bookmarks');
const sidebarTabMore = document.querySelector('#sidebar-tab-more');
const sidebarTabMoreLabel = document.querySelector('#sidebar-tab-more-label');
const sidebarMoreMenu = document.querySelector('#sidebar-more-menu');
const sidebarMoreFonts = document.querySelector('#sidebar-more-fonts');
const sidebarMoreAttachments = document.querySelector('#sidebar-more-attachments');
const sidebarMoreMedia = document.querySelector('#sidebar-more-media');
const sidebarMoreAnnotations = document.querySelector('#sidebar-more-annotations');
const sidebarMoreSignatures = document.querySelector('#sidebar-more-signatures');
const attachmentsElement = document.querySelector('#attachments');
const mediaElement = document.querySelector('#media');
const annotationsElement = document.querySelector('#annotations');
const signaturesElement = document.querySelector('#signatures');
const thumbnailToolbar = document.querySelector('#thumbnail-toolbar');
const thumbnailSizeSlider = document.querySelector('#thumbnail-size-slider');
const outlineElement = document.querySelector('#outline');
const bookmarksElement = document.querySelector('#bookmarks');
const fontsElement = document.querySelector('#fonts');
const outlineToolbar = document.querySelector('#outline-toolbar');
const outlineExpandAll = document.querySelector('#outline-expand-all');
const outlineCollapseAll = document.querySelector('#outline-collapse-all');
const engine = new OFDWorkerClient();
const pageCache = new BlobURLCache(128 << 20, url =>
  Array.from(document.querySelectorAll('.page-image')).some(image => !image.hidden && image.src === url));
const thumbnailCache = new BlobURLCache(32 << 20, url =>
  Array.from(document.querySelectorAll('.thumbnail img')).some(image => !image.hidden && image.src === url));
// 打开文档时限制 WASM 侧解析器保留的页面缓存，避免滚动浏览大文档时
// 已解析页面持续驻留。pageCacheBytes 为 0 时由 WASM 使用默认值。
const openDocumentOptions = {
  pageCacheCapacity: 6,
  pageCacheBytes: 64 << 20,
};
const fallbackFontURLs = [
  {
    family: 'Smiley Sans',
    url: 'https://cdn.jsdelivr.net/gh/deepin-community/fonts-smiley-sans@master/SmileySans-Oblique.ttf.woff2',
    alternateURL: 'https://raw.githubusercontent.com/deepin-community/fonts-smiley-sans/master/SmileySans-Oblique.ttf.woff2',
    weight: 400,
  },
];
const fallbackFontCacheName = 'ofd-fonts';
const fallbackFontTimeout = 45_000;
const fallbackFontLoads = new Map();
const fallbackFontData = new Map();
let fallbackFontRegistration;
const transparentRenderBackground = '#00000000';
const recentDatabaseName = 'ofd-reader';
const recentStoreName = 'files';
const recentFileLimit = 5;
const recentFileMaxBytes = 64 << 20;
const readingPositionStorageKey = 'ofd-reading-positions';
const pageRequests = new Map();
const pageCardRequests = new Map();
const thumbnailRequests = new Map();
const thumbnailBatchQueue = new Map();
let thumbnailBatchTimer;
const textRequests = new Map();
const textCache = new Map();
const textCacheMaxEntries = 64;
let pageInfos = [];

function trimTextCache() {
  while (textCache.size > textCacheMaxEntries) {
    const oldestKey = textCache.keys().next().value;
    textCache.delete(oldestKey);
  }
}
let pageCards = [];
let thumbnailButtons = [];
let current = 0;
let documentGeneration = 0;
let injectedFonts = new Map();
let resizeObserver;
let searchResults = [];
let activeSearchResult = -1;
let searchGeneration = 0;
let searchRequest;
let openRequest;
let opening = false;
let zoom = 1;
const zoomModeStorageKey = 'ofd-zoom-mode';
let zoomMode = (() => {
  try {
    const value = localStorage.getItem(zoomModeStorageKey);
    return ['fit', 'page'].includes(value) ? value : 'fit';
  } catch (_) {
    return 'fit';
  }
})();
const pageLayoutStorageKey = 'ofd-page-layout';
let pageLayout = (() => {
  try {
    const value = localStorage.getItem(pageLayoutStorageKey);
    return ['single', 'double', 'double-odd-left'].includes(value) ? value : 'single';
  } catch (_) {
    return 'single';
  }
})();
let zoomGeneration = 0;
const thumbnailsStorageKey = 'ofd-show-thumbnails';
const renderFormatStorageKey = 'ofd-render-format';
const thumbnailRenderFormat = 'png';
const clarityPriorityStorageKey = 'ofd-clarity-priority';
let thumbnailsVisible = (() => {
  try {
    return localStorage.getItem(thumbnailsStorageKey) !== 'false';
  } catch (_) {
    return true;
  }
})();
const sidebarTabStorageKey = 'ofd-sidebar-tab';
let activeSidebarTab = (() => {
  try {
    const value = localStorage.getItem(sidebarTabStorageKey);
    return ['thumbnails', 'outline', 'bookmarks', 'fonts'].includes(value) ? value : 'thumbnails';
  } catch (_) {
    return 'thumbnails';
  }
})();
let outlineNodes = [];
let bookmarkNodes = [];
let sidebarFilterValue = '';
const thumbnailSizeStorageKey = 'ofd-thumbnail-size';
// 缩略图尺寸以可用宽度的百分比表示，允许 40%–100%。兼容旧版 small/medium/large 档位。
const thumbnailSizeLegacy = { small: 55, medium: 75, large: 100 };
let thumbnailSizePercent = (() => {
  try {
    const raw = localStorage.getItem(thumbnailSizeStorageKey);
    if (raw == null) return 100;
    const value = Object.prototype.hasOwnProperty.call(thumbnailSizeLegacy, raw) ? thumbnailSizeLegacy[raw] : Number(raw);
    return Number.isFinite(value) ? Math.max(40, Math.min(100, Math.round(value))) : 100;
  } catch (_) {
    return 100;
  }
})();
const outlineExpandStorageKey = 'ofd-outline-expanded';
let outlineExpandState = {};
let documentInfo = null;
let documentInfoGeneration = -1;
let documentFontUsage = null;
let documentFontUsageGeneration = -1;
let documentFontUsageMeta = { scanned: 0, truncated: false };
let mediaObserver = null;
let mediaObjectURLs = [];
const sidebarScrollStorageKey = 'ofd-sidebar-scroll';
let sidebarScroll = {};
let sidebarScrollPersistTimer;
const sidebarScrollPanels = {};
const sidebarWidthStorageKey = 'ofd-sidebar-width';
let sidebarWidth = (() => {
  try {
    const stored = localStorage.getItem(sidebarWidthStorageKey);
    if (stored == null) return 210;
    const value = Number(stored);
    return Number.isFinite(value) ? Math.max(140, Math.min(520, value)) : 210;
  } catch (_) {
    return 210;
  }
})();
let renderFormat = (() => {
  try {
    const value = localStorage.getItem(renderFormatStorageKey);
    return ['png', 'jpg', 'svg'].includes(value) ? value : 'png';
  } catch (_) {
    return 'png';
  }
})();
let clarityPriority = (() => {
  try {
    return localStorage.getItem(clarityPriorityStorageKey) === 'true';
  } catch (_) {
    return false;
  }
})();
let textLayerVisible = true;
const pagePillStorageKey = 'ofd-show-page-pill';
let pagePillVisible = (() => {
  try {
    return localStorage.getItem(pagePillStorageKey) !== 'false';
  } catch (_) {
    return true;
  }
})();
let darkReadingVisible = false;
const documentBackgroundModeStorageKey = 'ofd-document-background-mode';
const documentBackgroundColorStorageKey = 'ofd-document-background-color';
const documentBackgroundThemes = {
  'adw-dracula': '#282a36',
  'adw-everforest': '#f3f1e5',
  'adw-gruvbox': '#282828',
  'adw-nord': '#2e3440',
  'adw-solarized': '#fdf6e3',
  'Peninsula-dark': '#20252b',
  Plano2: '#eef2f5',
};
let documentBackgroundMode = 'white';
let documentBackgroundCustomColor = '#ffffff';
let pageRotation = 0;
let touchStartX = 0;
let touchStartY = 0;
let touchStartDistance = 0;
let touchPinching = false;
let touchZoomTarget = 0;
let touchZoomTimer;
let documentActionBusy = false;
let documentActionCancelRequested = false;
let exportRequest;
let exportActive = false;
let startupProgressActive = true;
let startupWasmReady = false;
let startupFontReady = false;
let startupFontError;
let renderedPages = new Set();
let failedPages = new Set();
let copyFeedbackTimer;
let statusBeforeCopy;
let recentFiles = [];
let currentDocumentKey = '';
let pageSpreads = [];
let pageVirtualTrack;
let pageVirtualWindow;
let pageAnchor = 0;
let pageVirtualTranslate = 0;
let pageVirtualTranslateFrame;
let pageTrackContentHeight = 0;
let pageTrackScrollHeight = 0;
let pageTrackScale = 1;
let pageTrackMaxHeight = 0;
let thumbnailVirtualTrack;
let thumbnailVirtualWindow;
let thumbnailAnchor = 0;
let thumbnailVirtualTranslate = 0;
let thumbnailTrackScrollHeight = 0;
let thumbnailTrackScale = 1;
let thumbnailSlots = [];
let thumbnailSlotByPage = [];
let pageLoadToken = 0;
const pageInfoRequests = new Map();
let pageVirtualUpdateTimer;
let thumbnailVirtualUpdateFrame;
let thumbnailFollowTimer;
let thumbnailMetrics = {
  mobile: false,
  columns: 1,
  gap: 10,
  itemWidth: 72,
  rowHeights: [],
  rowOffsets: [],
  maxHeight: 102,
};

if (typeof window.matchMedia !== 'function') {
  window.matchMedia = query => {
    const media = String(query);
    return {
      matches: false,
      media,
      onchange: null,
      addListener() {},
      removeListener() {},
      addEventListener() {},
      removeEventListener() {},
      dispatchEvent() { return false; },
    };
  };
}

if (typeof window.requestAnimationFrame !== 'function') {
  window.requestAnimationFrame = callback => setTimeout(() => callback(Date.now()), 16);
  window.cancelAnimationFrame = id => clearTimeout(id);
}

function createResizeObserver(callback) {
  if (typeof ResizeObserver === 'function') return new ResizeObserver(callback);

  const observed = new Set();
  let timer;
  const notify = () => {
    timer = undefined;
    if (!observed.size) return;
    callback([...observed].map(target => ({ target })));
  };
  const schedule = () => {
    if (timer !== undefined) return;
    timer = setTimeout(notify, 0);
  };
  const onResize = schedule;
  window.addEventListener('resize', onResize);
  return {
    observe(element) {
      observed.add(element);
      schedule();
    },
    unobserve(element) {
      observed.delete(element);
    },
    disconnect() {
      observed.clear();
      clearTimeout(timer);
      timer = undefined;
      window.removeEventListener('resize', onResize);
    },
  };
}

function pageLayoutIsDouble() {
  return pageLayout !== 'single';
}

function updateThumbnailLayout() {
  document.body.classList.toggle('double-thumbnail-layout', pageLayoutIsDouble());
}

function pageSpreadGroups() {
  if (pageLayout === 'single') return pageInfos.map((_, index) => [index]);
  const groups = [];
  let index = pageLayout === 'double' ? -1 : 0;
  if (index === -1) groups.push([-1, 0]);
  else if (pageInfos.length) groups.push([0, 1 < pageInfos.length ? 1 : -1]);
  index = pageLayout === 'double' ? 1 : 2;
  for (; index < pageInfos.length; index += 2) groups.push([index, index + 1 < pageInfos.length ? index + 1 : -1]);
  return groups;
}

function spreadPositionForPage(index) {
  return pageSpreads.findIndex(spread => spread.pages.includes(index));
}

function firstPageInSpread(position) {
  return pageSpreads[position]?.pages.find(index => index >= 0) ?? -1;
}

function currentSpreadPosition() {
  const position = spreadPositionForPage(current);
  return position >= 0 ? position : 0;
}

function moveToSpread(delta) {
  if (!pageInfos.length) return;
  const position = currentSpreadPosition() + delta;
  const target = firstPageInSpread(Math.max(0, Math.min(pageSpreads.length - 1, position)));
  if (target >= 0) goTo(target);
}

function layoutBaseWidth() {
  return pageLayoutIsDouble() ? 2 * 820 + 18 : 820;
}

function spreadDimensions(position) {
  const pages = pageSpreads[position]?.pages || [];
  const heights = pages
    .filter(index => index >= 0 && pageInfos[index])
    .map(index => {
      const info = pageInfos[index];
      const ratio = pageRotation % 180 === 0 ? info.width / info.height : info.height / info.width;
      return 820 / ratio;
    });
  return {
    width: pageLayoutIsDouble() ? layoutBaseWidth() : 820,
    height: Math.max(...heights, 180),
  };
}

function pageSpreadOffset(position) {
  return pageSpreads[position]?.offset || 0;
}

// 浏览器对单个元素和文档的布局高度存在上限（Chromium 约 2^25 px，
// Firefox/Safari 也有各自的限制）。数百万页的大文档使轨道高度远超该上限时，
// 浏览器会截断可滚动高度，右键滚动条无法到达文档末尾。
// 这里用二分法探测当前浏览器实际允许的最大布局高度，一次探测即可缓存复用。
function detectMaxLayoutHeight() {
  const probe = document.createElement('div');
  probe.style.cssText = 'position:absolute;left:-99999px;top:0;visibility:hidden;pointer-events:none;';
  probe.style.height = '1px';
  document.body.append(probe);
  let low = 1;
  let high = 512 * 1024 * 1024;
  let result = 1;
  while (low <= high) {
    const mid = Math.floor((low + high) / 2);
    probe.style.height = `${mid}px`;
    void probe.offsetHeight;
    if (probe.getBoundingClientRect().height >= mid) {
      result = mid;
      low = mid + 1;
    } else {
      high = mid - 1;
    }
  }
  probe.remove();
  return result;
}

// 轨道高度上限的保守值。除了要低于浏览器元素布局坐标上限，还必须明显低于
// Firefox 对超长文档 position: sticky 的处理能力：文档接近其布局上限
// （约 1.8e7 px）时，sticky 的左侧缩略图面板会被错误定位、随文档滚出视口，
// 表现为整个缩略图区域空白。实测压缩到 6e6 以内 sticky 正常。
const trackSafeMaxHeight = 6_000_000;

function pageTrackMaxLayoutHeight() {
  // header、移动端展开的工具栏、页面内边距等也占用文档滚动高度，
  // 预留空间避免整个文档高度略微超出限制而被截断。
  if (pageTrackMaxHeight <= 0) {
    const detected = detectMaxLayoutHeight() - Math.round(headerHeight()) - 128;
    pageTrackMaxHeight = Math.min(Math.max(detected, 1_000_000), trackSafeMaxHeight);
  }
  return pageTrackMaxHeight;
}

// 轨道高度超过浏览器限制时，把轨道压缩到允许的高度。页面内容本身仍按
// 原始尺寸和间距排版，通过 pageVirtualWindow 整体平移来对齐当前视口。
//
// 页面绝对定位的 top 和窗口的 translate 都受浏览器布局坐标限制（约 2^25
// px），因此不能直接使用等于内容坐标的 top 或一次性的巨大平移。这里维护一个
// 锚点 pageAnchor：页面按「内容偏移 - pageAnchor」定位（范围小、不受限制），
// 窗口平移量 = scrollTop - 内容偏移 + pageAnchor，随滚动逐帧更新。当锚点与
// 当前内容偏移差得足够远时重新锚定，保证两个数值始终落在浏览器允许范围内。
// 任意锚点下页面最终位置都是 scrollTop - 内容偏移 + 内容偏移量，与锚点无关，
// 因此重新锚定时视觉上不会跳变。scale 为 1 时未压缩，所有换算保持原样。
//
// 漂移上限必须明显小于浏览器布局坐标上限：页面的 top = 内容偏移 - 锚点，
// 漂移越接近上限，越可能被浏览器截断（Firefox 实测上限约 8.9e6 px），从而
// 出现错位甚至整片空白。这里取「探测上限的一半」与原 8M 中的较小值。
function pageAnchorResetDrift() {
  return Math.min(8 * 1024 * 1024, Math.floor(pageTrackMaxLayoutHeight() / 2));
}

function pageTrackScrollFromContent(contentPx) {
  return pageTrackScale > 1 ? contentPx / pageTrackScale : contentPx;
}

function pageTrackContentFromScroll(scrollPx) {
  return pageTrackScale > 1 ? scrollPx * pageTrackScale : scrollPx;
}

// 轨道内的滚动位置。文档顶部（滚动到最上时）轨道上沿可能在视口之下，
// 此时相对位置为负，但滚动条无法再往上，直接按 0 处理，避免把页面
// 整体往下偏移（轨道顶部出现多余空白）并防止滑动换算出现负数。
function pageTrackScrollY() {
  if (!pageVirtualTrack) return 0;
  const viewportTop = pageVirtualTrack.getBoundingClientRect().top;
  return Math.max(0, -viewportTop);
}

function updatePageVirtualTranslate() {
  if (!pageVirtualWindow || !pageVirtualTrack) return;
  const scrollTop = pageTrackScrollY();
  const content = pageTrackContentFromScroll(scrollTop);
  if (Math.abs(content - pageAnchor) > pageAnchorResetDrift()) {
    pageAnchor = content;
    pageSpreads.forEach(spread => {
      if (spread.element) spread.element.style.top = `${spread.offset - pageAnchor}px`;
    });
  }
  const translate = scrollTop - content + pageAnchor;
  if (translate === pageVirtualTranslate) return;
  pageVirtualTranslate = translate;
  pageVirtualWindow.style.transform = `translate3d(0, ${translate}px, 0)`;
}

function schedulePageVirtualTranslate() {
  if (pageVirtualTranslateFrame) return;
  pageVirtualTranslateFrame = requestAnimationFrame(() => {
    pageVirtualTranslateFrame = undefined;
    updatePageVirtualTranslate();
  });
}

function updatePageVirtualMetrics() {
  if (!pageVirtualTrack) return;
  const maxHeight = pageTrackMaxLayoutHeight();
  const gap = 18 * zoom;
  let contentOffset = 0;
  pageSpreads.forEach((spread, position) => {
    const dimensions = spreadDimensions(position);
    spread.offset = contentOffset;
    spread.height = dimensions.height * zoom;
    spread.width = dimensions.width * zoom;
    if (spread.element) {
      spread.element.style.top = `${contentOffset - pageAnchor}px`;
      spread.element.style.width = `${spread.width}px`;
      spread.element.style.minHeight = `${spread.height}px`;
      spread.element.style.gap = `${gap}px`;
    }
    contentOffset += spread.height + gap;
  });
  pageTrackContentHeight = Math.max(0, contentOffset - gap);
  /* 内容超过浏览器布局高度上限时压缩轨道。压缩后滚到最底部时，视口底部必须
     正好对准内容末尾，否则最后一页会整页落在可达范围之外：滚动条到不了末页，
     点末尾缩略图跳进去也看不到最后一页。补偿 #pages 底部内边距 P 可满足要求：
       scale = (contentHeight - viewport) / (maxHeight - viewport)
       scrollHeight = contentHeight / scale
       P = maxHeight - scrollHeight
     这样「轨道 + P」恰好占满允许的最大布局高度，既不会再次触发浏览器截断，
     滚到最底部时视口底部也正好对准内容末尾，与未压缩时到达文末的表现一致。 */
  const viewportHeight = window.innerHeight;
  if (pageTrackContentHeight > maxHeight && viewportHeight < maxHeight) {
    pageTrackScale = (pageTrackContentHeight - viewportHeight) / (maxHeight - viewportHeight);
    pageTrackScrollHeight = pageTrackContentHeight / pageTrackScale;
    pagesElement.style.paddingBottom = `${Math.max(0, maxHeight - pageTrackScrollHeight)}px`;
  } else {
    pageTrackScrollHeight = Math.min(pageTrackContentHeight, maxHeight);
    pageTrackScale = pageTrackContentHeight > 0 ? pageTrackContentHeight / pageTrackScrollHeight : 1;
    pagesElement.style.paddingBottom = '';
  }
  pageVirtualTrack.style.height = `${pageTrackScrollHeight}px`;
  pageVirtualTranslate = 0;
  updatePageVirtualTranslate();
}

function pageSpreadPositionForPage(index) {
  return spreadPositionForPage(index);
}

function thumbnailSlotsForLayout() {
  if (!pageLayoutIsDouble()) return pageInfos.map((_, index) => index);
  return pageSpreads.flatMap(spread => spread.pages);
}

// 缩略图轨道与页面轨道共用同一套「超长内容压缩 + 锚点平移」策略。元素高度和
// 布局坐标都受浏览器上限约束（实测 Firefox 约 9e6 px，Chromium 约 3.4e7 px），
// 十万级页面的缩略图轨道会超过上限而被截断，Firefox 下滚到底部时缩略图区域会变空。
// 这里压缩轨道高度并补偿底部内边距，使滚动到底时对准内容末尾；未超限时保持原样。
function thumbnailScrollFromContent(contentPx) {
  return thumbnailTrackScale > 1 ? contentPx / thumbnailTrackScale : contentPx;
}

function thumbnailContentFromScroll(scrollPx) {
  return thumbnailTrackScale > 1 ? scrollPx * thumbnailTrackScale : scrollPx;
}

// 缩略图轨道相对滚动容器（#thumbnails）已滚过的距离。
function thumbnailTrackScrollY() {
  if (!thumbnailVirtualTrack) return 0;
  const viewportTop = thumbnailVirtualTrack.getBoundingClientRect().top -
    thumbnailsElement.getBoundingClientRect().top;
  return Math.max(0, -viewportTop);
}

function updateThumbnailVirtualTranslate() {
  if (!thumbnailVirtualWindow || !thumbnailVirtualTrack) return;
  const scrollTop = thumbnailTrackScrollY();
  const content = thumbnailContentFromScroll(scrollTop);
  if (Math.abs(content - thumbnailAnchor) > pageAnchorResetDrift()) {
    thumbnailAnchor = content;
    thumbnailButtons.forEach((button, index) => {
      if (button) resizeThumbnail(index, button);
    });
  }
  const translate = scrollTop - content + thumbnailAnchor;
  if (translate === thumbnailVirtualTranslate) return;
  thumbnailVirtualTranslate = translate;
  thumbnailVirtualWindow.style.transform = `translate3d(0, ${translate}px, 0)`;
}

function updateThumbnailMetrics() {
  if (!thumbnailVirtualTrack) return;
  const mobile = window.matchMedia('(max-width: 620px)').matches;
  const double = pageLayoutIsDouble();
  const columns = mobile ? 1 : double ? 2 : 1;
  const gap = mobile ? 8 : double ? 8 : 10;
  const available = Math.max(1, (thumbnailVirtualTrack.clientWidth - gap * (columns - 1)) / columns);
  const itemWidth = mobile ? 72 : Math.max(1, available * (thumbnailSizePercent / 100));
  const rows = Math.ceil(thumbnailSlots.length / columns);
  const rowHeights = Array.from({ length: rows }, (_, row) => {
    const start = row * columns;
    const end = Math.min(thumbnailSlots.length, start + columns);
    return Math.max(...thumbnailSlots.slice(start, end).map(index =>
      index >= 0 ? thumbnailHeight(index, itemWidth) : 0), 0);
  });
  const rowOffsets = [];
  let offset = 0;
  rowHeights.forEach(height => {
    rowOffsets.push(offset);
    offset += height + gap;
  });
  const maxHeight = Math.max(...rowHeights, 0);
  thumbnailMetrics = { mobile, columns, gap, itemWidth, rowHeights, rowOffsets, maxHeight };
  const contentHeight = mobile ? maxHeight : Math.max(0, offset - gap);
  const cap = pageTrackMaxLayoutHeight();
  const viewport = mobile ? thumbnailsElement.clientWidth : thumbnailsElement.clientHeight;
  if (!mobile && contentHeight > cap && viewport < cap) {
    // 与页面轨道相同的闭式解：scale 与底部内边距之和恰好占满允许高度。
    thumbnailTrackScale = (contentHeight - viewport) / (cap - viewport);
    thumbnailTrackScrollHeight = contentHeight / thumbnailTrackScale;
    thumbnailsElement.style.paddingBottom = `${Math.max(0, cap - thumbnailTrackScrollHeight)}px`;
  } else {
    thumbnailTrackScale = 1;
    thumbnailTrackScrollHeight = Math.min(contentHeight, cap);
    thumbnailsElement.style.paddingBottom = '';
  }
  thumbnailVirtualTrack.style.width = mobile ? `${thumbnailSlots.length * itemWidth + Math.max(0, thumbnailSlots.length - 1) * gap}px` : '100%';
  thumbnailVirtualTrack.style.height = `${mobile ? maxHeight : thumbnailTrackScrollHeight}px`;
  thumbnailVirtualTranslate = 0;
  // 尺寸/缩放变化会重算行偏移，必须按新的映射重新挂载可见行，否则已挂载的
  // 缩略图仍停留在旧坐标，可能出现空白。
  updateThumbnailVirtualWindow();
}

function thumbnailHeight(index, width) {
  const info = pageInfos[index];
  if (!info || info.width <= 0 || info.height <= 0) return width * 297 / 210;
  const rotated = pageRotation % 180 !== 0;
  const ratio = rotated ? info.height / info.width : info.width / info.height;
  return width / ratio;
}

function thumbnailSlotForPage(index) {
  return thumbnailSlotByPage[index] ?? -1;
}

function schedulePageVirtualUpdate() {
  clearTimeout(pageVirtualUpdateTimer);
  pageVirtualUpdateTimer = setTimeout(() => {
    pageVirtualUpdateTimer = undefined;
    updatePageVirtualWindow();
  }, 80);
}

function scheduleThumbnailVirtualUpdate() {
  if (thumbnailVirtualUpdateFrame) return;
  thumbnailVirtualUpdateFrame = requestAnimationFrame(() => {
    thumbnailVirtualUpdateFrame = undefined;
    updateThumbnailVirtualWindow();
  });
}

function scheduleThumbnailFollow() {
  clearTimeout(thumbnailFollowTimer);
  thumbnailFollowTimer = setTimeout(() => {
    thumbnailFollowTimer = undefined;
    if (!pageInfos.length || thumbnailsElement.hidden) return;
    const slot = thumbnailSlotForPage(current);
    if (slot < 0) return;
    updateThumbnailVirtualWindow(slot);
    const button = thumbnailButtons[current];
    if (button) keepThumbnailVisible(button);
  }, 160);
}

function scheduleVirtualUpdate() {
  schedulePageVirtualUpdate();
  scheduleThumbnailVirtualUpdate();
}

function mountPageSpread(position) {
  const spread = pageSpreads[position];
  if (!spread || spread.element || !pageVirtualWindow) return;
  // 打开新文档或打开失败时，延迟的虚拟列表回调可能暂时看到旧 spread。
  // 不要用新文档的 pageInfos 去挂载旧页面索引。
  if (spread.pages.some(index => index >= 0 && !pageInfos[index])) return;
  const element = document.createElement('div');
  element.className = 'page-spread';
  element.dataset.position = position;
  spread.element = element;
  pageVirtualWindow.append(element);
  spread.pages.forEach(index => {
    if (index < 0) {
      const placeholder = document.createElement('div');
      placeholder.className = 'page-placeholder';
      placeholder.dataset.index = '-1';
      const info = pageInfos[spread.pages.find(page => page >= 0)];
      if (info) {
        const ratio = pageRotation % 180 === 0 ? info.width / info.height : info.height / info.width;
        placeholder.style.aspectRatio = String(ratio);
      }
      element.append(placeholder);
      return;
    }
    const info = pageInfos[index];
    if (!info) {
      element.remove();
      spread.element = undefined;
      return;
    }
    const card = document.createElement('article');
    card.className = 'page-card loading';
    card.dataset.index = index;
    card.style.aspectRatio = `${info.width} / ${info.height}`;
    const image = document.createElement('img');
    image.className = 'page-image';
    image.alt = `第 ${index + 1} 页`;
    image.hidden = true;
    const surface = document.createElement('div');
    surface.className = 'page-surface';
    const textLayer = document.createElement('div');
    textLayer.className = 'text-layer';
    image.addEventListener('load', () => {
      image.classList.add('loaded');
      clearPageLoading(card, image);
    });
    image.addEventListener('error', () => {
      image.classList.remove('loaded');
      if (image.hidden || !image.src || card.classList.contains('render-error')) return;
      card.classList.remove('loading');
      markPageFailed(index);
      showPageError(index, '页面图片加载失败');
    });
    surface.append(image, textLayer);
    card.append(surface);
    element.append(card);
    pageCards[index] = card;
    if (textCache.has(index)) buildTextLayer(index);
    resizeObserver?.observe(card);
    loadPage(index);
  });
  applyPageWidthToSpread(spread);
  spread.pages.forEach(index => {
    if (index >= 0 && textCache.has(index)) buildTextLayer(index);
  });
}

function ensurePageMounted(index) {
  if (!pageInfos[index]) return undefined;
  const position = pageSpreadPositionForPage(index);
  if (position < 0) return undefined;
  mountPageSpread(position);
  applyPageWidthToSpread(pageSpreads[position]);
  return pageCards[index];
}

function unmountPageSpread(position) {
  const spread = pageSpreads[position];
  if (!spread?.element) return;
  spread.pages.forEach(index => {
    if (index >= 0) {
      const card = pageCards[index];
      cancelPageRequest(index, card);
      cancelTextRequest(index, card);
      textCache.delete(index);
      renderedPages.delete(index);
      failedPages.delete(index);
      if (card) pageCardRequests.delete(card);
      const pageInfoKey = `${documentGeneration}:${index}`;
      if (pageInfoRequests.has(pageInfoKey)) pageInfoRequests.delete(pageInfoKey);
      pageCache.clearIndex('page', index);
      thumbnailCache.clearIndex('thumbnail', index);
      if (card) resizeObserver?.unobserve(card);
      delete pageCards[index];
    }
  });
  spread.element.remove();
  spread.element = undefined;
}

function applyPageWidthToSpread(spread) {
  if (!spread?.element) return;
  spread.element.style.top = `${spread.offset - pageAnchor}px`;
  spread.element.style.width = `${spread.width}px`;
  spread.element.style.minHeight = `${spread.height}px`;
  spread.element.style.gap = `${18 * zoom}px`;
  spread.element.querySelectorAll('.page-placeholder').forEach(placeholder => {
    placeholder.style.width = `${820 * zoom}px`;
    placeholder.style.minHeight = `${180 * zoom}px`;
  });
  spread.pages.forEach(index => {
    if (index < 0) return;
    const card = pageCards[index];
    const info = pageInfos[index];
    if (!card || !info) return;
    const rotated = pageRotation % 180 !== 0;
    card.style.width = `${820 * zoom}px`;
    card.style.minHeight = `${180 * zoom}px`;
    card.style.aspectRatio = rotated ? `${info.height} / ${info.width}` : `${info.width} / ${info.height}`;
    const surface = card.querySelector('.page-surface');
    if (surface) {
      surface.className = 'page-surface';
      if (pageRotation === 90) surface.classList.add('rotated');
      if (pageRotation === 180) surface.classList.add('rotated-180');
      if (pageRotation === 270) surface.classList.add('rotated-270');
      surface.style.width = rotated ? `${info.width / info.height * 100}%` : '100%';
      surface.style.height = rotated ? `${info.height / info.width * 100}%` : '100%';
    }
  });
}

function updatePageVirtualWindow(updateCurrent = true) {
  if (!pageVirtualTrack || !pageSpreads.length) return;
  if (!pageInfos.length || pageSpreads.some(spread => spread.pages.some(index => index >= 0 && !pageInfos[index]))) return;
  updatePageVirtualTranslate();
  const buffer = Math.max(window.innerHeight, 800);
  const scrollTop = pageTrackScrollY();
  const viewTop = pageTrackContentFromScroll(scrollTop - buffer);
  const viewBottom = pageTrackContentFromScroll(scrollTop + window.innerHeight + buffer);
  pageSpreads.forEach((spread, position) => {
    const visible = spread.offset + spread.height >= viewTop && spread.offset <= viewBottom;
    if (visible) {
      mountPageSpread(position);
      spread.pages.forEach(index => {
        if (index < 0) return;
        const card = pageCards[index];
        const image = card?.querySelector('.page-image');
        const imageFailed = image && !image.hidden && image.complete && image.naturalWidth === 0;
        if (image && !card.classList.contains('render-error')) {
          if (!pageImageIsReady(image)) card.classList.add('loading');
          if ((image.hidden || !image.src || imageFailed) && !pageCardRequests.has(card)) {
            loadPage(index);
          }
        }
      });
    }
    else unmountPageSpread(position);
  });
  if (!updateCurrent) return;
  const viewportTop = pageTrackContentFromScroll(scrollTop);
  const viewportBottom = pageTrackContentFromScroll(scrollTop + window.innerHeight);
  const visible = pageSpreads
    .map((spread, position) => ({ spread, position }))
    .filter(({ spread }) => spread.element && spread.offset + spread.height >= viewportTop && spread.offset <= viewportBottom)
    .sort((left, right) => Math.abs(left.spread.offset - viewportTop) - Math.abs(right.spread.offset - viewportTop));
  const index = firstPageInSpread(visible[0]?.position ?? currentSpreadPosition());
  if (index >= 0 && index !== current) {
    setCurrent(index, false);
    scheduleThumbnailFollow();
  }
}

function createThumbnail(index) {
  const thumbnail = document.createElement('button');
  thumbnail.className = 'thumbnail';
  thumbnail.type = 'button';
  thumbnail.dataset.index = index;
  thumbnail.title = `第 ${index + 1} 页`;
  thumbnail.setAttribute('aria-label', `第 ${index + 1} 页`);
  thumbnail.addEventListener('click', () => goTo(index));
  thumbnail.addEventListener('keydown', event => {
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
    event.preventDefault();
    const target = event.key === 'ArrowLeft' ? index - 1 : index + 1;
    if (target >= 0 && target < pageInfos.length) {
      updateThumbnailVirtualWindow(thumbnailSlotForPage(target));
      thumbnailButtons[target]?.focus();
    }
  });
  const image = document.createElement('img');
  image.alt = `第 ${index + 1} 页缩略图`;
  image.hidden = true;
  image.addEventListener('load', () => image.classList.add('loaded'));
  image.addEventListener('error', () => image.classList.remove('loaded'));
  thumbnail.append(image);
  const label = document.createElement('span');
  label.textContent = index + 1;
  thumbnail.append(label);
  thumbnailButtons[index] = thumbnail;
  thumbnailVirtualWindow.append(thumbnail);
  resizeThumbnail(index, thumbnail);
  thumbnail.classList.toggle('active', index === current);
  if (index === current) thumbnail.setAttribute('aria-current', 'page');
  loadThumbnail(index);
}

function resizeThumbnail(index, thumbnail) {
  const slot = thumbnailSlotForPage(index);
  if (slot < 0) return;
  const { mobile, columns, gap, itemWidth, rowOffsets, maxHeight } = thumbnailMetrics;
  const width = itemWidth;
  const height = thumbnailHeight(index, width);
  if (mobile) {
    thumbnail.style.left = `${slot * (itemWidth + gap)}px`;
    thumbnail.style.top = `${(maxHeight - height) / 2}px`;
    thumbnail.style.width = `${itemWidth}px`;
    thumbnail.style.height = `${height}px`;
    thumbnail.style.minHeight = '0';
  } else {
    const column = slot % columns;
    const row = Math.floor(slot / columns);
    thumbnail.style.left = `${column * (width + gap)}px`;
    thumbnail.style.top = `${(rowOffsets[row] || 0) - thumbnailAnchor}px`;
    thumbnail.style.width = `${width}px`;
    thumbnail.style.height = `${height}px`;
    thumbnail.style.minHeight = '0';
  }
}

function thumbnailRowAtOrAfter(offsets, value) {
  let low = 0;
  let high = offsets.length;
  while (low < high) {
    const mid = (low + high) >> 1;
    if (offsets[mid] < value) low = mid + 1;
    else high = mid;
  }
  return low;
}

function updateThumbnailVirtualWindow(targetSlot = -1) {
  if (!thumbnailVirtualTrack || !thumbnailSlots.length || thumbnailsElement.hidden) return;
  updateThumbnailVirtualTranslate();
  const { mobile, columns, itemWidth, gap, rowHeights, rowOffsets } = thumbnailMetrics;
  const buffer = mobile ? thumbnailsElement.clientWidth * 2 : thumbnailsElement.clientHeight * 2;
  const cell = itemWidth + (mobile ? gap : 0);
  let start;
  let end;
  if (mobile) {
    start = Math.max(0, Math.floor((thumbnailsElement.scrollLeft - buffer) / cell));
    end = Math.min(thumbnailSlots.length, Math.ceil((thumbnailsElement.scrollLeft + thumbnailsElement.clientWidth + buffer) / cell));
  } else {
    const scrollTop = thumbnailTrackScrollY();
    const top = thumbnailContentFromScroll(scrollTop) - buffer;
    const bottom = thumbnailContentFromScroll(scrollTop + thumbnailsElement.clientHeight) + buffer;
    // rowOffsets 单调递增，二分定位可见行，避免十万级页面时线性扫描造成的卡顿。
    let startRow = thumbnailRowAtOrAfter(rowOffsets, top);
    if (startRow > 0 && rowOffsets[startRow - 1] + rowHeights[startRow - 1] >= top) startRow -= 1;
    const endRow = thumbnailRowAtOrAfter(rowOffsets, bottom);
    start = startRow * columns;
    end = Math.min(thumbnailSlots.length, endRow * columns);
  }
  const required = new Set();
  for (let slot = start; slot < end; slot += 1) required.add(slot);
  if (targetSlot >= 0) required.add(targetSlot);
  thumbnailSlots.forEach((index, slot) => {
    if (index < 0 || !required.has(slot)) return;
    if (!thumbnailButtons[index]) {
      createThumbnail(index);
    } else {
      resizeThumbnail(index, thumbnailButtons[index]);
      const image = thumbnailButtons[index].querySelector('img');
      if (image?.hidden || !image?.src) loadThumbnail(index);
    }
  });
  thumbnailSlots.forEach((index, slot) => {
    if (index < 0 || required.has(slot) || !thumbnailButtons[index]) return;
    cancelThumbnailRequest(index);
    thumbnailButtons[index].remove();
    delete thumbnailButtons[index];
  });
}

function documentKey(file) {
  return `${file.name}:${file.size}:${file.lastModified}`;
}

function readReadingPositions() {
  try {
    const value = JSON.parse(localStorage.getItem(readingPositionStorageKey) || '{}');
    return value && typeof value === 'object' ? value : {};
  } catch (_) {
    return {};
  }
}

function saveReadingPosition() {
  if (!currentDocumentKey || !pageInfos.length) return;
  try {
    const positions = readReadingPositions();
    positions[currentDocumentKey] = { page: current, updated: Date.now() };
    const entries = Object.entries(positions)
      .sort((left, right) => (right[1].updated || 0) - (left[1].updated || 0))
      .slice(0, 30);
    localStorage.setItem(readingPositionStorageKey, JSON.stringify(Object.fromEntries(entries)));
  } catch (_) {
    // 阅读位置持久化是可选功能，不能影响正常阅读。
  }
}

function restoreReadingPosition(file, pageCount) {
  const saved = readReadingPositions()[documentKey(file)];
  if (!saved || !Number.isInteger(saved.page)) return 0;
  return Math.max(0, Math.min(pageCount - 1, saved.page));
}

function openRecentDatabase() {
  if (typeof indexedDB === 'undefined') return Promise.reject(new Error('IndexedDB 不可用'));
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(recentDatabaseName, 1);
    request.onupgradeneeded = () => {
      const database = request.result;
      if (!database.objectStoreNames.contains(recentStoreName)) {
        const store = database.createObjectStore(recentStoreName, { keyPath: 'id' });
        store.createIndex('lastOpened', 'lastOpened');
      }
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error || new Error('打开最近文件存储失败'));
  });
}

function recentTransaction(mode, action) {
  return openRecentDatabase().then(database => new Promise((resolve, reject) => {
    const transaction = database.transaction(recentStoreName, mode);
    const request = action(transaction.objectStore(recentStoreName));
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error || new Error('最近文件存储操作失败'));
    transaction.onabort = () => reject(transaction.error || new Error('最近文件存储事务失败'));
    transaction.oncomplete = () => database.close();
  }));
}

async function readRecentFiles() {
  try {
    const records = await recentTransaction('readonly', store => store.getAll());
    return (records || [])
      .sort((left, right) => right.lastOpened - left.lastOpened)
      .slice(0, recentFileLimit)
      .map(({ data, ...metadata }) => metadata);
  } catch (_) {
    return [];
  }
}

async function refreshRecentFiles() {
  recentFiles = await readRecentFiles();
  recentList.replaceChildren();
  recentEmpty.hidden = recentFiles.length > 0;
  for (const record of recentFiles) {
    const item = document.createElement('button');
    item.className = 'recent-item';
    item.type = 'button';
    item.dataset.id = record.id;
    const title = document.createElement('strong');
    title.textContent = record.name;
    const detail = document.createElement('small');
    detail.textContent = `${formatFileSize(record.size)} · ${formatRecentDate(record.lastOpened)}`;
    item.append(title, detail);
    item.addEventListener('click', () => openRecentFile(record.id));
    recentList.append(item);
  }
}

function formatFileSize(size) {
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${Math.round(size / 1024)} KB`;
  return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}

// 内存诊断：打印浏览器侧各缓存与 WASM 运行时内存占用。可在控制台调用
// window.__readMemory() 手动收集当前数据。
async function reportMemory(trigger = '手动') {
  let wasm = null;
  try {
    wasm = await engine.memStats();
  } catch (_) {}
  let mountedPages = 0;
  for (const image of document.querySelectorAll('.page-image')) {
    if (!image.hidden && image.src) mountedPages++;
  }
  let fontBytes = 0;
  for (const data of fallbackFontData.values()) fontBytes += data.byteLength || 0;
  const summary = {
    '页面缓存': formatFileSize(pageCache.bytes),
    '缩略图缓存': formatFileSize(thumbnailCache.bytes),
    '文字缓存条目': textCache.size,
    '页面请求': pageRequests.size,
    '页面卡片请求': pageCardRequests.size,
    '页面信息请求': pageInfoRequests.size,
    '最近文件(驻留内存)': '按需加载',
    '回退字体(JS侧)': formatFileSize(fontBytes),
    '挂载页数(解码位图)': mountedPages,
  };
  console.group(`[OFD] 内存报告 ${trigger}`);
  console.table(summary);
  if (wasm) {
    console.log('[OFD] WASM 运行时(Go MemStats)', {
      WASM线性内存: formatFileSize(wasm.sys),
      heapSys: formatFileSize(wasm.heapSys),
      heapInuse: formatFileSize(wasm.heapInuse),
      heapAlloc: formatFileSize(wasm.heapAlloc),
      heapReleased: formatFileSize(wasm.heapReleased),
      heapObjects: wasm.heapObjects,
      累计分配: formatFileSize(wasm.totalAlloc),
      GC次数: wasm.numGC,
    });
  }
  console.groupEnd();
  return { ...summary, wasm };
}

function formatRecentDate(timestamp) {
  return new Intl.DateTimeFormat('zh-CN', { month: 'numeric', day: 'numeric', hour: 'numeric', minute: '2-digit' }).format(timestamp);
}

async function saveRecentFile(file) {
  if (file.size > recentFileMaxBytes) return;
  const record = {
    id: `${file.name}:${file.size}:${file.lastModified}`,
    name: file.name,
    size: file.size,
    lastModified: file.lastModified,
    lastOpened: Date.now(),
    // IndexedDB 可以直接持久化 Blob/File，避免为最近文件再保留一份
    // 与传给 WASM 的 ArrayBuffer 相同大小的内存副本。
    data: file,
  };
  try {
    await recentTransaction('readwrite', store => store.put(record));
    const records = await recentTransaction('readonly', store => store.getAll());
    records.sort((left, right) => right.lastOpened - left.lastOpened);
    for (const old of records.slice(recentFileLimit)) {
      await recentTransaction('readwrite', store => store.delete(old.id));
    }
    await refreshRecentFiles();
  } catch (_) {
    // 隐私浏览或存储空间已满不能影响文档查看。
  }
}

async function openRecentFile(id) {
  const record = recentFiles.find(item => item.id === id);
  if (!record) return;
  let stored;
  try {
    stored = await recentTransaction('readonly', store => store.get(id));
  } catch (_) {
    return;
  }
  if (!stored?.data) return;
  setRecentPanelOpen(false);
  const recent = typeof File === 'function'
    ? new File([stored.data], record.name, { type: 'application/ofd', lastModified: record.lastModified || record.lastOpened })
    : Object.assign(new Blob([stored.data], { type: 'application/ofd' }), { name: record.name });
  await openSelectedFile(recent);
}

async function clearRecentFiles() {
  try {
    await recentTransaction('readwrite', store => store.clear());
  } catch (_) {
    // 存储可能不可用，但内存中的列表仍然要清空。
  }
  recentFiles = [];
  recentList.replaceChildren();
  recentEmpty.hidden = false;
  setStatus('最近打开记录已清空。');
}

function setStatus(message) {
  clearTimeout(copyFeedbackTimer);
  statusBeforeCopy = undefined;
  statusMessage.classList.remove('copy-feedback');
  statusMessage.textContent = message;
}

function updateBackToTop() {
  backToTop.hidden = window.scrollY < 480;
}

function scrollToTop() {
  window.scrollTo({
    top: 0,
    behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth',
  });
}

function showCopyFeedback(message) {
  clearTimeout(copyFeedbackTimer);
  if (!statusMessage.classList.contains('copy-feedback')) statusBeforeCopy = statusMessage.textContent;
  statusMessage.textContent = message;
  statusMessage.classList.add('copy-feedback');
  copyFeedbackTimer = setTimeout(() => {
    statusMessage.classList.remove('copy-feedback');
    if (statusBeforeCopy !== undefined) statusMessage.textContent = statusBeforeCopy;
    statusBeforeCopy = undefined;
  }, 1800);
}

function resetRenderProgress() {
  renderedPages = new Set();
  failedPages = new Set();
  renderProgress.value = 0;
  renderProgressLabel.textContent = '';
  renderProgress.hidden = true;
  renderProgressLabel.hidden = true;
}

function updateStartupProgress() {
  if (!startupProgressActive) return;
  const completed = Number(startupWasmReady) + Number(startupFontReady);
  startupProgress.value = completed;
  if (startupFontError) {
    startupProgressLabel.textContent = '[2/2] 默认中文字体加载失败';
    startupMessage.textContent = `默认中文字体加载失败：${startupFontError.message}`;
    setStatus(`默认中文字体加载失败：${startupFontError.message}`);
    return;
  }
  if (!startupWasmReady) {
    startupProgressLabel.textContent = '[1/2] 加载 WASM 模块';
    startupMessage.textContent = '正在加载 WASM 模块...';
    setStatus('正在加载 WASM 模块...');
    return;
  }
  if (!startupFontReady) {
    startupProgressLabel.textContent = '[2/2] 加载默认中文字体';
    startupMessage.textContent = '正在加载默认中文字体...';
    setStatus('正在加载默认中文字体...');
    return;
  }
  startupProgressLabel.textContent = '[2/2] 已准备就绪';
  startupMessage.textContent = '已准备就绪，请选择 OFD 文件。';
  setStatus('已准备就绪，请选择 OFD 文件。');
  startupScreen.hidden = true;
}

function updateRenderProgress() {
  const total = pageInfos.length;
  if (exportActive) return;
  if (!total) {
    renderProgress.hidden = true;
    renderProgressLabel.hidden = true;
    return;
  }
  renderProgress.max = 1;
  const loaded = renderedPages.size;
  const failed = failedPages.size;
  renderProgress.value = loaded / total;
  renderProgressLabel.textContent = failed
    ? `已加载 ${loaded}/${total}，失败 ${failed}`
    : `已加载 ${loaded}/${total}`;
  renderProgress.hidden = false;
  renderProgressLabel.hidden = false;
}

function markPageLoaded(index) {
  if (index < 0 || index >= pageInfos.length) return;
  renderedPages.add(index);
  failedPages.delete(index);
  updateRenderProgress();
}

function markPageFailed(index) {
  if (index < 0 || index >= pageInfos.length || renderedPages.has(index)) return;
  failedPages.add(index);
  updateRenderProgress();
}

function showPageError(index, message) {
  const card = ensurePageMounted(index);
  if (!card) return;
  card.classList.add('render-error');
  const error = document.createElement('div');
  error.className = 'page-error';
  const title = document.createElement('strong');
  title.textContent = `第 ${index + 1} 页加载失败`;
  const detail = document.createElement('small');
  detail.textContent = message;
  detail.title = message;
  const retry = document.createElement('button');
  retry.type = 'button';
  retry.textContent = '重新渲染';
  retry.addEventListener('click', () => retryPage(index));
  error.append(title, detail, retry);
  card.append(error);
}

function retryPage(index) {
  const card = ensurePageMounted(index);
  if (!card || index < 0 || index >= pageInfos.length) return;
  pageCache.delete(cacheKey('page', index, documentGeneration, pageDPI(), renderFormat));
  failedPages.delete(index);
  card.classList.remove('render-error');
  card.querySelector('.page-error')?.remove();
  const image = card.querySelector('.page-image');
  image.hidden = true;
  card.classList.add('loading');
  loadPage(index);
}

function showThumbnailError(entry, message) {
  entry.button.classList.add('render-error');
  entry.button.querySelector('.thumbnail-error')?.remove();
  const error = document.createElement('small');
  error.className = 'thumbnail-error';
  error.textContent = '加载失败，点击重试';
  error.title = message;
  error.addEventListener('click', event => {
    event.stopPropagation();
    entry.button.classList.remove('render-error');
    error.remove();
    loadThumbnail(entry.index);
  });
  entry.button.append(error);
}

function isCancelledError(error) {
  return error && error.name === 'AbortError';
}

function clearInjectedFonts() {
  for (const [key, face] of injectedFonts.entries()) {
    if (fallbackFontURLs.some(source => key.startsWith(`${source.family}:`))) continue;
    if (!document.fonts) {
      injectedFonts.delete(key);
      continue;
    }
    document.fonts.delete(face);
    injectedFonts.delete(key);
  }
}

async function injectFonts(fonts, generation) {
  const loaded = new Map();
  if (typeof FontFace !== 'function' || !document.fonts) return 0;
  await Promise.all((fonts || []).map(async resource => {
    if (!resource.data?.byteLength) return;
    const descriptors = {
      style: resource.italic ? 'italic' : 'normal',
      weight: resource.bold ? '700' : '400',
    };
    try {
      const face = new FontFace(resource.family, resource.data, descriptors);
      await face.load();
      if (generation !== documentGeneration) {
        document.fonts.delete(face);
        return;
      }
      document.fonts.add(face);
      loaded.set(`${resource.family}:${descriptors.weight}:${descriptors.style}`, face);
    } catch (_) {
      // 不支持的内嵌字体不能阻止文档打开。
    }
  }));
  if (generation !== documentGeneration) {
    for (const face of loaded.values()) document.fonts.delete(face);
    return 0;
  }
  clearInjectedFonts();
  for (const [key, face] of injectedFonts.entries()) {
    if (fallbackFontURLs.some(source => key.startsWith(`${source.family}:`))) loaded.set(key, face);
  }
  injectedFonts = loaded;
  return loaded.size;
}

async function loadCachedFont(url) {
  let cache = null;
  if (typeof caches !== 'undefined') {
    try {
      cache = await caches.open(fallbackFontCacheName);
    } catch (_) {
      // Cache Storage 不可用时继续从网络加载字体。
    }
  }
  if (cache) {
    try {
      const cached = await cache.match(url);
      if (cached) {
        const data = await cached.arrayBuffer();
        if (isSupportedFontData(data)) return data;
        await cache.delete(url);
      }
    } catch (_) {
      // 忽略缓存读取错误，网络请求仍可提供字体。
    }
  }
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), fallbackFontTimeout);
  let response;
  try {
    response = await fetch(url, { mode: 'cors', cache: 'no-cache', signal: controller.signal });
  } catch (error) {
    if (error.name === 'AbortError') throw new Error('字体下载超时');
    throw error;
  } finally {
    clearTimeout(timeout);
  }
  if (!response.ok) throw new Error(`加载字体失败: ${response.status}`);
  const copy = response.clone();
  const data = await response.arrayBuffer();
  if (!isSupportedFontData(data)) {
    throw new Error('字体数据无效');
  }
  if (cache) {
    try {
      await cache.put(url, copy);
    } catch (_) {
      // 隐私浏览或受限环境可能无法使用 Cache Storage。
    }
  }
  return data;
}

function isSupportedFontData(data) {
  if (data.byteLength === 0 || data.byteLength > 32 << 20 || data.byteLength < 4) return false;
  const signature = new Uint8Array(data, 0, 4);
  return (signature[0] === 0x4f && signature[1] === 0x54 &&
      signature[2] === 0x54 && signature[3] === 0x4f) ||
    (signature[0] === 0x00 && signature[1] === 0x01 &&
      signature[2] === 0x00 && signature[3] === 0x00) ||
    (signature[0] === 0x74 && signature[1] === 0x74 &&
      signature[2] === 0x63 && signature[3] === 0x66) ||
    (signature[0] === 0x77 && signature[1] === 0x4f &&
      signature[2] === 0x46 && (signature[3] === 0x46 || signature[3] === 0x32));
}

async function preloadFallbackFonts() {
  const results = await Promise.allSettled(fallbackFontURLs.map(source => loadFallbackFont(source)));
  const loaded = results
    .filter(result => result.status === 'fulfilled')
    .map(result => result.value);
  if (!loaded.some(font => font.weight === 400)) {
    const failure = results.find(result => result.status === 'rejected');
    throw failure?.reason || new Error('没有可用的常规回退字体');
  }
  // 打开文档前等待默认字体，确保首屏渲染不依赖浏览器本地字体。
  return loaded;
}

function loadFallbackFont(source) {
  const key = `${source.family}:${source.weight}`;
  const cached = fallbackFontData.get(key);
  if (cached) return Promise.resolve({ ...source, data: cached });
  const pending = fallbackFontLoads.get(key);
  if (pending) return pending;

  const load = (async () => {
    let data;
    let failure;
    for (const url of [source.url, source.alternateURL].filter(Boolean)) {
      try {
        data = await loadCachedFont(url);
        break;
      } catch (error) {
        failure = error;
      }
    }
    if (!data) throw failure || new Error(`字体加载失败: ${source.weight}`);
    if (typeof FontFace !== 'function' || !document.fonts) {
      fallbackFontData.set(key, data);
      return { ...source, data };
    }
    try {
      const face = new FontFace(source.family, data.slice(0), {
        style: 'normal',
        weight: String(source.weight),
      });
      await face.load();
      document.fonts.add(face);
      injectedFonts.set(`${source.family}:${source.weight}:normal`, face);
    } catch (_) {
      // 浏览器 FontFace 失败时仍将原始数据交给 WASM 渲染器。
    }
    fallbackFontData.set(key, data);
    return { ...source, data };
  })();
  fallbackFontLoads.set(key, load);
  load.then(() => fallbackFontLoads.delete(key)).catch(() => fallbackFontLoads.delete(key));
  return load;
}

// 阅读器空闲时开始下载字体。之后打开文档时，可以在首个页面渲染前
// 将已加载的字体数据传给 WASM。
updateStartupProgress();
void preloadFallbackFonts().then(() => {
  startupFontReady = true;
  updateStartupProgress();
}).catch(error => {
  startupFontError = error;
  updateStartupProgress();
});

function updateNavigation() {
  pageNumber.value = pageInfos.length ? current + 1 : 1;
  pageNumber.max = pageInfos.length || 1;
  pageCount.textContent = pageInfos.length;
  pageNumber.disabled = pageInfos.length === 0;
  previous.disabled = pageInfos.length === 0 || (pageLayoutIsDouble() ? currentSpreadPosition() <= 0 : current <= 0);
  next.disabled = pageInfos.length === 0 || (pageLayoutIsDouble() ? currentSpreadPosition() + 1 >= pageSpreads.length : current + 1 >= pageInfos.length);
  printPage.disabled = documentActionBusy || pageInfos.length === 0;
  exportDocument.disabled = documentActionBusy || pageInfos.length === 0;
  copyPageText.disabled = documentActionBusy || pageInfos.length === 0;
  copyAllTextButton.disabled = documentActionBusy || pageInfos.length === 0;
  searchInput.disabled = pageInfos.length === 0;
  searchToggle.disabled = pageInfos.length === 0;
  searchButton.disabled = pageInfos.length === 0;
  searchPrevious.disabled = searchResults.length === 0;
  searchNext.disabled = searchResults.length === 0;
  zoomOut.disabled = pageInfos.length === 0;
  zoomIn.disabled = pageInfos.length === 0;
  zoomMenuToggle.disabled = pageInfos.length === 0;
  documentMenuToggle.disabled = pageInfos.length === 0;
  rotatePageButton.disabled = pageInfos.length === 0;
  readingMode.disabled = pageInfos.length === 0;
  viewToggle.disabled = pageInfos.length === 0;
  infoToggle.disabled = pageInfos.length === 0;
  pageLayoutSelect.disabled = documentActionBusy || pageInfos.length === 0;
  renderFormatSelect.disabled = documentActionBusy || pageInfos.length === 0;
  clarityPrioritySelect.disabled = documentActionBusy || pageInfos.length === 0 || !imageRenderFormat();
  zoomLabel.textContent = `${Math.round(zoom * 100)}%`;
  pagePill.hidden = !pagePillVisible || pageInfos.length === 0;
}

function setDocumentActionBusy(busy) {
  documentActionBusy = busy;
  documentActionCancelRequested = false;
  cancelAction.hidden = !busy;
  updateNavigation();
}

function cancelDocumentAction() {
  if (!documentActionBusy) return;
  documentActionCancelRequested = true;
  cancelRequests(textRequests);
  cancelRequests(pageRequests);
  exportRequest?.cancel();
  setStatus('正在取消操作...');
}

function throwIfDocumentActionCancelled(generation) {
  if (generation !== documentGeneration) throw new Error('文档已切换');
  if (documentActionCancelRequested) {
    const error = new Error('操作已取消');
    error.name = 'ActionCancelled';
    throw error;
  }
}

function updateSearchStatus(message) {
  searchStatus.textContent = message;
}

function keepThumbnailVisible(button) {
  if (!button) return;
  if (thumbnailMetrics.mobile) {
    const start = button.offsetLeft;
    const size = button.offsetWidth;
    const visibleStart = thumbnailsElement.scrollLeft;
    const visibleSize = thumbnailsElement.clientWidth;
    const offset = start < visibleStart ? start : start + size > visibleStart + visibleSize
      ? start + size - visibleSize
      : -1;
    if (offset >= 0) {
      thumbnailsElement.scrollLeft = offset;
      updateThumbnailVirtualWindow();
    }
    return;
  }
  // 不能用 offsetTop：压缩轨道里远距离目标的 top 会被浏览器布局上限（Firefox
  // 约 9e6 px）截断，导致跟随滚动到错误位置甚至空白。这里直接由行元数据取内容坐标。
  const { columns, rowOffsets, rowHeights } = thumbnailMetrics;
  const index = Number(button.dataset.index);
  const slot = thumbnailSlotForPage(index);
  const row = slot >= 0 ? Math.floor(slot / columns) : -1;
  const contentStart = row >= 0 ? (rowOffsets[row] || 0) : button.offsetTop + thumbnailAnchor;
  const size = row >= 0 ? (rowHeights[row] || button.offsetHeight) : button.offsetHeight;
  const contentVisibleStart = thumbnailContentFromScroll(thumbnailTrackScrollY());
  const visibleSize = thumbnailsElement.clientHeight;
  const target = contentStart < contentVisibleStart
    ? contentStart
    : contentStart + size > contentVisibleStart + visibleSize
      ? contentStart + size - visibleSize
      : -1;
  if (target < 0) return;
  thumbnailsElement.scrollTop = thumbnailScrollFromContent(target);
  // 滚动后同步刷新虚拟窗口：重新锚定并挂载目标附近的行。只依赖异步 scroll/rAF
  // 时，远距离跳转可能迟迟不刷新，表现为缩略图区域空白。
  updateThumbnailVirtualWindow();
}

function setCurrent(index, syncThumbnail = true) {
  if (index < 0 || index >= pageInfos.length) return;
  const changed = current !== index;
  current = index;
  saveReadingPosition();
  // 图片可能已经从缓存显示，但此前的文字请求可能在虚拟页面卸载时被取消。
  // 当前页切换时主动补发一次，避免正文层因请求竞态缺失。
  if (changed && pageCards[index]) void loadText(index);
  if (changed && syncThumbnail) {
    updateThumbnailVirtualWindow(thumbnailSlotForPage(index));
    keepThumbnailVisible(thumbnailButtons[index]);
  }
  thumbnailButtons.forEach((button, buttonIndex) => {
    if (!button) return;
    const active = buttonIndex === current;
    button.classList.toggle('active', active);
    if (active) button.setAttribute('aria-current', 'page');
    else button.removeAttribute('aria-current');
  });
  if (changed) updateOutlineActive();
  if (zoomMode === 'page') fitPageZoom();
  updateNavigation();
}

function goTo(index) {
  if (index < 0 || index >= pageInfos.length) return;
  const position = pageSpreadPositionForPage(index);
  mountPageSpread(position);
  setCurrent(index);
  const trackTop = pageVirtualTrack.getBoundingClientRect().top + window.scrollY;
  window.scrollTo({ top: trackTop + pageTrackScrollFromContent(pageSpreadOffset(position)), behavior: 'smooth' });
  schedulePageVirtualTranslate();
}

function navigatePage(delta) {
  if (pageLayoutIsDouble()) {
    moveToSpread(delta);
    return;
  }
  goTo(current + delta);
}

function setPageLayout(value) {
  if (!['single', 'double', 'double-odd-left'].includes(value) || value === pageLayout) return;
  pageLayout = value;
  pageLayoutSelect.value = value;
  updateThumbnailLayout();
  try {
    localStorage.setItem(pageLayoutStorageKey, value);
  } catch (_) {}
  cancelRequests(pageRequests);
  cancelRequests(thumbnailRequests);
  pageCache.clear();
  thumbnailCache.clear();
  resetRenderProgress();
  buildPages();
}

function applyPageWidth() {
  updatePageVirtualMetrics();
  updateThumbnailMetrics();
  pageSpreads.forEach(applyPageWidthToSpread);
  thumbnailButtons.forEach((button, index) => {
    if (button) {
      resizeThumbnail(index, button);
      button.querySelector('img')?.style.setProperty('transform', `rotate(${pageRotation}deg)`);
    }
  });
}

function setZoom(value, mode = 'manual') {
	if (!pageInfos.length) return;
	const target = Math.max(0.5, Math.min(3, value));
	if (mode === 'fit' || mode === 'page') {
		try {
			localStorage.setItem(zoomModeStorageKey, mode);
		} catch (_) {}
	}
	if (target === zoom && mode === zoomMode) return;
	const previousDPI = pageDPI();
  zoom = target;
  zoomMode = mode;
  applyPageWidth();
  if (previousDPI !== pageDPI()) reloadPageImages();
  else pageCards.forEach((card, index) => {
    if (textCache.has(index)) buildTextLayer(index);
  });
  scheduleVirtualUpdate();
  updateNavigation();
}

function reloadPageImages() {
  zoomGeneration++;
  resetRenderProgress();
  cancelRequests(pageRequests);
  pageCards.forEach(card => {
    card.classList.add('loading');
    card.querySelector('.page-image').hidden = true;
  });
  // 先隐藏旧图片，再清理缓存，避免撤销仍挂在 DOM 上的 Blob URL 后留下空白页。
  pageCache.clear();
  pageCards.forEach((card, index) => {
    const bounds = card.getBoundingClientRect();
    if (bounds.top < window.innerHeight + 800 && bounds.bottom > -800) loadPage(index);
  });
}

function fitWidthZoom() {
  if (!pageInfos.length || !pagesElement.clientWidth) return;
  setZoom(Math.max(0.5, Math.min(3, pagesElement.clientWidth / layoutBaseWidth())), 'fit');
}

function fitPageZoom() {
  const dimensions = spreadDimensions(currentSpreadPosition());
  if (!dimensions.height || !pagesElement.clientWidth) return;
  const readerStyle = getComputedStyle(document.querySelector('.reader'));
  const verticalPadding = parseFloat(readerStyle.paddingTop) + parseFloat(readerStyle.paddingBottom);
  const availableWidth = Math.max(1, pagesElement.clientWidth);
  const availableHeight = Math.max(1, window.innerHeight - headerHeight() - status.offsetHeight - verticalPadding - 24);
  const spreadRatio = dimensions.width / dimensions.height;
  const spreadWidth = Math.min(availableWidth, availableHeight * spreadRatio);
  setZoom(Math.max(0.5, Math.min(3, spreadWidth / dimensions.width)), 'page');
}

function rotationStorageKey() {
  return currentDocumentKey ? `ofd-rotation:${currentDocumentKey}` : '';
}

function restorePageRotation() {
  pageRotation = 0;
  try {
    const value = Number.parseInt(localStorage.getItem(rotationStorageKey()) || '0', 10);
    if ([0, 90, 180, 270].includes(value)) pageRotation = value;
  } catch (_) {}
  rotatePageButton.title = `旋转 ${pageRotation}°`;
  rotatePageButton.setAttribute('aria-label', `旋转 ${pageRotation}°`);
}

function rotatePage() {
  if (!pageInfos.length) return;
  pageRotation = (pageRotation + 90) % 360;
  applyPageWidth();
  if (zoomMode === 'fit') fitWidthZoom();
  else if (zoomMode === 'page') fitPageZoom();
  try {
    localStorage.setItem(rotationStorageKey(), String(pageRotation));
  } catch (_) {}
  rotatePageButton.title = `旋转 ${pageRotation}°`;
  rotatePageButton.setAttribute('aria-label', `旋转 ${pageRotation}°`);
}

function touchDistance(touches) {
  const first = touches[0];
  const second = touches[1];
  return Math.hypot(second.clientX - first.clientX, second.clientY - first.clientY);
}

function handleTouchStart(event) {
  if (!pageInfos.length) return;
  if (event.touches.length === 1) {
    touchStartX = event.touches[0].clientX;
    touchStartY = event.touches[0].clientY;
    touchPinching = false;
  } else if (event.touches.length === 2) {
    touchStartDistance = touchDistance(event.touches);
    touchPinching = true;
  }
}

function handleTouchMove(event) {
  if (event.touches.length !== 2 || !touchStartDistance) return;
  event.preventDefault();
  const scale = touchDistance(event.touches) / touchStartDistance;
  if (Number.isFinite(scale) && scale > 0) {
    touchZoomTarget = Math.max(0.5, Math.min(3, zoom * scale));
    if (!touchZoomTimer) {
      touchZoomTimer = setTimeout(() => {
        touchZoomTimer = undefined;
        if (touchZoomTarget) {
          setZoom(touchZoomTarget);
          touchZoomTarget = 0;
        }
      }, 80);
    }
  }
  touchStartDistance = touchDistance(event.touches);
}

function handleTouchEnd(event) {
  if (touchPinching || event.changedTouches.length !== 1 || !pageInfos.length) {
    touchStartDistance = 0;
    touchPinching = false;
    return;
  }
  const touch = event.changedTouches[0];
  const deltaX = touch.clientX - touchStartX;
  const deltaY = touch.clientY - touchStartY;
  touchStartDistance = 0;
  if (Math.abs(deltaX) < 60 || Math.abs(deltaX) < Math.abs(deltaY) * 1.25) return;
  navigatePage(deltaX < 0 ? 1 : -1);
}

function headerHeight() {
  const height = document.querySelector('header')?.offsetHeight || 0;
  document.documentElement.style.setProperty('--header-height', `${height}px`);
  return height;
}

function matchingSearchResults(pageIndex, runIndex) {
  return searchResults
    .filter(result => result.page === pageIndex && result.run === runIndex)
    .sort((left, right) => left.start - right.start);
}

// textLayerSegments 把同一行、同字体、相邻的文本片段合并成一个片段。
// OFD 常把每个字形输出为独立的 TextObject；若逐字形创建绝对定位的 span，
// 单页就可能产生数千个节点，浏览器布局/绘制的文字结构内存会急剧膨胀。
function textLayerSegments(runs) {
  const segments = [];
  for (const run of runs) {
    if (!run.text) continue;
    const angle = run.glyphs?.[0]?.angle ?? run.charDirection ?? 0;
    const previous = segments[segments.length - 1];
    const tolerance = previous
      ? Math.max(1, Math.min(run.height || 1, previous.height || 1) * 0.5)
      : 0;
    const mergeable = angle === 0 && previous && previous.angle === 0 &&
      previous.fontFamily === (run.fontFamily || '') &&
      previous.weight === run.weight &&
      previous.bold === run.bold &&
      previous.italic === run.italic &&
      previous.size === run.size &&
      Math.abs(run.y - previous.y) <= tolerance &&
      run.x <= previous.x + previous.width + Math.max(run.height || 1, 1) * 2;
    if (mergeable) {
      previous.text += run.text;
      previous.width = Math.max(previous.width, run.x + run.width - previous.x);
      previous.height = Math.max(previous.height, run.height);
      continue;
    }
    segments.push({
      x: run.x,
      y: run.y,
      width: run.width,
      height: run.height,
      text: run.text,
      fontFamily: run.fontFamily || '',
      weight: run.weight,
      bold: run.bold,
      italic: run.italic,
      size: run.size,
      angle,
    });
  }
  return segments;
}

function buildTextLayer(index) {
  const card = pageCards[index];
  const runs = textCache.get(index);
  const info = pageInfos[index];
  if (!card || !runs || !info) return;

  const layer = card.querySelector('.text-layer') || document.createElement('div');
  layer.className = 'text-layer';
  layer.replaceChildren();
  if (!layer.parentElement) card.append(layer);
  if (!textLayerVisible) return;

  for (const segment of textLayerSegments(runs)) {
    const element = document.createElement('span');
    element.className = 'text-run';
    element.style.left = `${segment.x / info.width * 100}%`;
    // TextRun 坐标已经是以页面左上角为原点的覆盖层坐标。
    // 渲染器只在画布内部翻转 Y 轴，因此这里不能再次翻转文字层。
    element.style.top = `${segment.y / info.height * 100}%`;
    element.style.width = `${Math.max(segment.width / info.width * 100, 0.1)}%`;
    element.style.height = `${Math.max(segment.height / info.height * 100, 0.1)}%`;
    element.style.fontSize = `${Math.max(segment.size * card.clientWidth / info.width, 1)}px`;
    if (segment.fontFamily) element.style.fontFamily = `'${segment.fontFamily}', sans-serif`;
    element.style.fontWeight = segment.weight > 0 ? String(segment.weight) : (segment.bold ? '700' : '400');
    element.style.fontStyle = segment.italic ? 'italic' : 'normal';
    // 只有旋转文本才需要 transform；水平文本省略可避免为每个片段创建
    // 额外的变换/合成上下文。
    if (segment.angle) {
      element.style.transformOrigin = 'top left';
      element.style.transform = `rotate(${segment.angle}deg)`;
    }
    element.textContent = segment.text;
    layer.append(element);
  }
  // 搜索高亮仍按原始 run 的矩形单独渲染，保持命中位置精确。
  runs.forEach((run, runIndex) => {
    for (const match of matchingSearchResults(index, runIndex)) {
      for (const rect of match.rects || []) {
        const highlight = document.createElement('span');
        highlight.className = 'search-highlight';
        if (searchResults[activeSearchResult] === match) highlight.classList.add('active');
        highlight.style.left = `${rect.x / info.width * 100}%`;
        highlight.style.top = `${rect.y / info.height * 100}%`;
        highlight.style.width = `${Math.max(rect.width / info.width * 100, 0.1)}%`;
        highlight.style.height = `${Math.max(rect.height / info.height * 100, 0.1)}%`;
        if (rect.angle) {
          highlight.style.transformOrigin = 'top left';
          highlight.style.transform = `rotate(${rect.angle}deg)`;
        }
        highlight.setAttribute('aria-hidden', 'true');
        layer.append(highlight);
      }
    }
  });
}

function pageImageIsReady(image) {
  return image && !image.hidden && image.complete && image.naturalWidth > 0;
}

function clearPageLoading(card, image) {
  if (pageImageIsReady(image)) card.classList.remove('loading');
}

function showImageWhenReady(imageElement, url, card) {
  imageElement.classList.remove('loaded');
  imageElement.src = url;
  imageElement.hidden = false;
  if (imageElement.complete && imageElement.naturalWidth > 0) {
    imageElement.classList.add('loaded');
    if (card) clearPageLoading(card, imageElement);
  }
}

function loadText(index, pinned = false) {
  const card = pageCards[index];
  if (textCache.has(index)) {
    if (card) buildTextLayer(index);
    return Promise.resolve();
  }
  if (textRequests.has(index)) {
    const existing = textRequests.get(index);
    if (card) existing.textCards.add(card);
    if (pinned || !card) existing.textPinned = true;
    return existing;
  }
  const generation = documentGeneration;
  const engineRequest = engine.text(index);
  const request = engineRequest
    .then(runs => {
      if (generation !== documentGeneration) return;
      textCache.set(index, runs);
      trimTextCache();
      if (pageCards[index]) buildTextLayer(index);
    })
    .catch(error => {
      if (generation === documentGeneration && !isCancelledError(error) && pageCards[index]) {
        pageCards[index].title = error.message;
      }
    })
    .finally(() => {
      if (textRequests.get(index) === request) textRequests.delete(index);
    });
  request.cancel = () => engineRequest.cancel();
  request.textCards = card ? new Set([card]) : new Set();
  request.textPinned = pinned || !card;
  textRequests.set(index, request);
  return request;
}

function pageDPI() {
  return imageRenderFormat() && clarityPriority
    ? Math.max(96, Math.min(300, Math.round(96 * zoom)))
    : 96;
}

function imageRenderFormat() {
  return renderFormat === 'png' || renderFormat === 'jpg';
}

function cacheKey(kind, index, generation, dpi, format = renderFormat) {
  return `${generation}:${kind}:${index}:${dpi}:${format}`;
}

function renderMimeType(format) {
  if (format === 'svg') return 'image/svg+xml';
  if (format === 'jpg') return 'image/jpeg';
  return 'image/png';
}

function loadImage(index, kind, generation, imageElement, card) {
  const cache = kind === 'page' ? pageCache : thumbnailCache;
  const requests = kind === 'page' ? pageRequests : thumbnailRequests;
  const dpi = kind === 'page' ? pageDPI() : 15;
  const format = renderFormat;
  const requestedZoomGeneration = zoomGeneration;
  const key = cacheKey(kind, index, generation, dpi, format);
  // 同一 DPI 下缩放变化时，旧请求不能被新页面复用，否则旧请求返回
  // null 后，新页面会一直保留 loading 状态而不会重新发起渲染。
  const requestKey = kind === 'page' ? `${key}:${requestedZoomGeneration}` : key;
  const cached = cache.get(key);
  if (cached) {
    showImageWhenReady(imageElement, cached, card);
    if (kind === 'page' && generation === documentGeneration) markPageLoaded(index);
    return Promise.resolve(cached);
  }
  if (requests.has(requestKey)) {
    const existing = requests.get(requestKey);
    if (kind === 'page') {
      if (card) existing.pageCards.add(card);
      else existing.pagePinned = true;
    }
    return existing.then(url => {
      if (url && format === renderFormat && generation === documentGeneration &&
          (kind !== 'page' || requestedZoomGeneration === zoomGeneration)) {
        showImageWhenReady(imageElement, url, card);
      }
      return url;
    });
  }

  const engineRequest = engine.renderPage(index, { format, dpi, background: transparentRenderBackground });
  const request = engineRequest
    .then(data => {
      const url = URL.createObjectURL(new Blob([data], { type: renderMimeType(format) }));
      if (format !== renderFormat || generation !== documentGeneration ||
          (kind === 'page' && requestedZoomGeneration !== zoomGeneration)) {
        URL.revokeObjectURL(url);
        return null;
      }
      cache.set(key, url, data.byteLength);
      showImageWhenReady(imageElement, url, card);
      if (kind === 'page' && generation === documentGeneration) markPageLoaded(index);
      return url;
    })
    .catch(error => {
      if (format === renderFormat && generation === documentGeneration &&
          (kind !== 'page' || requestedZoomGeneration === zoomGeneration) && card) {
        if (!isCancelledError(error)) {
          card.classList.remove('loading');
          card.title = error.message;
          if (kind === 'page') markPageFailed(index);
          if (kind === 'page') showPageError(index, error.message);
        }
      }
      throw error;
    })
    .finally(() => {
      if (requests.get(requestKey) === request) requests.delete(requestKey);
    });
  request.cancel = () => engineRequest.cancel();
  if (kind === 'page') {
    request.pageIndex = index;
    request.pageCards = card ? new Set([card]) : new Set();
    request.pagePinned = !card;
  }
  requests.set(requestKey, request);
  return request;
}

function cancelPageRequest(index, card) {
  if (card) pageCardRequests.delete(card);
  for (const [key, request] of pageRequests.entries()) {
    if (request.pageIndex !== index) continue;
    request.pageCards?.delete(card);
    if (request.pagePinned || request.pageCards?.size) continue;
    request.cancel?.();
    if (pageRequests.get(key) === request) pageRequests.delete(key);
  }
}

function cancelTextRequest(index, card) {
  const request = textRequests.get(index);
  if (!request) return;
  request.textCards?.delete(card);
  if (request.textPinned || request.textCards?.size) return;
  request.cancel?.();
  if (textRequests.get(index) === request) textRequests.delete(index);
}

function loadPage(index) {
  const card = ensurePageMounted(index);
  if (!card) return;
  const existing = pageCardRequests.get(card);
  if (existing) return existing;
  const generation = documentGeneration;
  const image = card.querySelector('.page-image');
  const token = String(++pageLoadToken);
  card.dataset.loadToken = token;
  card.classList.remove('render-error');
  card.querySelector('.page-error')?.remove();
  card.classList.add('loading');
  clearPageLoading(card, image);
  const isCurrentCardRequest = () =>
    generation === documentGeneration && pageCards[index] === card && card.dataset.loadToken === token;
  const request = loadImage(index, 'page', generation, image, card)
    .then(url => {
      if (!url) {
        // 缩放期间旧请求会正常返回 null；setZoom 已经负责启动新 DPI 请求。
        return undefined;
      }
      // WASM 返回 PNG 数据不等于浏览器已经完成图片解码；加载遮罩由
      // 图片的 load 事件移除，避免滚动期间出现短暂空白页。
      void loadActualPageInfo(index);
      // 页面离开虚拟窗口后不再为已卸载的页面读取文字层。
      return isCurrentCardRequest() ? loadText(index) : undefined;
    })
    .catch(error => {
      if (isCurrentCardRequest()) {
        card.classList.remove('loading');
      }
      if (isCancelledError(error)) return;
      if (generation === documentGeneration && pageCards[index] === card) {
        setStatus(`第 ${index + 1} 页渲染失败：${error.message}`);
      }
    })
    .finally(() => {
      // 无论图片请求成功、失败、取消还是因缩放过期返回 null，
      // 当前卡片都不能遗留“正在渲染...”遮罩。
      if (isCurrentCardRequest()) clearPageLoading(card, image);
      if (pageCardRequests.get(card) === request) pageCardRequests.delete(card);
    });
  pageCardRequests.set(card, request);
  return request;
}

function loadActualPageInfo(index) {
  const generation = documentGeneration;
  const key = `${generation}:${index}`;
  if (pageInfoRequests.has(key)) return pageInfoRequests.get(key);
  const engineRequest = engine.pageInfo(index);
  const request = engineRequest
    .then(info => {
      if (generation !== documentGeneration || !info || info.width <= 0 || info.height <= 0) return;
      const currentInfo = pageInfos[index];
      if (!currentInfo || (currentInfo.width === info.width && currentInfo.height === info.height)) return;
      pageInfos[index] = info;
      updatePageVirtualMetrics();
      applyPageWidth();
      if (pageCards[index]) buildTextLayer(index);
    })
    .catch(() => undefined)
    .finally(() => {
      if (pageInfoRequests.get(key) === request) pageInfoRequests.delete(key);
    });
  request.cancel = () => engineRequest.cancel();
  pageInfoRequests.set(key, request);
  return request;
}

function loadThumbnail(index) {
  const button = thumbnailButtons[index];
  if (!button) return;
  const generation = documentGeneration;
  const image = button.querySelector('img');
  const format = thumbnailRenderFormat;
  const key = cacheKey('thumbnail', index, generation, 15, format);
  const cached = thumbnailCache.get(key);
  if (cached) {
    showImageWhenReady(image, cached);
    button.classList.remove('loading');
    button.classList.remove('render-error');
    button.querySelector('.thumbnail-error')?.remove();
    return Promise.resolve(cached);
  }
  if (thumbnailRequests.has(key)) {
    return thumbnailRequests.get(key).then(url => {
      if (url && generation === documentGeneration) {
        showImageWhenReady(image, url);
        button.classList.remove('loading');
      }
      return url;
    }).catch(error => {
      if (!isCancelledError(error)) button.title = error.message;
      return null;
    });
  }

  let resolveRequest;
  let rejectRequest;
  const request = new Promise((resolve, reject) => {
    resolveRequest = resolve;
    rejectRequest = reject;
  });
  const entry = { index, generation, format, key, image, button, resolveRequest, rejectRequest, request };
  button.classList.add('loading');
  request.cancel = () => {
    entry.cancelled = true;
    thumbnailBatchQueue.delete(key);
    thumbnailRequests.delete(key);
    const batchEntries = entry.batchRequest?.thumbnailEntries;
    if (batchEntries?.every(item => item.cancelled)) entry.batchRequest.cancel();
    const error = new Error('请求已取消');
    error.name = 'AbortError';
    rejectRequest(error);
  };
  request.index = index;
  thumbnailRequests.set(key, request);
  thumbnailBatchQueue.set(key, entry);
  if (!thumbnailBatchTimer) thumbnailBatchTimer = setTimeout(flushThumbnailBatch, 0);
  request.catch(error => {
    if (!isCancelledError(error)) button.title = error.message;
  });
  return request;
}

function cancelThumbnailRequest(index) {
  for (const [key, request] of thumbnailRequests.entries()) {
    if (request.index !== index) continue;
    request.cancel?.();
    if (thumbnailRequests.get(key) === request) thumbnailRequests.delete(key);
  }
}

function clearThumbnailBatch() {
  if (thumbnailBatchTimer) {
    clearTimeout(thumbnailBatchTimer);
    thumbnailBatchTimer = undefined;
  }
  thumbnailBatchQueue.clear();
}

function flushThumbnailBatch() {
  thumbnailBatchTimer = undefined;
  const entries = Array.from(thumbnailBatchQueue.values()).slice(0, 8);
  for (const entry of entries) thumbnailBatchQueue.delete(entry.key);
  if (!entries.length) return;

  const generation = entries[0].generation;
  const active = entries.filter(entry => !entry.cancelled && entry.generation === generation);
  if (!active.length) return;
  const format = thumbnailRenderFormat;
  const renderRequest = engine.renderPages(
    active.map(entry => entry.index),
    { format, dpi: 15, background: transparentRenderBackground },
  );
  renderRequest.thumbnailEntries = active;
  for (const entry of active) entry.batchRequest = renderRequest;
  renderRequest.then(images => {
    images.forEach((data, index) => {
      const entry = active[index];
      if (entry.cancelled || entry.generation !== documentGeneration) return;
      const url = URL.createObjectURL(new Blob([data], { type: renderMimeType(format) }));
      thumbnailCache.set(entry.key, url, data.byteLength);
      entry.image.src = url;
      entry.image.hidden = false;
      entry.image.classList.add('loaded');
      entry.button.classList.remove('loading');
      entry.button.classList.remove('render-error');
      entry.button.querySelector('.thumbnail-error')?.remove();
      entry.resolveRequest(url);
    });
    for (const entry of active) thumbnailRequests.delete(entry.key);
  }).catch(error => {
    for (const entry of active) {
      thumbnailRequests.delete(entry.key);
      if (!entry.cancelled) {
        entry.button.classList.remove('loading');
        showThumbnailError(entry, error.message);
        entry.rejectRequest(error);
      }
    }
  }).finally(() => {
    if (thumbnailBatchQueue.size && !thumbnailBatchTimer) {
      thumbnailBatchTimer = setTimeout(flushThumbnailBatch, 0);
    }
  });
}

function buildPages() {
  updateThumbnailLayout();
  readerElement.classList.toggle('hide-thumbnails', !thumbnailsVisible);
  applySidebarPanels();
  resizeObserver?.disconnect();
  pagesElement.replaceChildren();
  thumbnailsElement.replaceChildren();
  pageCards = [];
  thumbnailButtons = [];
  pageSpreads = pageSpreadGroups().map(pages => ({ pages, offset: 0, height: 0, width: 0 }));
  pageVirtualTrack = document.createElement('div');
  pageVirtualTrack.className = 'page-virtual-track';
  pagesElement.append(pageVirtualTrack);
  pageVirtualWindow = document.createElement('div');
  pageVirtualWindow.className = 'page-virtual-window';
  pageVirtualTrack.append(pageVirtualWindow);
  thumbnailVirtualTrack = document.createElement('div');
  thumbnailVirtualTrack.className = 'thumbnail-virtual-track';
  thumbnailsElement.append(thumbnailVirtualTrack);
  thumbnailVirtualWindow = document.createElement('div');
  thumbnailVirtualWindow.className = 'thumbnail-virtual-window';
  thumbnailVirtualTrack.append(thumbnailVirtualWindow);
  thumbnailSlots = thumbnailSlotsForLayout();
  thumbnailSlotByPage = [];
  thumbnailSlots.forEach((index, slot) => {
    if (index >= 0) thumbnailSlotByPage[index] = slot;
  });
  updatePageVirtualMetrics();
  updateThumbnailMetrics();

  pageLayoutSelect.value = pageLayout;
  resizeObserver = createResizeObserver(entries => {
    for (const entry of entries) {
      const index = Number(entry.target.dataset.index);
      const runs = textCache.get(index);
      if (runs) buildTextLayer(index);
    }
  });
  if (zoomMode === 'fit') {
    const available = pagesElement.clientWidth;
    if (available > 0) zoom = Math.max(0.5, Math.min(3, available / layoutBaseWidth()));
  }
  applyPageWidth();
  updatePageVirtualWindow(false);
  updateThumbnailVirtualWindow();
  empty.hidden = pageInfos.length > 0;
  setCurrent(current);
  updateNavigation();
  if (pageInfos.length) {
    ensurePageMounted(current);
    void loadText(current);
    updateThumbnailVirtualWindow(thumbnailSlotForPage(current));
    loadThumbnail(current);
    const currentThumbnail = thumbnailButtons[current];
    if (currentThumbnail) {
      keepThumbnailVisible(currentThumbnail);
      requestAnimationFrame(() => {
        if (pageInfos.length && thumbnailButtons[current] === currentThumbnail) {
          keepThumbnailVisible(currentThumbnail);
        }
      });
    }
    if (current > 0) {
      const trackTop = pageVirtualTrack.getBoundingClientRect().top + window.scrollY;
      window.scrollTo({ top: Math.max(0, trackTop + pageTrackScrollFromContent(pageSpreadOffset(pageSpreadPositionForPage(current))) - headerHeight()), behavior: 'auto' });
      schedulePageVirtualTranslate();
    }
  }
}

function cancelRequests(requests) {
  for (const request of requests.values()) {
    if (requests === pageRequests) {
      request.pageCards?.forEach(card => {
        if (pageCardRequests.get(card) === request) pageCardRequests.delete(card);
      });
    }
    if (typeof request.cancel === 'function') request.cancel();
  }
  requests.clear();
  if (requests === pageRequests) pageCardRequests.clear();
}

async function loadFile() {
  const selected = file.files[0];
  return openSelectedFile(selected);
}

async function openSelectedFile(selected) {
  if (!selected) return;
  startupProgressActive = false;
  startupScreen.hidden = true;
  saveReadingPosition();
  const generation = ++documentGeneration;
  opening = true;
  cancelOpen.hidden = false;
  openRequest?.cancel();
  openRequest = undefined;
  clearInjectedFonts();
  pageCache.clear();
  thumbnailCache.clear();
  cancelRequests(pageRequests);
  cancelRequests(pageInfoRequests);
  cancelRequests(thumbnailRequests);
  clearThumbnailBatch();
  cancelRequests(textRequests);
  const wasExporting = exportActive || !!exportRequest;
  exportRequest?.cancel();
  exportRequest = undefined;
  if (wasExporting) {
    exportActive = false;
    setDocumentActionBusy(false);
  }
  searchRequest?.cancel();
  searchRequest = undefined;
  textCache.clear();
  searchResults = [];
  activeSearchResult = -1;
  searchGeneration++;
  updateSearchStatus('');
  resetRenderProgress();
  resizeObserver?.disconnect();
  pagesElement.replaceChildren(empty);
  thumbnailsElement.replaceChildren();
  pageCards = [];
  thumbnailButtons = [];
  pageSpreads = [];
  pageVirtualTrack = undefined;
  pageVirtualWindow = undefined;
  pageAnchor = 0;
  pageVirtualTranslate = 0;
  pageVirtualTranslateFrame = undefined;
  thumbnailVirtualTrack = undefined;
  thumbnailVirtualWindow = undefined;
  thumbnailAnchor = 0;
  thumbnailVirtualTranslate = 0;
  thumbnailsElement.style.paddingBottom = '';
  resetSidebarFilter();
  outlineNodes = [];
  bookmarkNodes = [];
  renderOutline();
  renderBookmarks();
  documentInfo = null;
  documentInfoGeneration = -1;
  documentFontUsage = null;
  documentFontUsageGeneration = -1;
  documentFontUsageMeta = { scanned: 0, truncated: false };
  outlineExpandState = {};
  sidebarScroll = {};
  if (activeSidebarTab === 'fonts') fontsElement?.replaceChildren();
  if (activeSidebarTab === 'attachments') attachmentsElement?.replaceChildren();
  revokeMediaURLs();
  if (activeSidebarTab === 'media') mediaElement?.replaceChildren();
  if (activeSidebarTab === 'annotations') annotationsElement?.replaceChildren();
  if (activeSidebarTab === 'signatures') signaturesElement?.replaceChildren();
  documentName.textContent = selected.name;
  documentName.title = selected.name;
  setStatus(`正在打开 ${selected.name}...`);
  try {
    // 在读取和解析新文件前释放旧 Reader，避免切换大文档时新旧文档同时驻留。
    await engine.close();
    if (generation !== documentGeneration) return;
    const data = await selected.arrayBuffer();
    if (generation !== documentGeneration) return;
    try {
      const fallbackFonts = await preloadFallbackFonts();
      if (!fallbackFontRegistration) {
        fallbackFontRegistration = Promise.all(fallbackFonts.map(font => engine.addFallbackFont(
          font.data,
          font.family,
          font.weight,
          false,
        ))).catch(error => {
            fallbackFontRegistration = undefined;
            throw error;
          });
      }
      await fallbackFontRegistration;
    } catch (error) {
      throw new Error(`默认中文字体不可用：${error.message}`);
    }
    if (generation !== documentGeneration) return;
    openRequest = engine.open(data, openDocumentOptions);
    const result = await openRequest;
    if (generation !== documentGeneration) return;
    await injectFonts(result.fonts, generation);
    if (generation !== documentGeneration) return;
    const pageCount = Number.isInteger(result.pageCount) && result.pageCount >= 0
      ? result.pageCount
      : (result.pages?.length || 0);
    pageInfos = Array.from({ length: pageCount }, (_, index) => {
      const info = result.pages?.[index];
      return info && Number.isFinite(info.width) && info.width > 0 &&
        Number.isFinite(info.height) && info.height > 0
        ? info
        : { index, width: 210, height: 297 };
    });
    currentDocumentKey = documentKey(selected);
    sidebarScroll = readSidebarScroll();
    current = restoreReadingPosition(selected, pageInfos.length);
    await applyDocumentPreferences();
    if (generation !== documentGeneration) return;
    restorePageRotation();
    buildPages();
    if (activeSidebarTab === 'fonts') renderFonts();
    if (activeSidebarTab === 'attachments') renderAttachments();
    if (activeSidebarTab === 'media') renderMedia();
    if (activeSidebarTab === 'annotations') renderAnnotations();
    if (activeSidebarTab === 'signatures') renderSignatures();
    setStatus(`已打开：${selected.name}`);
    updateRenderProgress();
    void loadOutline();
    void saveRecentFile(selected);
    void reportMemory('打开文档');
  } catch (error) {
    if (generation !== documentGeneration || isCancelledError(error)) return;
    pageInfos = [];
    pageSpreads = [];
    pageVirtualTrack = undefined;
    pageVirtualWindow = undefined;
    pageAnchor = 0;
    pageVirtualTranslate = 0;
    pageVirtualTranslateFrame = undefined;
    thumbnailVirtualTrack = undefined;
    thumbnailVirtualWindow = undefined;
    thumbnailAnchor = 0;
    thumbnailVirtualTranslate = 0;
    resetSidebarFilter();
    outlineNodes = [];
    bookmarkNodes = [];
    renderOutline();
    renderBookmarks();
    documentInfo = null;
    documentInfoGeneration = -1;
    documentFontUsage = null;
    documentFontUsageGeneration = -1;
    documentFontUsageMeta = { scanned: 0, truncated: false };
    outlineExpandState = {};
    if (activeSidebarTab === 'fonts') renderFonts();
    if (activeSidebarTab === 'attachments') renderAttachments();
    if (activeSidebarTab === 'media') renderMedia();
    if (activeSidebarTab === 'annotations') renderAnnotations();
    if (activeSidebarTab === 'signatures') renderSignatures();
    pagesElement.replaceChildren();
    thumbnailsElement.replaceChildren();
    pagesElement.append(empty);
    empty.hidden = false;
    pagesElement.style.paddingBottom = '';
    thumbnailsElement.style.paddingBottom = '';
    resetRenderProgress();
    currentDocumentKey = '';
    current = 0;
    updateNavigation();
    setStatus(`打开失败：${error.message}`);
  } finally {
    if (generation === documentGeneration) {
      openRequest = undefined;
      opening = false;
      cancelOpen.hidden = true;
    }
  }
}

function cancelOpening() {
  if (!opening) return;
  documentGeneration++;
  openRequest?.cancel();
  openRequest = undefined;
  cancelRequests(pageRequests);
  cancelRequests(pageInfoRequests);
  cancelRequests(thumbnailRequests);
  clearThumbnailBatch();
  cancelRequests(textRequests);
  searchRequest?.cancel();
  searchRequest = undefined;
  clearInjectedFonts();
  // open 已经进入 Worker 时，取消只会取消前端 Promise；显式 close
  // 确保 WASM 侧不会留下被取消打开的 Reader。
  void engine.close().catch(error => console.warn('[OFD] 取消打开时释放 Reader 失败', error));
  pageCache.clear();
  thumbnailCache.clear();
  textCache.clear();
  pageInfos = [];
  pageSpreads = [];
  if (!infoPanel.hidden) updateDocumentInfo();
  pageCards = [];
  thumbnailButtons = [];
  pageVirtualTrack = undefined;
  pageVirtualWindow = undefined;
  pageAnchor = 0;
  pageVirtualTranslate = 0;
  pageVirtualTranslateFrame = undefined;
  thumbnailVirtualTrack = undefined;
  thumbnailVirtualWindow = undefined;
  thumbnailAnchor = 0;
  thumbnailVirtualTranslate = 0;
  resetSidebarFilter();
  outlineNodes = [];
  bookmarkNodes = [];
  renderOutline();
  renderBookmarks();
  documentInfo = null;
  documentInfoGeneration = -1;
  documentFontUsage = null;
  documentFontUsageGeneration = -1;
  documentFontUsageMeta = { scanned: 0, truncated: false };
  outlineExpandState = {};
  if (activeSidebarTab === 'fonts') renderFonts();
  if (activeSidebarTab === 'attachments') renderAttachments();
  if (activeSidebarTab === 'media') renderMedia();
  if (activeSidebarTab === 'annotations') renderAnnotations();
  if (activeSidebarTab === 'signatures') renderSignatures();
  thumbnailSlots = [];
  thumbnailSlotByPage = [];
  currentDocumentKey = '';
  current = 0;
  searchGeneration++;
  pagesElement.replaceChildren(empty);
  empty.hidden = false;
  pagesElement.style.paddingBottom = '';
  thumbnailsElement.style.paddingBottom = '';
  resetRenderProgress();
  updateNavigation();
  documentName.textContent = '未打开文档';
  documentName.title = '';
  opening = false;
  cancelOpen.hidden = true;
  setStatus('已取消打开文档。');
}

async function searchDocument() {
  const generation = ++searchGeneration;
  const query = searchInput.value.trim();
  searchRequest?.cancel();
  searchRequest = undefined;
  if (!query || !pageInfos.length) {
    searchResults = [];
    activeSearchResult = -1;
    updateSearchStatus('');
    pageCards.forEach((card, index) => { if (card) buildTextLayer(index); });
    updateNavigation();
    return;
  }
  searchButton.disabled = true;
  updateSearchStatus('搜索中...');
  try {
    searchRequest = engine.search(query);
    const results = await searchRequest;
    if (generation !== searchGeneration) return;
    searchResults = results;
    activeSearchResult = results.length ? 0 : -1;
    if (!results.length) {
      updateSearchStatus('无匹配');
      pageCards.forEach((card, index) => { if (card) buildTextLayer(index); });
      return;
    }
    updateSearchStatus(`找到 ${results.length} 处`);
    pageCards.forEach((card, index) => { if (card) buildTextLayer(index); });
    if (activeSearchResult >= 0) goTo(results[activeSearchResult].page);
  } catch (error) {
    if (generation !== searchGeneration || isCancelledError(error)) return;
    updateSearchStatus(`搜索失败：${error.message}`);
    searchResults = [];
    activeSearchResult = -1;
  } finally {
    if (generation === searchGeneration) {
      searchRequest = undefined;
      searchButton.disabled = pageInfos.length === 0;
      updateNavigation();
    }
  }
}

function moveSearchResult(step) {
  if (!searchResults.length) return;
  activeSearchResult = (activeSearchResult + step + searchResults.length) % searchResults.length;
  updateSearchStatus(`第 ${activeSearchResult + 1} / ${searchResults.length} 处`);
  pageCards.forEach((card, index) => { if (card) buildTextLayer(index); });
  goTo(searchResults[activeSearchResult].page);
}

function escapeHTML(value) {
  return value.replace(/[&<>'"]/g, character => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#39;', '"': '&quot;',
  }[character]));
}

function safeDownloadName(value) {
  return (value || 'ofd-document')
    .replace(/[/:*?"<>|\x00-\x1f]/g, '_')
    .replace(/\.+$/, '')
    .slice(0, 120) || 'ofd-document';
}

function setExportProgress(completed, total) {
  renderProgress.max = total;
  renderProgress.value = completed;
  renderProgressLabel.textContent = `已导出 ${completed}/${total}`;
  renderProgress.hidden = false;
  renderProgressLabel.hidden = false;
}

function exportIndexes() {
  if (exportRange.value === 'current') return [current];
  if (exportRange.value === 'all') return pageInfos.map((_, index) => index);
  return parsePrintRange(exportCustomRange.value, '导出');
}

function updateExportRangeControl() {
  exportCustomRange.disabled = exportRange.value !== 'custom';
  exportError.hidden = true;
}

function updateExportFormatControl() {
  const text = exportFormat.value === 'txt';
  exportDPI.disabled = text;
  exportBackground.disabled = text;
  exportBackgroundField.hidden = text;
}

function openExportDialog() {
  if (!pageInfos.length || documentActionBusy) return;
  exportRange.value = 'current';
  exportCustomRange.value = '';
  exportDPI.value = '150';
  exportFormat.value = 'png';
  exportBackground.value = 'transparent';
  exportError.hidden = true;
  if (typeof exportDialog.showModal === 'function') exportDialog.showModal();
  else exportDialog.setAttribute('open', '');
  updateExportRangeControl();
  updateExportFormatControl();
}

function closeExportDialog() {
  if (typeof exportDialog.close === 'function') exportDialog.close();
  else exportDialog.removeAttribute('open');
}

function openAboutDialog() {
  if (typeof aboutDialog.showModal === 'function') aboutDialog.showModal();
  else aboutDialog.setAttribute('open', '');
  aboutClose.focus();
}

function closeAboutDialog() {
  if (typeof aboutDialog.close === 'function') aboutDialog.close();
  else aboutDialog.removeAttribute('open');
}

function setInfoPanelOpen(open) {
  const show = !!open;
  infoPanel.hidden = !show;
  infoToggle.setAttribute('aria-expanded', String(show));
  if (show) updateDocumentInfo();
}

function updateDocumentInfo() {
  if (!pageInfos.length) {
    infoBody.innerHTML = '<p class="info-empty">未打开文档</p>';
    return;
  }
  const generation = documentGeneration;
  loadDocumentInfo().then(info => {
    if (generation !== documentGeneration || !info) return;
    const rows = [];
    if (info.docID) rows.push(['文档标识', info.docID]);
    if (info.title) rows.push(['标题', info.title]);
    if (info.author) rows.push(['作者', info.author]);
    if (info.subject) rows.push(['主题', info.subject]);
    if (info.abstract) rows.push(['摘要', info.abstract]);
    if (info.creationDate) rows.push(['创建时间', info.creationDate]);
    if (info.modDate) rows.push(['修改时间', info.modDate]);
    if (info.creator) rows.push(['创建软件', info.creator]);
    if (info.version) rows.push(['OFD 版本', info.version]);
    rows.push(['页数', String(pageInfos.length)]);
    const fonts = Array.isArray(info.fonts) ? info.fonts : [];
    let html = rows.map(([label, value]) =>
      `<div class="info-row"><span class="info-label">${label}</span><span class="info-value">${escapeHTML(value)}</span></div>`
    ).join('');
    html += '<div class="info-section-title">字体</div>';
    if (!fonts.length) {
      html += '<p class="info-empty">文档未声明字体</p>';
    } else {
      html += fonts.map(font => {
        const badges = [font.embedded ? '嵌入' : '逻辑'];
        if (font.bold) badges.push('粗体');
        if (font.italic) badges.push('斜体');
        if (font.serif) badges.push('衬线');
        if (font.fixed_width) badges.push('等宽');
        if (font.format) badges.push(String(font.format).toUpperCase());
        const name = font.name || font.family || `字体 ${font.id}`;
        const family = font.family && font.family !== font.name
          ? `<span class="font-family">${escapeHTML(font.family)}</span>` : '';
        return `<div class="font-item"><span class="font-name">${escapeHTML(name)}</span>${family}` +
          `<span class="font-badges">${badges.map(badge => `<span class="font-badge">${escapeHTML(badge)}</span>`).join('')}</span></div>`;
      }).join('');
    }
    infoBody.innerHTML = html;
    engine.stats().then(stats => {
      if (generation !== documentGeneration || !stats) return;
      const title = document.createElement('div');
      title.className = 'info-section-title';
      title.textContent = '资源统计';
      const box = document.createElement('div');
      [['字体', stats.fonts], ['附件', stats.attachments], ['多媒体', stats.media], ['注解页', stats.annotation_pages], ['签名', stats.signatures]]
        .forEach(([label, value]) => {
          const row = document.createElement('div');
          row.className = 'info-row';
          const name = document.createElement('span');
          name.className = 'info-label';
          name.textContent = label;
          const count = document.createElement('span');
          count.className = 'info-value';
          count.textContent = String(value ?? 0);
          row.append(name, count);
          box.append(row);
        });
      infoBody.append(title, box);
    }).catch(() => {});
  }).catch(() => {
    if (generation !== documentGeneration) return;
    infoBody.innerHTML = '<p class="info-empty">获取信息失败</p>';
  });
}

function escapeHTML(text) {
  const div = document.createElement('div');
  div.textContent = text;
  return div.innerHTML;
}

function exportBackgroundColor() {
  if (exportBackground.value === 'white') return '#ffffff';
  if (exportBackground.value === 'black') return '#000000';
  return '#00000000';
}

function imageDataBlob(data, type) {
  return new Blob([data], { type });
}

async function decodePNG(data) {
  const blob = imageDataBlob(data, 'image/png');
  if (typeof createImageBitmap === 'function') {
    const bitmap = await createImageBitmap(blob);
    return { source: bitmap, width: bitmap.width, height: bitmap.height, close: () => bitmap.close() };
  }
  const url = URL.createObjectURL(blob);
  const image = await new Promise((resolve, reject) => {
    const value = new Image();
    value.onload = () => resolve(value);
    value.onerror = () => {
      URL.revokeObjectURL(url);
      reject(new Error('PNG 图片解码失败'));
    };
    value.src = url;
  });
  return {
    source: image,
    width: image.naturalWidth,
    height: image.naturalHeight,
    close: () => URL.revokeObjectURL(url),
  };
}

async function convertImageFormat(data, format, background) {
  if (format === 'png') return imageDataBlob(data, 'image/png');
  const image = await decodePNG(data);
  try {
    const canvas = document.createElement('canvas');
    canvas.width = image.width;
    canvas.height = image.height;
    const context = canvas.getContext('2d');
    if (!context) throw new Error('浏览器不支持图像导出');
    context.fillStyle = background === '#00000000' ? '#ffffff' : background;
    context.fillRect(0, 0, canvas.width, canvas.height);
    context.drawImage(image.source, 0, 0);
    return new Promise((resolve, reject) => {
      canvas.toBlob(blob => blob ? resolve(blob) : reject(new Error('JPG 图片编码失败')), 'image/jpeg', 0.95);
    });
  } finally {
    image.close();
  }
}

function downloadBytes(data, name, type) {
  const url = URL.createObjectURL(new Blob([data], { type }));
  const link = document.createElement('a');
  link.href = url;
  link.download = name;
  document.body.append(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 0);
}

function exportFileName(baseName, indexes, format) {
  if (format === 'pdf') return `${baseName}-document.pdf`;
  if (format === 'txt') return `${baseName}-document.txt`;
  if (indexes.length === 1) return `${baseName}-page-${String(indexes[0] + 1).padStart(4, '0')}.${format}`;
  return `${baseName}-导出.zip`;
}

async function chooseSaveFile(name, type) {
  if (typeof window.showSaveFilePicker !== 'function') return undefined;
  const extension = `.${name.split('.').pop()}`;
  try {
    return await window.showSaveFilePicker({
      suggestedName: name,
      types: [{ description: type.description, accept: { [type.mime]: [extension] } }],
    });
  } catch (error) {
    if (error?.name === 'AbortError') return null;
    throw error;
  }
}

function exportMimeType(format) {
  switch (format) {
    case 'pdf': return { description: 'PDF 文档', mime: 'application/pdf' };
    case 'txt': return { description: '文本文件', mime: 'text/plain' };
    case 'jpg': return { description: 'JPG 图片', mime: 'image/jpeg' };
    case 'png': return { description: 'PNG 图片', mime: 'image/png' };
    default: return { description: 'ZIP 压缩包', mime: 'application/zip' };
  }
}

async function saveBytes(data, name, type, saveFile, isCancelled = () => false) {
  const sink = await openSaveSink(name, type, saveFile);
  try {
    const bytes = data instanceof Uint8Array ? data : new Uint8Array(data);
    const chunkSize = 1 << 20;
    for (let offset = 0; offset < bytes.length; offset += chunkSize) {
      if (isCancelled()) throw new Error('导出已取消');
      await sink.write(bytes.subarray(offset, Math.min(offset + chunkSize, bytes.length)));
      setStatus(`正在保存 ${name}...（${Math.round(Math.min(offset + chunkSize, bytes.length) / bytes.length * 100)}%）`);
    }
    await sink.close();
  } catch (error) {
    await sink.abort();
    throw error;
  }
}

async function openSaveSink(name, type, saveFile) {
  if (!saveFile) {
    const chunks = [];
    return {
      write(data) {
        chunks.push(data instanceof Uint8Array ? data.slice() : new Uint8Array(data));
        return Promise.resolve();
      },
      close() {
        downloadBytes(chunks, name, type);
        return Promise.resolve();
      },
      abort() {
        chunks.length = 0;
        return Promise.resolve();
      },
    };
  }
  const writable = await saveFile.createWritable();
  let closed = false;
  return {
    write(data) {
      return writable.write(data);
    },
    async close() {
      if (closed) return;
      closed = true;
      await writable.close();
    },
    async abort() {
      if (closed) return;
      closed = true;
      try {
        await writable.abort();
      } catch (_) {
      }
    },
  };
}

async function saveRenderStream(render, indexes, options, name, type, saveFile, isCancelled) {
  const sink = await openSaveSink(name, type, saveFile);
  let writeChain = Promise.resolve();
  let streamError;
  let activeRequest;
  const writeChunk = (value, sequence, requestID) => {
    if (streamError) return;
    const chunk = value instanceof Uint8Array ? value : new Uint8Array(value);
    writeChain = writeChain.then(async () => {
      try {
        if (isCancelled()) throw new Error('导出已取消');
        await sink.write(chunk);
        engine.streamAck(requestID, sequence);
      } catch (error) {
        streamError ||= error;
        engine.streamAck(requestID, sequence, error);
        activeRequest?.cancel();
      }
    });
  };
  try {
    activeRequest = render(indexes, options, writeChunk);
    await activeRequest;
    await writeChain;
    if (streamError) throw streamError;
    if (isCancelled()) throw new Error('导出已取消');
    await sink.close();
  } catch (error) {
    activeRequest?.cancel();
    await sink.abort();
    throw error;
  }
}

async function exportDocumentPages(indexes, dpi, format, background, saveFile) {
  const generation = documentGeneration;
  const files = [];
  const textParts = [];
  let activeRequest;
  let cancelled = false;
  const requestState = { cancel: () => { cancelled = true; activeRequest?.cancel(); } };
  exportRequest = requestState;
  const baseName = safeDownloadName(documentName.textContent);
  try {
    if (format === 'pdf') {
      await saveRenderStream(
         (pages, options, onChunk) => engine.renderStream(pages, { ...options, format: 'pdf' }, onChunk),
        indexes,
        { dpi, background },
        `${baseName}-document.pdf`,
        'application/pdf',
        saveFile,
        () => cancelled || documentActionCancelRequested,
      );
      setExportProgress(indexes.length, indexes.length);
      setStatus(`导出完成，共 ${indexes.length} 页。`);
      return;
    }
    if (format !== 'txt' && indexes.length > 1) {
      await saveRenderStream(
         (pages, options, onChunk) => engine.renderStream(pages, options, onChunk),
        indexes,
        { dpi, background, format },
        `${baseName}-导出.zip`,
        'application/zip',
        saveFile,
        () => cancelled || documentActionCancelRequested,
      );
      setExportProgress(indexes.length, indexes.length);
      setStatus(`导出完成，共 ${indexes.length} 页。`);
      return;
    }
    for (let position = 0; position < indexes.length; position++) {
      throwIfDocumentActionCancelled(generation);
      if (cancelled) throw new Error('导出已取消');
      const index = indexes[position];
      setStatus(`正在导出第 ${index + 1} / ${indexes.length} 页...`);
      if (format === 'txt') {
        await loadText(index, true);
        if (!textCache.has(index)) throw new Error(`第 ${index + 1} 页文字读取失败`);
        const text = pageText(index);
        if (text) textParts.push(text);
      } else {
        activeRequest = engine.renderPage(index, { dpi, background });
        const data = await activeRequest;
        const blob = await convertImageFormat(new Uint8Array(data), format, background);
        const file = { name: `page-${String(index + 1).padStart(4, '0')}.${format}`, data: new Uint8Array(await blob.arrayBuffer()) };
        files.push(file);
      }
      setExportProgress(position + 1, indexes.length);
    }
    throwIfDocumentActionCancelled(generation);
    if (format === 'txt') {
      if (!textParts.length) throw new Error('选中的页面没有可导出的文字');
      files.push({ name: 'document.txt', data: new TextEncoder().encode(textParts.join('\n\n')) });
    }
    if (files.length === 1) {
      const type = format === 'txt' ? 'text/plain;charset=utf-8' : format === 'jpg' ? 'image/jpeg' : format === 'pdf' ? 'application/pdf' : 'image/png';
      await saveBytes(files[0].data, `${baseName}-${files[0].name}`, type, saveFile, () => cancelled || documentActionCancelRequested);
    }
    setStatus(`导出完成，共 ${indexes.length} 页。`);
  } finally {
    activeRequest = undefined;
    if (exportRequest === requestState) exportRequest = undefined;
  }
}

async function startExport() {
  if (!pageInfos.length || documentActionBusy) return;
  const generation = documentGeneration;
  let indexes;
  let dpi;
  try {
    indexes = exportIndexes();
  } catch (error) {
    exportError.textContent = error.message;
    exportError.hidden = false;
    if (exportRange.value === 'custom') exportCustomRange.focus();
    return;
  }
  dpi = Number(exportDPI.value);
  if (!Number.isInteger(dpi) || dpi < 1 || dpi > 1200) {
    exportError.textContent = 'DPI 必须是 1-1200 之间的整数';
    exportError.hidden = false;
    return;
  }
  if (!indexes.length) {
    exportError.textContent = '请选择至少一页';
    exportError.hidden = false;
    return;
  }
  const baseName = safeDownloadName(documentName.textContent);
  let saveFile;
  try {
    saveFile = await chooseSaveFile(exportFileName(baseName, indexes, exportFormat.value), exportMimeType(exportFormat.value));
    if (saveFile === null) {
      setStatus('已取消保存。');
      return;
    }
  } catch (error) {
    exportError.textContent = `选择保存位置失败：${error.message}`;
    exportError.hidden = false;
    return;
  }
  closeExportDialog();
  setDocumentActionBusy(true);
  exportActive = true;
  setExportProgress(0, indexes.length);
  try {
    await exportDocumentPages(indexes, dpi, exportFormat.value, exportBackgroundColor(), saveFile);
  } catch (error) {
    if (generation === documentGeneration) {
      if (documentActionCancelRequested || isCancelledError(error)) setStatus('已取消导出。');
      else setStatus(`导出失败：${error.message}`);
    }
  } finally {
    if (generation === documentGeneration) {
      exportActive = false;
      setDocumentActionBusy(false);
    }
  }
}

function pageText(index) {
  const runs = textCache.get(index) || [];
  if (!runs.length) return '';
  const ordered = runs
    .filter(run => run.text)
    .map((run, index) => ({ run, index }))
    .sort((left, right) => left.run.y - right.run.y || left.run.x - right.run.x || left.index - right.index);
  const lines = [];
  for (const entry of ordered) {
    const previous = lines[lines.length - 1];
    const tolerance = Math.max(1, Math.min(entry.run.height || 1, previous?.height || entry.run.height || 1) * 0.5);
    if (previous && Math.abs(entry.run.y - previous.y) <= tolerance) {
      previous.parts.push(entry.run.text);
      previous.y = (previous.y + entry.run.y) / 2;
      previous.height = Math.max(previous.height, entry.run.height || 0);
    } else {
      lines.push({ y: entry.run.y, height: entry.run.height || 0, parts: [entry.run.text] });
    }
  }
  return lines.map(line => line.parts.join('')).join('\n').trim();
}

async function copyTextToClipboard(text) {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return;
    }
  } catch (_) {
  }
  const textarea = document.createElement('textarea');
  textarea.value = text;
  textarea.setAttribute('readonly', '');
  textarea.style.position = 'fixed';
  textarea.style.opacity = '0';
  document.body.append(textarea);
  textarea.select();
  const copied = document.execCommand('copy');
  textarea.remove();
  if (!copied) throw new Error('Copy command failed');
}

async function copyCurrentPageText() {
  if (!pageInfos.length) return;
  if (documentActionBusy) return;
  setDocumentActionBusy(true);
  const generation = documentGeneration;
  const index = current;
  try {
    setStatus(`正在读取第 ${index + 1} 页文字...`);
    await loadText(index, true);
    throwIfDocumentActionCancelled(generation);
    const text = pageText(index);
    if (!text) {
      showCopyFeedback('当前页没有可复制的文字。');
      return;
    }
    await copyTextToClipboard(text);
  } catch (_) {
    if (documentActionCancelRequested) {
      setStatus('已取消复制当前页文字。');
      return;
    }
    showCopyFeedback('复制失败，请检查浏览器剪贴板权限。');
    return;
  } finally {
    setDocumentActionBusy(false);
  }
  showCopyFeedback(`已复制第 ${index + 1} 页文字。`);
}

async function collectDocumentText(generation) {
  const parts = [];
  let failed = 0;
  for (let index = 0; index < pageInfos.length; index += 1) {
    throwIfDocumentActionCancelled(generation);
    setStatus(`正在读取第 ${index + 1} / ${pageInfos.length} 页文字...`);
    try { await loadText(index, true); } catch (_) {}
    throwIfDocumentActionCancelled(generation);
    if (!textCache.has(index)) { failed += 1; continue; }
    const text = pageText(index);
    if (text) parts.push(text);
  }
  for (const [, request] of textRequests.entries()) request.textPinned = false;
  return { text: parts.join('\n\n'), failed };
}

async function copyDocumentText() {
  if (!pageInfos.length) return;
  if (documentActionBusy) return;
  setDocumentActionBusy(true);
  try {
    const { text, failed } = await collectDocumentText(documentGeneration);
    if (!text) {
      showCopyFeedback(failed ? `全文读取失败：${failed} 页无法读取。` : '文档没有可复制的文字。');
      return;
    }
    await copyTextToClipboard(text);
    showCopyFeedback(failed ? `已复制全文，另有 ${failed} 页读取失败。` : `已复制全文 ${text.length} 个字符。`);
  } catch (_) {
    showCopyFeedback(documentActionCancelRequested ? '已取消复制全文。' : '复制失败，请检查浏览器剪贴板权限。');
  } finally {
    setDocumentActionBusy(false);
  }
}

function parsePrintRange(value, label = '打印') {
  const indexes = new Set();
  for (const part of value.split(',')) {
    const range = part.trim();
    if (!range) throw new Error(`${label}范围不能为空`);
    const values = range.split('-').map(item => item.trim());
    if (values.length > 2 || values.some(item => !/^\d+$/.test(item))) {
      throw new Error(`${label}范围格式无效：${range}`);
    }
    const start = Number(values[0]);
    const end = values.length === 2 ? Number(values[1]) : start;
    if (!Number.isSafeInteger(start) || !Number.isSafeInteger(end) ||
        start < 1 || end < start || end > pageInfos.length) {
      throw new Error(`${label}页码必须在 1 到 ${pageInfos.length} 之间`);
    }
    for (let page = start; page <= end; page++) indexes.add(page - 1);
  }
  return [...indexes];
}

function selectedPrintIndexes() {
  const range = printForm.elements.namedItem('print-range').value;
  if (range === 'current') return [current];
  if (range === 'all') return pageInfos.map((_, index) => index);
  return parsePrintRange(printCustomRange.value);
}

function updatePrintRangeControl() {
  const custom = printForm.elements.namedItem('print-range').value === 'custom';
  printCustomRange.disabled = !custom;
  if (!custom) printError.hidden = true;
}

function openPrintDialog() {
  if (!pageInfos.length || documentActionBusy) return;
  printCurrentLabel.textContent = `当前页（第 ${current + 1} 页）`;
  printError.hidden = true;
  if (typeof printDialog.showModal === 'function') {
    printDialog.showModal();
  } else {
    printDialog.setAttribute('open', '');
  }
  updatePrintRangeControl();
}

function closePrintDialog() {
  if (typeof printDialog.close === 'function') printDialog.close();
  else printDialog.removeAttribute('open');
}

function waitForPrintImages(images) {
  return Promise.all(images.map(image => {
    if (image.complete && image.naturalWidth > 0) return undefined;
    return new Promise((resolve, reject) => {
      image.onload = resolve;
      image.onerror = () => reject(new Error('打印图片加载失败'));
    });
  }));
}

async function printSelectedPages(indexes) {
  if (!pageInfos.length) return;
  if (documentActionBusy) return;
  const printWindow = window.open('', '_blank', 'popup,width=900,height=1200');
  if (!printWindow) {
    setStatus('打印窗口被浏览器阻止，请允许弹出窗口后重试。');
    return;
  }
  printWindow.document.write('<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><title>正在准备打印...</title></head><body><p>正在准备打印...</p></body></html>');
  printWindow.document.close();
  setDocumentActionBusy(true);
  const generation = documentGeneration;
  setStatus(`正在准备 ${indexes.length} 页打印内容...`);
  try {
    const images = await Promise.all(indexes.map(async index => {
      const image = document.createElement('img');
      const src = await loadImage(index, 'page', generation, image);
      if (!src) throw new Error(`第 ${index + 1} 页尚未渲染完成`);
      return { index, src };
    }));
    throwIfDocumentActionCancelled(generation);
    const title = escapeHTML(documentName.textContent || 'OFD 文档');
    const imageMarkup = images.map(image =>
      `<section class="page"><img src="${escapeHTML(image.src)}" alt="第 ${image.index + 1} 页"></section>`
    ).join('');
    printWindow.document.open();
    printWindow.document.write(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><title>${title} - 打印</title><style>@page{margin:0}html,body{margin:0}.page{display:flex;min-height:100vh;align-items:center;justify-content:center;break-after:page;page-break-after:always}.page:last-child{break-after:auto;page-break-after:auto}img{display:block;max-width:100%;max-height:100vh;object-fit:contain}</style></head><body>${imageMarkup}</body></html>`);
    printWindow.document.close();
    printWindow.onload = async () => {
      try {
        await waitForPrintImages([...printWindow.document.images]);
      } catch (error) {
        setStatus(`打印图片准备失败：${error.message}`);
        return;
      }
      printWindow.focus();
      printWindow.print();
    };
    setStatus(`已准备 ${indexes.length} 页打印。`);
  } catch (error) {
    printWindow.close();
    setStatus(documentActionCancelRequested ? '已取消打印。' : `打印准备失败：${error.message}`);
  } finally {
    setDocumentActionBusy(false);
  }
}

function setSearchPanelOpen(open) {
  if (open) headerHeight();
  searchPanel.hidden = !open;
  searchToggle.setAttribute('aria-expanded', String(open));
  if (open) {
    setRecentPanelOpen(false);
    setViewPanelOpen(false);
    setZoomMenuOpen(false);
    setDocumentMenuOpen(false);
    searchInput.focus();
  }
}

function setReadingMode(enabled) {
  document.body.classList.toggle('reading-mode', enabled);
  readingMode.setAttribute('aria-pressed', String(enabled));
  const readingLabel = enabled ? '退出阅读' : '阅读模式';
  readingMode.title = readingLabel;
  readingMode.setAttribute('aria-label', readingLabel);
  if (enabled && !document.fullscreenElement && document.documentElement.requestFullscreen) {
    document.documentElement.requestFullscreen().catch(() => {});
  } else if (!enabled && document.fullscreenElement && document.exitFullscreen) {
    document.exitFullscreen().catch(() => {});
  }
}

function setViewPanelOpen(open) {
  viewPanel.hidden = !open;
  viewToggle.setAttribute('aria-expanded', String(open));
  if (open) {
    setRecentPanelOpen(false);
    setSearchPanelOpen(false);
    setZoomMenuOpen(false);
    setDocumentMenuOpen(false);
  }
}

function setZoomMenuOpen(open) {
  if (!zoomMenu || !zoomMenuToggle) return;
  zoomMenu.hidden = !open;
  zoomMenuToggle.setAttribute('aria-expanded', String(open));
  if (open) {
    setRecentPanelOpen(false);
    setSearchPanelOpen(false);
    setViewPanelOpen(false);
    setDocumentMenuOpen(false);
  }
}

function setDocumentMenuOpen(open) {
  if (!documentMenu || !documentMenuToggle) return;
  documentMenu.hidden = !open;
  documentMenuToggle.setAttribute('aria-expanded', String(open));
  if (open) {
    setRecentPanelOpen(false);
    setSearchPanelOpen(false);
    setViewPanelOpen(false);
    setZoomMenuOpen(false);
  }
}

function setMobileToolbarExpanded(expanded) {
  const header = document.querySelector('header');
  header?.classList.toggle('mobile-toolbar-expanded', expanded);
  if (expanded) {
    setRecentPanelOpen(false);
    setSearchPanelOpen(false);
    setViewPanelOpen(false);
  }
  requestAnimationFrame(() => headerHeight());
  mobileToolbarToggle?.setAttribute('aria-expanded', String(expanded));
  if (mobileToolbarToggle) {
    mobileToolbarToggle.title = expanded ? '收起工具栏' : '展开工具栏';
    mobileToolbarToggle.setAttribute('aria-label', expanded ? '收起工具栏' : '展开工具栏');
    const icon = mobileToolbarToggle.querySelector('.material-symbols-outlined');
    if (icon) icon.textContent = expanded ? 'expand_less' : 'expand_more';
  }
}

function readSidebarScroll() {
  try {
    const all = JSON.parse(localStorage.getItem(sidebarScrollStorageKey) || '{}');
    const state = currentDocumentKey ? all[currentDocumentKey] : null;
    return state && typeof state === 'object' ? state : {};
  } catch (_) {
    return {};
  }
}

function persistSidebarScroll() {
  clearTimeout(sidebarScrollPersistTimer);
  sidebarScrollPersistTimer = setTimeout(() => {
    if (!currentDocumentKey) return;
    try {
      const all = JSON.parse(localStorage.getItem(sidebarScrollStorageKey) || '{}');
      all[currentDocumentKey] = sidebarScroll;
      const entries = Object.entries(all).slice(-20);
      localStorage.setItem(sidebarScrollStorageKey, JSON.stringify(Object.fromEntries(entries)));
    } catch (_) {}
  }, 300);
}

function restorePanelScroll(panel) {
  const element = sidebarScrollPanels[panel];
  if (element) element.scrollTop = sidebarScroll[panel] || 0;
}

const sidebarTabs = ['thumbnails', 'outline', 'bookmarks', 'fonts', 'attachments', 'media', 'annotations', 'signatures'];
const sidebarMoreTabs = ['fonts', 'attachments', 'media', 'annotations', 'signatures'];
const sidebarMoreLabels = { fonts: '字体', attachments: '附件', media: '资源', annotations: '注解', signatures: '签名' };

// applySidebarPanels 根据侧栏可见性和当前面板，决定缩略图/大纲/书签/字体/附件面板的显隐。
function applySidebarPanels() {
  if (!sidebarElement) return;
  if (!sidebarTabs.includes(activeSidebarTab)) activeSidebarTab = 'thumbnails';
  sidebarElement.hidden = !thumbnailsVisible;
  const active = tab => thumbnailsVisible && activeSidebarTab === tab;
  thumbnailsElement.hidden = !active('thumbnails');
  if (outlineElement) outlineElement.hidden = !active('outline');
  if (bookmarksElement) bookmarksElement.hidden = !active('bookmarks');
  if (fontsElement) fontsElement.hidden = !active('fonts');
  if (attachmentsElement) attachmentsElement.hidden = !active('attachments');
  if (mediaElement) mediaElement.hidden = !active('media');
  if (annotationsElement) annotationsElement.hidden = !active('annotations');
  if (signaturesElement) signaturesElement.hidden = !active('signatures');
  if (thumbnailToolbar) thumbnailToolbar.hidden = !active('thumbnails');
  if (outlineToolbar) outlineToolbar.hidden = !active('outline');
  if (outlineExpandAll) outlineExpandAll.disabled = Boolean(sidebarFilterValue);
  if (outlineCollapseAll) outlineCollapseAll.disabled = Boolean(sidebarFilterValue);
  if (sidebarFilter) {
    const filterable = active('outline') || active('bookmarks') || active('fonts') || active('attachments') || active('media') || active('annotations') || active('signatures');
    sidebarFilter.hidden = !filterable;
    if (active('fonts')) sidebarFilter.placeholder = '过滤字体';
    else if (active('attachments')) sidebarFilter.placeholder = '过滤附件';
    else if (active('media')) sidebarFilter.placeholder = '过滤资源';
    else if (active('annotations')) sidebarFilter.placeholder = '过滤注解';
    else if (active('signatures')) sidebarFilter.placeholder = '过滤签名';
    else sidebarFilter.placeholder = '过滤标题';
  }
  const tabs = [
    [sidebarTabThumbnails, 'thumbnails'],
    [sidebarTabOutline, 'outline'],
    [sidebarTabBookmarks, 'bookmarks'],
  ];
  tabs.forEach(([button, tab]) => {
    button?.classList.toggle('active', activeSidebarTab === tab);
    button?.setAttribute('aria-selected', String(activeSidebarTab === tab));
  });
  const moreActive = sidebarMoreTabs.includes(activeSidebarTab);
  sidebarTabMore?.classList.toggle('active', moreActive);
  sidebarTabMore?.setAttribute('aria-selected', String(moreActive));
  if (sidebarTabMoreLabel) {
    sidebarTabMoreLabel.textContent = moreActive ? (sidebarMoreLabels[activeSidebarTab] || '更多') : '更多';
  }
  sidebarMoreFonts?.classList.toggle('active', activeSidebarTab === 'fonts');
  sidebarMoreAttachments?.classList.toggle('active', activeSidebarTab === 'attachments');
  sidebarMoreMedia?.classList.toggle('active', activeSidebarTab === 'media');
  sidebarMoreAnnotations?.classList.toggle('active', activeSidebarTab === 'annotations');
  sidebarMoreSignatures?.classList.toggle('active', activeSidebarTab === 'signatures');
  if (sidebarMoreMenu && thumbnailsVisible === false) setSidebarMoreOpen(false);
  if (thumbnailSizeSlider) thumbnailSizeSlider.value = String(thumbnailSizePercent);
}

function setSidebarMoreOpen(open) {
  if (!sidebarMoreMenu) return;
  sidebarMoreMenu.hidden = !open;
  sidebarTabMore?.setAttribute('aria-expanded', String(open));
  if (open) {
    setViewPanelOpen(false);
    setSearchPanelOpen(false);
    setZoomMenuOpen(false);
    setDocumentMenuOpen(false);
  }
}

function setSidebarTab(tab) {
  if (!sidebarTabs.includes(tab)) tab = 'thumbnails';
  const changed = activeSidebarTab !== tab;
  if ((activeSidebarTab === 'media' || activeSidebarTab === 'signatures') && tab !== activeSidebarTab) revokeMediaURLs();
  activeSidebarTab = tab;
  if (changed) {
    try {
      localStorage.setItem(sidebarTabStorageKey, tab);
    } catch (_) {}
  }
  if (sidebarMoreTabs.includes(tab)) setSidebarMoreOpen(false);
  applySidebarPanels();
  if (tab === 'thumbnails') {
    updateThumbnailMetrics();
    scheduleVirtualUpdate();
  } else if (tab === 'outline') {
    renderOutline();
    updateOutlineActive();
  } else if (tab === 'bookmarks') {
    renderBookmarks();
    updateOutlineActive();
  } else if (tab === 'attachments') {
    renderAttachments();
  } else if (tab === 'media') {
    renderMedia();
  } else if (tab === 'annotations') {
    renderAnnotations();
  } else if (tab === 'signatures') {
    renderSignatures();
  } else {
    renderFonts();
  }
}

function toggleSidebar() {
  setThumbnailsVisible(!thumbnailsVisible);
}

// cycleSidebarPanel 在侧栏面板之间循环切换（含“更多”里的字体/附件/资源/注解/签名）。
function cycleSidebarPanel(step) {
  if (!sidebarTabs.length) return;
  if (!thumbnailsVisible) setThumbnailsVisible(true);
  const index = sidebarTabs.indexOf(activeSidebarTab);
  const next = sidebarTabs[(index + step + sidebarTabs.length) % sidebarTabs.length];
  setSidebarTab(next);
}

function setThumbnailSize(percent) {
  const value = Math.max(40, Math.min(100, Math.round(Number(percent) || 100)));
  if (value === thumbnailSizePercent) return;
  thumbnailSizePercent = value;
  updateThumbnailMetrics();
  scheduleVirtualUpdate();
}

function persistThumbnailSize() {
  try {
    localStorage.setItem(thumbnailSizeStorageKey, String(thumbnailSizePercent));
  } catch (_) {}
}

function applySidebarWidth() {
  if (!readerElement) return;
  readerElement.style.setProperty('--thumbnail-column', `${sidebarWidth}px`);
}

function setSidebarWidth(value) {
  const max = Math.max(180, Math.min(520, window.innerWidth - 320));
  sidebarWidth = Math.max(140, Math.min(max, Math.round(value)));
  applySidebarWidth();
}

function persistSidebarWidth() {
  try {
    localStorage.setItem(sidebarWidthStorageKey, String(sidebarWidth));
  } catch (_) {}
}

function finishSidebarResize() {
  persistSidebarWidth();
  updateThumbnailMetrics();
  updatePageVirtualMetrics();
  if (zoomMode === 'fit') fitWidthZoom();
  else if (zoomMode === 'page') fitPageZoom();
  scheduleVirtualUpdate();
}

function resetSidebarFilter() {
  sidebarFilterValue = '';
  if (sidebarFilter) sidebarFilter.value = '';
}

function setThumbnailsVisible(visible) {
  thumbnailsVisible = visible;
  showThumbnails.checked = visible;
  applySidebarPanels();
  readerElement.classList.toggle('hide-thumbnails', !visible);
  document.body.classList.toggle('hide-thumbnails', !visible);
  try {
    localStorage.setItem(thumbnailsStorageKey, String(visible));
  } catch (_) {}
  if (!visible) setViewPanelOpen(false);
  updateThumbnailMetrics();
  scheduleVirtualUpdate();
  requestAnimationFrame(() => {
    if (zoomMode === 'fit') fitWidthZoom();
    else if (zoomMode === 'page') fitPageZoom();
  });
}

function hasSavedPreference(key) {
  try {
    return localStorage.getItem(key) != null;
  } catch (_) {
    return false;
  }
}

// 文档声明的页面布局到阅读器布局的近似映射；未识别时返回空串。
function mapDocumentPageLayout(value) {
  switch (String(value)) {
    case 'OnePage':
    case 'OneColumn':
      return 'single';
    case 'TwoPageL':
    case 'TwoColumnL':
      return 'double-odd-left';
    case 'TwoPageR':
    case 'TwoColumnR':
      return 'double';
    default:
      return '';
  }
}

// 文档声明的缩放模式映射；FitHeight/FitRect 暂用“适应页面”近似。
function mapDocumentZoomMode(value) {
  switch (String(value)) {
    case 'FitWidth':
      return 'fit';
    case 'FitHeight':
    case 'FitRect':
      return 'page';
    default:
      return '';
  }
}

function normalizeDocumentZoom(value) {
  let zoomValue = Number(value);
  if (!Number.isFinite(zoomValue) || zoomValue <= 0) return 0;
  if (zoomValue > 5) zoomValue /= 100;
  return Math.max(0.5, Math.min(3, zoomValue));
}

// applyDocumentPreferences 在用户没有显式保存偏好时，应用文档声明的布局与初始缩放。
async function applyDocumentPreferences() {
  let preferences = null;
  try {
    preferences = await engine.preferences();
  } catch (_) {
    return;
  }
  if (!preferences) return;
  if (!hasSavedPreference(pageLayoutStorageKey)) {
    const layout = mapDocumentPageLayout(preferences.page_layout);
    if (layout) {
      pageLayout = layout;
      pageLayoutSelect.value = layout;
    }
  }
  if (!hasSavedPreference(zoomModeStorageKey)) {
    const zoomValue = normalizeDocumentZoom(preferences.zoom);
    if (zoomValue) {
      zoom = zoomValue;
      zoomMode = 'manual';
    } else {
      const mode = mapDocumentZoomMode(preferences.zoom_mode);
      if (mode) zoomMode = mode;
    }
  }
}

async function loadOutline() {
  const generation = documentGeneration;
  let tree = null;
  try {
    tree = await engine.outline();
  } catch (_) {
    tree = null;
  }
  if (generation !== documentGeneration) return;
  outlineNodes = Array.isArray(tree?.nodes) ? tree.nodes : [];
  bookmarkNodes = Array.isArray(tree?.bookmarks) ? tree.bookmarks : [];
  outlineExpandState = readOutlineExpandState();
  applyOutlineExpandState(outlineNodes);
  renderOutline();
  renderBookmarks();
  // 文档声明的显示模式优先决定默认页签。
  const mode = String(tree?.page_mode || '');
  if (mode === 'UseOutlines' && outlineNodes.length) setSidebarTab('outline');
  else if (mode === 'UseBookmarks' && bookmarkNodes.length) setSidebarTab('bookmarks');
}

// attachOutlineKeyboard 让大纲/书签面板支持方向键、Home/End 在条目间移动焦点。
function attachOutlineKeyboard(panel) {
  if (!panel || panel.dataset.keyboardBound) return;
  panel.dataset.keyboardBound = '1';
  panel.addEventListener('keydown', event => {
    if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return;
    const items = [...panel.querySelectorAll('.outline-label:not(:disabled), .outline-toggle')]
      .filter(element => element.offsetParent !== null);
    if (!items.length) return;
    let index = items.indexOf(document.activeElement);
    if (index < 0) index = 0;
    if (event.key === 'ArrowDown') index = Math.min(items.length - 1, index + 1);
    else if (event.key === 'ArrowUp') index = Math.max(0, index - 1);
    else if (event.key === 'Home') index = 0;
    else index = items.length - 1;
    event.preventDefault();
    items[index].focus();
  });
}

function outlineEmptyMessage(text) {
  const empty = document.createElement('p');
  empty.className = 'outline-empty';
  empty.textContent = text;
  return empty;
}

function appendOutlinePageTag(row, page) {
  if (!Number.isInteger(page) || page < 0) return;
  const tag = document.createElement('span');
  tag.className = 'outline-page';
  tag.textContent = String(page + 1);
  row.append(tag);
}

// filterOutlineTree 过滤大纲树：命中节点保留整棵子树，否则保留含命中后代的节点。
function filterOutlineTree(nodes, needle) {
  const result = [];
  for (const node of nodes) {
    const children = filterOutlineTree(node.children || [], needle);
    if (String(node.title || '').toLowerCase().includes(needle)) {
      result.push(node);
    } else if (children.length) {
      result.push({ ...node, children });
    }
  }
  return result;
}

// 大纲展开状态按文档（文件标识）持久化，键为节点在树中的索引路径，如 "0.1"。
function readOutlineExpandState() {
  try {
    const all = JSON.parse(localStorage.getItem(outlineExpandStorageKey) || '{}');
    const state = currentDocumentKey ? all[currentDocumentKey] : null;
    return state && typeof state === 'object' ? state : {};
  } catch (_) {
    return {};
  }
}

function persistOutlineExpandState() {
  if (!currentDocumentKey) return;
  try {
    const all = JSON.parse(localStorage.getItem(outlineExpandStorageKey) || '{}');
    all[currentDocumentKey] = outlineExpandState;
    const entries = Object.entries(all).slice(-30);
    localStorage.setItem(outlineExpandStorageKey, JSON.stringify(Object.fromEntries(entries)));
  } catch (_) {}
}

function applyOutlineExpandState(nodes, pathPrefix = '') {
  nodes.forEach((node, index) => {
    const path = pathPrefix ? `${pathPrefix}.${index}` : String(index);
    if (Object.prototype.hasOwnProperty.call(outlineExpandState, path)) {
      node.expanded = outlineExpandState[path];
    }
    if (Array.isArray(node.children) && node.children.length) {
      applyOutlineExpandState(node.children, path);
    }
  });
}

function setAllOutlineExpanded(expanded) {
  setAllOutlineExpandedIn(outlineNodes, expanded);
  persistOutlineExpandState();
  renderOutline();
}

function setAllOutlineExpandedIn(nodes, expanded, pathPrefix = '') {
  nodes.forEach((node, index) => {
    const path = pathPrefix ? `${pathPrefix}.${index}` : String(index);
    if (Array.isArray(node.children) && node.children.length) {
      outlineExpandState[path] = expanded;
      node.expanded = expanded;
      setAllOutlineExpandedIn(node.children, expanded, path);
    }
  });
}

function renderOutline() {
  if (!outlineElement) return;
  if (outlineExpandAll) outlineExpandAll.disabled = Boolean(sidebarFilterValue);
  if (outlineCollapseAll) outlineCollapseAll.disabled = Boolean(sidebarFilterValue);
  outlineElement.replaceChildren();
  if (!outlineNodes.length) {
    outlineElement.append(outlineEmptyMessage('此文档没有大纲'));
    return;
  }
  const nodes = sidebarFilterValue ? filterOutlineTree(outlineNodes, sidebarFilterValue) : outlineNodes;
  if (!nodes.length) {
    outlineElement.append(outlineEmptyMessage('无匹配结果'));
    return;
  }
  outlineElement.append(buildOutlineList(nodes, 0, Boolean(sidebarFilterValue)));
  attachOutlineKeyboard(outlineElement);
  updateOutlineActive();
  restorePanelScroll('outline');
}

function renderBookmarks() {
  if (!bookmarksElement) return;
  bookmarksElement.replaceChildren();
  if (!bookmarkNodes.length) {
    bookmarksElement.append(outlineEmptyMessage('此文档没有书签'));
    return;
  }
  const bookmarks = sidebarFilterValue
    ? bookmarkNodes.filter(bookmark => String(bookmark.name || '').toLowerCase().includes(sidebarFilterValue))
    : bookmarkNodes;
  if (!bookmarks.length) {
    bookmarksElement.append(outlineEmptyMessage('无匹配结果'));
    return;
  }
  const list = document.createElement('ul');
  list.className = 'outline-list';
  bookmarks.forEach(bookmark => {
    const item = document.createElement('li');
    item.className = 'outline-item';
    const row = document.createElement('div');
    row.className = 'outline-row';
    const spacer = document.createElement('span');
    spacer.className = 'outline-toggle-placeholder';
    row.append(spacer);
    const label = document.createElement('button');
    label.type = 'button';
    label.className = 'outline-label';
    label.textContent = bookmark.name || '未命名书签';
    label.title = bookmark.name || '';
    if (Number.isInteger(bookmark.page) && bookmark.page >= 0) {
      label.dataset.page = String(bookmark.page);
      label.addEventListener('click', () => goToDestination(bookmark.page, bookmark.dest));
    } else {
      label.disabled = true;
    }
    row.append(label);
    appendOutlinePageTag(row, bookmark.page);
    item.append(row);
    list.append(item);
  });
  bookmarksElement.append(list);
  attachOutlineKeyboard(bookmarksElement);
  updateOutlineActive();
  restorePanelScroll('bookmarks');
}

// loadDocumentInfo 在每个文档生命周期内缓存一次 engine.info()，供信息面板和字体页签共用。
async function loadDocumentInfo() {
  if (documentInfoGeneration === documentGeneration && documentInfo) return documentInfo;
  const generation = documentGeneration;
  const info = await engine.info();
  if (generation !== documentGeneration) return null;
  documentInfo = info;
  documentInfoGeneration = generation;
  return info;
}

function buildFontItem(font) {
  const badges = [font.embedded ? '嵌入' : '逻辑'];
  if (font.bold) badges.push('粗体');
  if (font.italic) badges.push('斜体');
  if (font.serif) badges.push('衬线');
  if (font.fixed_width) badges.push('等宽');
  if (font.format) badges.push(String(font.format).toUpperCase());
  const wrap = document.createElement('div');
  wrap.className = 'font-item-wrap';
  const item = document.createElement('div');
  item.className = 'font-item';
  const name = document.createElement('span');
  name.className = 'font-name';
  name.textContent = font.name || font.family || `字体 ${font.id}`;
  item.append(name);
  if (font.family && font.family !== font.name) {
    const family = document.createElement('span');
    family.className = 'font-family';
    family.textContent = font.family;
    item.append(family);
  }
  const badgeBox = document.createElement('span');
  badgeBox.className = 'font-badges';
  badges.forEach(text => {
    const badge = document.createElement('span');
    badge.className = 'font-badge';
    badge.textContent = text;
    badgeBox.append(badge);
  });
  item.append(badgeBox);
  const usage = document.createElement('button');
  usage.type = 'button';
  usage.className = 'font-usage';
  usage.dataset.fontKey = fontUsageKey(font);
  usage.textContent = '定位使用页';
  usage.addEventListener('click', () => toggleFontUsage(font, wrap, usage));
  item.append(usage);
  wrap.append(item);
  return wrap;
}

function fontUsageLabel(usage) {
  const count = Array.isArray(usage?.pages) ? usage.pages.length : 0;
  return count ? `${count} 页` : '未使用';
}

// 字体作用域 + ID 唯一标识一个字体，避免多文档体之间字体 ID 冲突。
function fontUsageKey(font) {
  return `${Number(font?.scope) || 0}:${Number(font?.id)}`;
}

// fontUsageLimits 按文档规模选择统计扫描上限：小文档全量，大文档限制自动统计的扫描成本。
function fontUsageLimits(batch) {
  const pages = pageInfos.length || 0;
  if (!batch) {
    return { maxScan: Math.max(1, Math.min(pages || 1, 10000)), maxPages: 500 };
  }
  const maxScan = pages <= 2000 ? pages : pages <= 20000 ? 4000 : 2000;
  return { maxScan: Math.max(1, maxScan), maxPages: 500 };
}

// loadFontUsageAll 在每个文档生命周期内缓存一次批量字体使用统计。
async function loadFontUsageAll() {
  if (documentFontUsageGeneration === documentGeneration && documentFontUsage) return documentFontUsage;
  const generation = documentGeneration;
  const report = await engine.fontUsageAll(fontUsageLimits(true));
  if (generation !== documentGeneration) return null;
  const map = new Map();
  for (const entry of report?.fonts || []) {
    map.set(fontUsageKey(entry), {
      pages: Array.isArray(entry.pages) ? entry.pages : [],
      scanned: report.scanned || 0,
      truncated: !!report.truncated,
    });
  }
  documentFontUsage = map;
  documentFontUsageMeta = { scanned: report?.scanned || 0, truncated: !!report?.truncated };
  documentFontUsageGeneration = generation;
  return map;
}

function applyFontUsageLabels() {
  if (!fontsElement) return;
  const generation = documentGeneration;
  loadFontUsageAll().then(map => {
    if (generation !== documentGeneration || !map) return;
    fontsElement.querySelectorAll('.font-usage').forEach(button => {
      const usage = map.get(button.dataset.fontKey);
      if (usage && !button.classList.contains('active')) button.textContent = fontUsageLabel(usage);
    });
    if (documentFontUsageMeta.truncated && !fontsElement.querySelector('.fonts-usage-note')) {
      const note = document.createElement('p');
      note.className = 'outline-empty fonts-usage-note';
      note.textContent = `使用页统计仅覆盖前 ${documentFontUsageMeta.scanned} 页`;
      fontsElement.append(note);
    }
  }).catch(() => {});
}

// toggleFontUsage 查找使用指定字体的页面并展开页码列表；再次点击收起。
async function toggleFontUsage(font, wrap, button) {
  const generation = documentGeneration;
  const key = fontUsageKey(font);
  const existing = wrap.querySelector('.font-pages');
  if (existing) {
    existing.remove();
    button.classList.remove('active');
    const cached = documentFontUsageGeneration === generation ? documentFontUsage?.get(key) : null;
    button.textContent = cached ? fontUsageLabel(cached) : '定位使用页';
    return;
  }
  let usage = documentFontUsageGeneration === generation ? documentFontUsage?.get(key) : null;
  if (!usage) {
    button.disabled = true;
    button.textContent = '查找中…';
    try {
      usage = await engine.fontUsage(Number(font.scope) || 0, Number(font.id), fontUsageLimits(false));
    } catch (_) {
      usage = null;
    }
    if (generation !== documentGeneration) return;
    button.disabled = false;
    if (!usage) {
      button.textContent = '查找失败';
      return;
    }
    if (documentFontUsageGeneration !== generation || !documentFontUsage) {
      documentFontUsage = new Map();
      documentFontUsageGeneration = generation;
    }
    documentFontUsage.set(key, usage);
  }
  const pages = Array.isArray(usage.pages) ? usage.pages : [];
  button.classList.add('active');
  button.textContent = fontUsageLabel(usage);
  const box = document.createElement('div');
  box.className = 'font-pages';
  if (!pages.length) {
    box.append(outlineEmptyMessage(`前 ${usage.scanned || 0} 页未使用`));
    wrap.append(box);
    return;
  }
  const shown = pages.slice(0, 60);
  shown.forEach(page => {
    const pageButton = document.createElement('button');
    pageButton.type = 'button';
    pageButton.className = 'font-page';
    pageButton.textContent = String(page + 1);
    pageButton.title = `跳转到第 ${page + 1} 页`;
    pageButton.addEventListener('click', () => goTo(page));
    box.append(pageButton);
  });
  if (pages.length > shown.length) {
    const more = document.createElement('span');
    more.className = 'font-page-more';
    more.textContent = `+${pages.length - shown.length}`;
    box.append(more);
  }
  if (usage.truncated) {
    box.append(outlineEmptyMessage(`仅扫描前 ${usage.scanned} 页，可能还有更多`));
  }
  wrap.append(box);
}

function renderFonts() {
  if (!fontsElement) return;
  const generation = documentGeneration;
  fontsElement.replaceChildren();
  if (!pageInfos.length) {
    fontsElement.append(outlineEmptyMessage('未打开文档'));
    return;
  }
  fontsElement.append(outlineEmptyMessage('正在读取字体...'));
  loadDocumentInfo().then(info => {
    if (generation !== documentGeneration) return;
    fontsElement.replaceChildren();
    if (!info) {
      fontsElement.append(outlineEmptyMessage('获取字体失败'));
      return;
    }
    const fonts = Array.isArray(info.fonts) ? info.fonts : [];
    if (!fonts.length) {
      fontsElement.append(outlineEmptyMessage('文档未声明字体'));
      return;
    }
    const filtered = sidebarFilterValue
      ? fonts.filter(font => `${font.name || ''} ${font.family || ''}`.toLowerCase().includes(sidebarFilterValue))
      : fonts;
    if (!filtered.length) {
      fontsElement.append(outlineEmptyMessage('无匹配结果'));
      return;
    }
    const embedded = fonts.filter(font => font.embedded).length;
    const summary = document.createElement('p');
    summary.className = 'fonts-summary';
    summary.textContent = `共 ${fonts.length} 个字体（嵌入 ${embedded} · 逻辑 ${fonts.length - embedded}）`;
    fontsElement.append(summary);
    const list = document.createElement('div');
    list.className = 'fonts-list';
    filtered.forEach(font => list.append(buildFontItem(font)));
    fontsElement.append(list);
    applyFontUsageLabels();
    restorePanelScroll('fonts');
  }).catch(() => {
    if (generation !== documentGeneration) return;
    fontsElement.replaceChildren(outlineEmptyMessage('获取字体失败'));
  });
}

function attachmentBadge(text, extraClass) {
  const badge = document.createElement('span');
  badge.className = 'attachment-badge' + (extraClass ? ` ${extraClass}` : '');
  badge.textContent = text;
  return badge;
}

function formatAttachmentBytes(bytes) {
  if (!Number.isFinite(bytes) || bytes <= 0) return '';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

// attachmentPreviewMime 返回可在新标签预览的 MIME，不支持预览时返回空串。
function attachmentPreviewMime(item) {
  const format = String(item?.format || '').toLowerCase();
  const name = String(item?.name || '').toLowerCase();
  const ext = name.includes('.') ? name.slice(name.lastIndexOf('.') + 1) : format;
  const known = {
    pdf: 'application/pdf',
    png: 'image/png',
    jpg: 'image/jpeg',
    jpeg: 'image/jpeg',
    gif: 'image/gif',
    webp: 'image/webp',
    bmp: 'image/bmp',
    svg: 'image/svg+xml',
    txt: 'text/plain',
    xml: 'application/xml',
    json: 'application/json',
    csv: 'text/csv',
    md: 'text/markdown',
    html: 'text/html',
    htm: 'text/html',
    mp3: 'audio/mpeg',
    wav: 'audio/wav',
    ogg: 'audio/ogg',
    mp4: 'video/mp4',
    webm: 'video/webm',
  };
  return known[ext] || known[format] || '';
}

function safeResourceName(item, fallback) {
  const raw = String(item?.name || item?.id || fallback);
  const cleaned = raw.replace(/[\\/:*?"<>|\u0000-\u001f]/g, '_').replace(/^\.+/, '').trim();
  return cleaned || `${fallback}-${item?.id ?? ''}`;
}

async function fetchAttachmentData(item) {
  const maxBytes = 0;
  return engine.attachmentData(Number(item?.scope) || 0, String(item?.id ?? ''), maxBytes);
}

async function runAttachmentAction(button, action) {
  const generation = documentGeneration;
  const label = button.textContent;
  button.disabled = true;
  button.textContent = '读取中…';
  try {
    await action();
  } catch (error) {
    if (generation === documentGeneration) setStatus(`附件操作失败：${error.message}`);
  } finally {
    if (generation === documentGeneration && button.isConnected) {
      button.disabled = false;
      button.textContent = label;
    }
  }
}

async function downloadAttachment(item) {
  const data = await fetchAttachmentData(item);
  const mime = attachmentPreviewMime(item) || 'application/octet-stream';
  downloadBytes(data, safeResourceName(item, 'attachment'), mime);
}

async function previewAttachment(item) {
  const mime = attachmentPreviewMime(item);
  const data = await fetchAttachmentData(item);
  const url = URL.createObjectURL(new Blob([data], { type: mime || 'application/octet-stream' }));
  window.open(url, '_blank', 'noopener');
  setTimeout(() => URL.revokeObjectURL(url), 60_000);
}

function buildAttachmentItem(item) {
  const row = document.createElement('div');
  row.className = 'attachment-item';
  const info = document.createElement('div');
  info.className = 'attachment-info';
  const name = document.createElement('span');
  name.className = 'attachment-name';
  name.textContent = item?.name || item?.id || '未命名附件';
  name.title = name.textContent;
  const meta = document.createElement('span');
  meta.className = 'attachment-meta';
  const parts = [];
  if (item?.format) parts.push(String(item.format).toUpperCase());
  const size = formatAttachmentBytes(Number(item?.actual_size) || (item?.has_size ? Number(item?.size) : 0));
  if (size) parts.push(size);
  meta.textContent = parts.join(' · ');
  info.append(name, meta);

  const badges = document.createElement('span');
  badges.className = 'attachment-badges';
  if (!item?.visible) badges.append(attachmentBadge('隐藏', 'attachment-warn'));
  if (item?.usage && item.usage !== 'none') badges.append(attachmentBadge(String(item.usage)));
  if (!item?.exists) badges.append(attachmentBadge('文件缺失', 'attachment-warn'));

  const actions = document.createElement('span');
  actions.className = 'attachment-actions';
  const download = document.createElement('button');
  download.type = 'button';
  download.className = 'attachment-action';
  download.textContent = '下载';
  const preview = document.createElement('button');
  preview.type = 'button';
  preview.className = 'attachment-action';
  preview.textContent = '预览';
  if (!item?.exists) {
    download.disabled = true;
    preview.disabled = true;
  } else {
    download.addEventListener('click', () => runAttachmentAction(download, () => downloadAttachment(item)));
    if (attachmentPreviewMime(item)) {
      preview.addEventListener('click', () => runAttachmentAction(preview, () => previewAttachment(item)));
    } else {
      preview.hidden = true;
    }
  }
  actions.append(download);
  if (!preview.hidden) actions.append(preview);

  row.append(info, badges, actions);
  return row;
}

function renderAttachments() {
  if (!attachmentsElement) return;
  const generation = documentGeneration;
  attachmentsElement.replaceChildren();
  if (!pageInfos.length) {
    attachmentsElement.append(outlineEmptyMessage('未打开文档'));
    return;
  }
  attachmentsElement.append(outlineEmptyMessage('正在读取附件...'));
  engine.attachments().then(list => {
    if (generation !== documentGeneration) return;
    const attachments = Array.isArray(list) ? list : [];
    attachmentsElement.replaceChildren();
    if (!attachments.length) {
      attachmentsElement.append(outlineEmptyMessage('此文档没有附件'));
      return;
    }
    const filtered = sidebarFilterValue
      ? attachments.filter(item => `${item.name || ''} ${item.format || ''}`.toLowerCase().includes(sidebarFilterValue))
      : attachments;
    if (!filtered.length) {
      attachmentsElement.append(outlineEmptyMessage('无匹配结果'));
      return;
    }
    filtered.forEach(item => attachmentsElement.append(buildAttachmentItem(item)));
    restorePanelScroll('attachments');
  }).catch(() => {
    if (generation !== documentGeneration) return;
    attachmentsElement.replaceChildren(outlineEmptyMessage('获取附件失败'));
  });
}

function revokeMediaURLs() {
  mediaObserver?.disconnect();
  mediaObserver = null;
  mediaObjectURLs.forEach(url => URL.revokeObjectURL(url));
  mediaObjectURLs = [];
}

// mediaMime 根据资源名后缀或声明格式推断 MIME。
function mediaMime(item) {
  const name = String(item?.name || '').toLowerCase();
  const ext = name.includes('.') ? name.slice(name.lastIndexOf('.') + 1) : String(item?.format || '').toLowerCase();
  const known = {
    png: 'image/png',
    jpg: 'image/jpeg',
    jpeg: 'image/jpeg',
    gif: 'image/gif',
    bmp: 'image/bmp',
    webp: 'image/webp',
    svg: 'image/svg+xml',
    tif: 'image/tiff',
    tiff: 'image/tiff',
    mp3: 'audio/mpeg',
    wav: 'audio/wav',
    ogg: 'audio/ogg',
    m4a: 'audio/mp4',
    aac: 'audio/aac',
    mp4: 'video/mp4',
    webm: 'video/webm',
    mov: 'video/quicktime',
    avi: 'video/x-msvideo',
    mkv: 'video/x-matroska',
  };
  return known[ext] || '';
}

function mediaKind(item) {
  const type = String(item?.type || '').toLowerCase();
  if (type === 'audio') return 'audio';
  if (type === 'video') return 'video';
  return 'image';
}

function mediaTypeLabel(item) {
  const kind = mediaKind(item);
  return kind === 'audio' ? '音频' : kind === 'video' ? '视频' : '图片';
}

function mediaIcon(kind) {
  return kind === 'audio' ? 'music_note' : kind === 'video' ? 'movie' : 'image';
}

async function fetchMediaData(item) {
  return engine.mediaData(Number(item?.scope) || 0, Number(item?.id), 0);
}

async function previewMedia(item) {
  const data = await fetchMediaData(item);
  const mime = mediaMime(item) || 'application/octet-stream';
  const url = URL.createObjectURL(new Blob([data], { type: mime }));
  window.open(url, '_blank', 'noopener');
  setTimeout(() => URL.revokeObjectURL(url), 60_000);
}

async function downloadMedia(item) {
  const data = await fetchMediaData(item);
  downloadBytes(data, safeResourceName(item, 'resource'), mediaMime(item) || 'application/octet-stream');
}

function buildMediaItem(item) {
  const kind = mediaKind(item);
  const element = document.createElement('button');
  element.type = 'button';
  element.className = 'media-item loading';
  element.item = item;
  element.title = item?.name || `资源 ${item?.id}`;
  element.addEventListener('click', () => {
    void previewMedia(item).catch(error => setStatus(`资源打开失败：${error.message}`));
  });
  const thumb = document.createElement('span');
  thumb.className = 'media-thumb';
  const icon = document.createElement('span');
  icon.className = 'material-symbols-outlined';
  icon.setAttribute('aria-hidden', 'true');
  icon.textContent = mediaIcon(kind);
  thumb.append(icon);
  const meta = document.createElement('span');
  meta.className = 'media-meta';
  const size = formatAttachmentBytes(Number(item?.size) || 0);
  meta.textContent = [mediaTypeLabel(item), size].filter(Boolean).join(' · ');
  element.append(thumb, meta);
  return element;
}

async function loadMediaThumb(element) {
  const item = element.item;
  if (!item || element.dataset.loaded) return;
  element.dataset.loaded = '1';
  try {
    const data = await fetchMediaData(item);
    const mime = mediaMime(item) || 'application/octet-stream';
    const url = URL.createObjectURL(new Blob([data], { type: mime }));
    mediaObjectURLs.push(url);
    const thumb = element.querySelector('.media-thumb');
    const icon = thumb?.querySelector('.material-symbols-outlined');
    const img = document.createElement('img');
    img.alt = item.name || '';
    img.src = url;
    img.addEventListener('load', () => {
      if (icon) icon.hidden = true;
      const meta = element.querySelector('.media-meta');
      if (!meta || !img.naturalWidth) return;
      const size = formatAttachmentBytes(Number(item.size) || 0);
      meta.textContent = [mediaTypeLabel(item), `${img.naturalWidth}×${img.naturalHeight}`, size].filter(Boolean).join(' · ');
    });
    img.addEventListener('error', () => img.remove());
    thumb?.append(img);
  } catch (_) {
    // 缩略图加载失败时保留类型图标。
  } finally {
    element.classList.remove('loading');
  }
}

function renderMedia() {
  if (!mediaElement) return;
  const generation = documentGeneration;
  revokeMediaURLs();
  mediaElement.replaceChildren();
  if (!pageInfos.length) {
    mediaElement.append(outlineEmptyMessage('未打开文档'));
    return;
  }
  mediaElement.append(outlineEmptyMessage('正在读取资源...'));
  engine.media().then(list => {
    if (generation !== documentGeneration) return;
    const resources = Array.isArray(list) ? list : [];
    mediaElement.replaceChildren();
    if (!resources.length) {
      mediaElement.append(outlineEmptyMessage('此文档没有多媒体资源'));
      return;
    }
    const filtered = sidebarFilterValue
      ? resources.filter(item => `${item.name || ''} ${item.format || ''} ${item.type || ''}`.toLowerCase().includes(sidebarFilterValue))
      : resources;
    if (!filtered.length) {
      mediaElement.append(outlineEmptyMessage('无匹配结果'));
      return;
    }
    const grid = document.createElement('div');
    grid.className = 'media-grid';
    filtered.forEach(item => grid.append(buildMediaItem(item)));
    mediaElement.append(grid);
    const thumbs = [...grid.querySelectorAll('.media-item')].filter(element => mediaKind(element.item) === 'image');
    grid.querySelectorAll('.media-item').forEach(element => {
      if (mediaKind(element.item) !== 'image') element.classList.remove('loading');
    });
    if (thumbs.length && typeof IntersectionObserver === 'function') {
      mediaObserver = new IntersectionObserver(entries => {
        entries.forEach(entry => {
          if (!entry.isIntersecting) return;
          mediaObserver?.unobserve(entry.target);
          void loadMediaThumb(entry.target);
        });
      }, { root: mediaElement, rootMargin: '200px' });
      thumbs.forEach(element => mediaObserver.observe(element));
    } else {
      thumbs.forEach(element => { void loadMediaThumb(element); });
    }
    restorePanelScroll('media');
  }).catch(() => {
    if (generation !== documentGeneration) return;
    mediaElement.replaceChildren(outlineEmptyMessage('获取资源失败'));
  });
}

function goToAnnotation(info) {
  const boundary = info?.boundary;
  if (boundary && Number.isFinite(boundary.x) && Number.isFinite(boundary.y)) {
    goToDestination(info.page, { type: 'XYZ', left: boundary.x, top: boundary.y, right: null, bottom: null, zoom: null });
    return;
  }
  goTo(info.page);
}

function buildAnnotationItem(info) {
  const item = document.createElement('button');
  item.type = 'button';
  item.className = 'annotation-item';
  const hasPage = Number.isInteger(info?.page) && info.page >= 0;
  if (hasPage) {
    item.addEventListener('click', () => goToAnnotation(info));
    item.title = `跳转到第 ${info.page + 1} 页`;
  } else {
    item.disabled = true;
  }
  const page = document.createElement('span');
  page.className = 'outline-page';
  page.textContent = hasPage ? String(info.page + 1) : '—';
  const type = document.createElement('span');
  type.className = 'annotation-type';
  type.textContent = [info?.type, info?.subtype].filter(Boolean).join(' / ') || '注解';
  item.append(page, type);
  const metaText = [info?.creator, info?.last_mod_date].filter(Boolean).join(' · ');
  if (metaText) {
    const meta = document.createElement('span');
    meta.className = 'annotation-meta';
    meta.textContent = metaText;
    item.append(meta);
  }
  if (info?.remark) {
    const remark = document.createElement('span');
    remark.className = 'annotation-remark';
    remark.textContent = info.remark;
    item.append(remark);
  }
  if (info?.visible === false) item.append(attachmentBadge('隐藏', 'attachment-warn'));
  return item;
}

function renderAnnotations() {
  if (!annotationsElement) return;
  const generation = documentGeneration;
  annotationsElement.replaceChildren();
  if (!pageInfos.length) {
    annotationsElement.append(outlineEmptyMessage('未打开文档'));
    return;
  }
  annotationsElement.append(outlineEmptyMessage('正在读取注解...'));
  engine.annotations().then(list => {
    if (generation !== documentGeneration) return;
    const annotations = Array.isArray(list) ? list : [];
    annotationsElement.replaceChildren();
    if (!annotations.length) {
      annotationsElement.append(outlineEmptyMessage('此文档没有注解'));
      return;
    }
    const filtered = sidebarFilterValue
      ? annotations.filter(item => `${item.type || ''} ${item.subtype || ''} ${item.creator || ''} ${item.remark || ''}`.toLowerCase().includes(sidebarFilterValue))
      : annotations;
    if (!filtered.length) {
      annotationsElement.append(outlineEmptyMessage('无匹配结果'));
      return;
    }
    filtered.forEach(item => annotationsElement.append(buildAnnotationItem(item)));
    restorePanelScroll('annotations');
  }).catch(() => {
    if (generation !== documentGeneration) return;
    annotationsElement.replaceChildren(outlineEmptyMessage('获取注解失败'));
  });
}

// signatureMethodLabel 把常见签名/摘要算法 OID 显示为易读名称。
function signatureMethodLabel(method) {
  const value = String(method || '');
  const known = {
    '1.2.156.10197.1.501': 'SM2',
    '1.2.156.10197.1.401': 'SM3',
    '1.3.14.3.2.26': 'SHA1',
    '1.2.840.113549.2.5': 'MD5',
    '2.16.840.1.101.3.4.2.1': 'SHA256',
  };
  return known[value] || value;
}

function sealMime(type) {
  const value = String(type || '').toLowerCase();
  if (value === 'png') return 'image/png';
  if (value === 'jpg' || value === 'jpeg') return 'image/jpeg';
  return '';
}

function signatureSealIcon() {
  const icon = document.createElement('span');
  icon.className = 'material-symbols-outlined signature-seal-icon';
  icon.setAttribute('aria-hidden', 'true');
  icon.textContent = 'verified_user';
  return icon;
}

function buildSignatureStamp(info, stamp, stampIndex) {
  const button = document.createElement('button');
  button.type = 'button';
  button.className = 'signature-stamp';
  const hasPage = Number.isInteger(stamp?.page) && stamp.page >= 0;
  if (hasPage) {
    button.addEventListener('click', () => goToAnnotation({ page: stamp.page, boundary: stamp.boundary }));
    button.title = `跳转到第 ${stamp.page + 1} 页`;
  } else {
    button.disabled = true;
  }
  const mime = sealMime(stamp?.seal_type);
  if (stamp?.has_seal && mime) {
    const img = document.createElement('img');
    img.className = 'signature-seal';
    img.alt = '印章';
    engine.signatureSeal(Number(info.scope) || 0, String(info.id), stampIndex).then(data => {
      const url = URL.createObjectURL(new Blob([data], { type: mime }));
      mediaObjectURLs.push(url);
      img.src = url;
    }).catch(() => img.replaceWith(signatureSealIcon()));
    button.append(img);
  } else {
    button.append(signatureSealIcon());
  }
  const label = document.createElement('span');
  label.textContent = hasPage ? `第 ${stamp.page + 1} 页` : '签章';
  button.append(label);
  return button;
}

function buildSignatureItem(info) {
  const item = document.createElement('div');
  item.className = 'signature-item';
  const head = document.createElement('div');
  head.className = 'signature-head';
  const title = document.createElement('span');
  title.className = 'signature-title';
  title.textContent = info.provider || info.company || `签名 ${info.id}`;
  head.append(title);
  const metaParts = [];
  if (info.company && info.company !== info.provider) metaParts.push(info.company);
  const method = signatureMethodLabel(info.method);
  if (method) metaParts.push(method);
  if (info.date) metaParts.push(info.date);
  if (metaParts.length) {
    const meta = document.createElement('span');
    meta.className = 'signature-meta';
    meta.textContent = metaParts.join(' · ');
    head.append(meta);
  }
  if (info.has_digest) head.append(attachmentBadge(info.digest_valid ? '摘要一致' : '摘要不一致', info.digest_valid ? 'attachment-ok' : 'attachment-warn'));
  if (info.has_verification) {
    head.append(attachmentBadge(info.verified ? '验签通过' : '验签失败', info.verified ? 'attachment-ok' : 'attachment-warn'));
    if (info.trust_checked) head.append(attachmentBadge(info.trusted ? '可信' : '未授信', info.trusted ? 'attachment-ok' : 'attachment-warn'));
    else head.append(attachmentBadge('未校验证书链', ''));
  }
  item.append(head);
  if (info.verification_error) {
    const error = document.createElement('p');
    error.className = 'outline-empty';
    error.textContent = `验签错误：${info.verification_error}`;
    item.append(error);
  }
  if (Array.isArray(info.references) && info.references.length) {
    const toggle = document.createElement('button');
    toggle.type = 'button';
    toggle.className = 'signature-detail-toggle';
    toggle.textContent = `签名范围（${info.references.length}）`;
    const box = document.createElement('div');
    box.className = 'signature-refs';
    box.hidden = true;
    if (info.has_data_hash) {
      const row = document.createElement('div');
      row.className = 'signature-ref';
      row.append(attachmentBadge(info.data_hash_match ? '数据摘要一致' : '数据摘要不一致', info.data_hash_match ? 'attachment-ok' : 'attachment-warn'));
      box.append(row);
    }
    const shown = info.references.slice(0, 50);
    shown.forEach(reference => {
      const row = document.createElement('div');
      row.className = 'signature-ref';
      const path = document.createElement('span');
      path.className = 'signature-ref-path';
      path.textContent = reference.file_ref;
      path.title = reference.file_ref;
      row.append(path);
      if (!reference.exists) row.append(attachmentBadge('文件缺失', 'attachment-warn'));
      else row.append(attachmentBadge(reference.match ? '摘要一致' : '摘要不一致', reference.match ? 'attachment-ok' : 'attachment-warn'));
      if (reference.error) {
        const error = document.createElement('span');
        error.className = 'signature-ref-error';
        error.textContent = reference.error;
        row.append(error);
      }
      box.append(row);
    });
    if (info.references.length > shown.length) {
      const more = document.createElement('div');
      more.className = 'signature-ref';
      more.textContent = `…其余 ${info.references.length - shown.length} 项`;
      box.append(more);
    }
    toggle.addEventListener('click', () => {
      box.hidden = !box.hidden;
      toggle.classList.toggle('active', !box.hidden);
    });
    item.append(toggle, box);
  }
  if (Array.isArray(info.stamps) && info.stamps.length) {
    const stamps = document.createElement('div');
    stamps.className = 'signature-stamps';
    info.stamps.forEach((stamp, stampIndex) => stamps.append(buildSignatureStamp(info, stamp, stampIndex)));
    item.append(stamps);
  }
  if (Array.isArray(info.certificates) && info.certificates.length) {
    const toggle = document.createElement('button');
    toggle.type = 'button';
    toggle.className = 'signature-detail-toggle';
    toggle.textContent = '证书详情';
    const box = document.createElement('div');
    box.className = 'signature-certs';
    box.hidden = true;
    info.certificates.forEach(cert => box.append(buildCertificateDetail(info, cert)));
    toggle.addEventListener('click', () => {
      box.hidden = !box.hidden;
      toggle.classList.toggle('active', !box.hidden);
    });
    item.append(toggle, box);
  }
  const valueButton = document.createElement('button');
  valueButton.type = 'button';
  valueButton.className = 'signature-detail-toggle';
  valueButton.textContent = '导出签名值';
  valueButton.addEventListener('click', () => runAttachmentAction(valueButton, () => downloadSignatureValue(info)));
  item.append(valueButton);
  return item;
}

function derToPEM(der) {
  const bytes = new Uint8Array(der);
  let binary = '';
  for (let index = 0; index < bytes.length; index += 1) binary += String.fromCharCode(bytes[index]);
  const lines = (btoa(binary).match(/.{1,64}/g) || []).join('\n');
  return `-----BEGIN CERTIFICATE-----\n${lines}\n-----END CERTIFICATE-----\n`;
}

function certificateFileBase(info, cert) {
  const name = `${info?.provider || info?.company || info?.id || 'certificate'}-${cert?.slot_key || 'cert'}`;
  return safeResourceName({ name }, 'certificate');
}

async function downloadCertificate(info, cert, format) {
  const der = await engine.signatureCertificate(Number(info?.scope) || 0, String(info?.id), String(cert?.slot_key));
  const base = certificateFileBase(info, cert);
  if (format === 'pem') {
    downloadBytes(new TextEncoder().encode(derToPEM(der)), `${base}.pem`, 'application/x-pem-file');
  } else {
    downloadBytes(der, `${base}.der`, 'application/pkix-cert');
  }
}

async function downloadSignatureValue(info) {
  const data = await engine.signatureValue(Number(info?.scope) || 0, String(info?.id));
  const base = safeResourceName({ name: info?.provider || info?.company || info?.id || 'signature' }, 'signature');
  downloadBytes(data, `${base}.dat`, 'application/octet-stream');
}

function buildCertificateDetail(info, cert) {
  const box = document.createElement('div');
  box.className = 'certificate';
  const title = document.createElement('div');
  title.className = 'certificate-title';
  title.textContent = `${cert?.slot || '证书'}证书`;
  box.append(title);
  const badges = document.createElement('div');
  badges.className = 'certificate-badges';
  badges.append(attachmentBadge(cert?.signature_valid ? '签名有效' : '签名无效', cert?.signature_valid ? 'attachment-ok' : 'attachment-warn'));
  badges.append(attachmentBadge(cert?.certificate_valid ? '证书在有效期内' : '证书有效期未通过', cert?.certificate_valid ? 'attachment-ok' : 'attachment-warn'));
  if (cert?.trust_checked) badges.append(attachmentBadge(cert.trusted ? '证书链可信' : '证书链不可信', cert.trusted ? 'attachment-ok' : 'attachment-warn'));
  else badges.append(attachmentBadge('未校验证书链', ''));
  if (cert?.revocation_checked) badges.append(attachmentBadge(`吊销：${cert.revocation_status || 'unknown'}`, cert.revocation_status === 'good' ? 'attachment-ok' : 'attachment-warn'));
  box.append(badges);
  const rows = [
    ['主体', cert?.subject],
    ['签发者', cert?.issuer],
    ['序列号', cert?.serial_number],
    ['有效期', cert?.not_before && cert?.not_after ? `${cert.not_before} ~ ${cert.not_after}` : ''],
    ['公钥', cert?.public_key],
    ['签名算法', signatureMethodLabel(cert?.algorithm)],
    ['签名格式', cert?.signature_format],
  ];
  rows.forEach(([label, value]) => {
    if (!value) return;
    const row = document.createElement('div');
    row.className = 'certificate-row';
    const name = document.createElement('span');
    name.className = 'certificate-label';
    name.textContent = label;
    const text = document.createElement('span');
    text.className = 'certificate-value';
    text.textContent = value;
    row.append(name, text);
    box.append(row);
  });
  const errors = [cert?.trust_error, cert?.revocation_error, cert?.error].filter(Boolean);
  if (errors.length) {
    const error = document.createElement('p');
    error.className = 'outline-empty';
    error.textContent = errors.join('；');
    box.append(error);
  }
  if (cert?.slot_key) {
    const actions = document.createElement('div');
    actions.className = 'certificate-actions';
    const derButton = document.createElement('button');
    derButton.type = 'button';
    derButton.className = 'certificate-action';
    derButton.textContent = '导出 DER';
    derButton.addEventListener('click', () => runAttachmentAction(derButton, () => downloadCertificate(info, cert, 'der')));
    const pemButton = document.createElement('button');
    pemButton.type = 'button';
    pemButton.className = 'certificate-action';
    pemButton.textContent = '导出 PEM';
    pemButton.addEventListener('click', () => runAttachmentAction(pemButton, () => downloadCertificate(info, cert, 'pem')));
    actions.append(derButton, pemButton);
    box.append(actions);
  }
  return box;
}

function renderSignatures() {
  if (!signaturesElement) return;
  const generation = documentGeneration;
  revokeMediaURLs();
  signaturesElement.replaceChildren();
  if (!pageInfos.length) {
    signaturesElement.append(outlineEmptyMessage('未打开文档'));
    return;
  }
  signaturesElement.append(outlineEmptyMessage('正在读取签名...'));
  engine.signatures().then(list => {
    if (generation !== documentGeneration) return;
    const signatures = Array.isArray(list) ? list : [];
    signaturesElement.replaceChildren();
    if (!signatures.length) {
      signaturesElement.append(outlineEmptyMessage('此文档没有签名'));
      return;
    }
    const filtered = sidebarFilterValue
      ? signatures.filter(item => `${item.provider || ''} ${item.company || ''} ${item.method || ''} ${item.date || ''}`.toLowerCase().includes(sidebarFilterValue))
      : signatures;
    if (!filtered.length) {
      signaturesElement.append(outlineEmptyMessage('无匹配结果'));
      return;
    }
    filtered.forEach(item => signaturesElement.append(buildSignatureItem(item)));
    restorePanelScroll('signatures');
  }).catch(() => {
    if (generation !== documentGeneration) return;
    signaturesElement.replaceChildren(outlineEmptyMessage('获取签名失败'));
  });
}

function buildOutlineList(nodes, depth, forceExpand = false, pathPrefix = '') {
  const list = document.createElement('ul');
  list.className = 'outline-list';
  nodes.forEach((node, index) => {
    const path = pathPrefix ? `${pathPrefix}.${index}` : String(index);
    const item = document.createElement('li');
    item.className = 'outline-item';
    const row = document.createElement('div');
    row.className = 'outline-row';
    row.style.paddingLeft = `${depth * 14}px`;
    const hasChildren = Array.isArray(node.children) && node.children.length > 0;
    if (hasChildren) {
      const toggle = document.createElement('button');
      toggle.type = 'button';
      toggle.className = 'outline-toggle';
      toggle.setAttribute('aria-label', '折叠/展开');
      const icon = document.createElement('span');
      icon.className = 'material-symbols-outlined';
      icon.setAttribute('aria-hidden', 'true');
      icon.textContent = 'expand_more';
      toggle.append(icon);
      const children = buildOutlineList(node.children, depth + 1, forceExpand, path);
      if (!forceExpand && node.expanded === false) {
        children.hidden = true;
        toggle.classList.add('collapsed');
      }
      toggle.addEventListener('click', () => {
        children.hidden = !children.hidden;
        toggle.classList.toggle('collapsed', children.hidden);
        if (!sidebarFilterValue) {
          outlineExpandState[path] = !children.hidden;
          persistOutlineExpandState();
        }
      });
      row.append(toggle);
      item.append(row, children);
    } else {
      const spacer = document.createElement('span');
      spacer.className = 'outline-toggle-placeholder';
      row.append(spacer);
      item.append(row);
    }
    const label = document.createElement('button');
    label.type = 'button';
    label.className = 'outline-label';
    label.textContent = node.title || '未命名';
    label.title = node.title || '';
    if (Number.isInteger(node.page) && node.page >= 0) {
      label.dataset.page = String(node.page);
      label.addEventListener('click', () => goToDestination(node.page, node.dest));
    } else if (typeof node.uri === 'string' && node.uri) {
      label.classList.add('outline-link');
      label.title = node.uri;
      label.addEventListener('click', () => window.open(node.uri, '_blank', 'noopener'));
    } else {
      label.disabled = true;
    }
    row.append(label);
    appendOutlinePageTag(row, node.page);
    list.append(item);
  });
  return list;
}

// normalizeDestZoom 把 OFD 目标缩放归一化为阅读器支持的倍率。
// 部分生产者按百分比（100=100%）书写，这里对明显大于 5 的值按百分比处理。
function normalizeDestZoom(value) {
  let zoomValue = Number(value);
  if (!Number.isFinite(zoomValue) || zoomValue <= 0) return 0;
  if (zoomValue > 5) zoomValue /= 100;
  return Math.max(0.5, Math.min(3, zoomValue));
}

// destPagePoint 把 Dest 的 Left/Top（毫米）换算为页面显示区域内的基准像素坐标
// （zoom=1，X 向右、Y 向下），并考虑当前页面旋转。
function destPagePoint(info, dest) {
  const rotation = ((pageRotation % 360) + 360) % 360;
  const rawLeft = Number(dest.left);
  const rawTop = Number(dest.top);
  const x = Number.isFinite(rawLeft) ? rawLeft : 0;
  const y = Number.isFinite(rawTop) ? rawTop : 0;
  const base = rotation % 180 === 0 ? 820 / info.width : 820 / info.height;
  switch (rotation) {
    case 90:
      return { x: (info.height - y) * base, y: x * base };
    case 180:
      return { x: (info.width - x) * base, y: (info.height - y) * base };
    case 270:
      return { x: y * base, y: (info.width - x) * base };
    default:
      return { x: x * base, y: y * base };
  }
}

// fitRectZoom 计算把 Dest 的 FitR 矩形适配到可用区域所需的缩放。
function fitRectZoom(info, dest) {
  const rawLeft = Number(dest.left);
  const rawTop = Number(dest.top);
  const rawRight = Number(dest.right);
  const rawBottom = Number(dest.bottom);
  const rectWidth = Math.abs((Number.isFinite(rawRight) ? rawRight : info.width) - (Number.isFinite(rawLeft) ? rawLeft : 0));
  const rectHeight = Math.abs((Number.isFinite(rawBottom) ? rawBottom : info.height) - (Number.isFinite(rawTop) ? rawTop : 0));
  if (rectWidth <= 0 || rectHeight <= 0) return 0;
  const base = 820 / info.width; // 毫米到基准像素在横纵方向等比
  const rotation = ((pageRotation % 360) + 360) % 360;
  const displayWidth = (rotation % 180 === 0 ? rectWidth : rectHeight) * base;
  const displayHeight = (rotation % 180 === 0 ? rectHeight : rectWidth) * base;
  const readerStyle = getComputedStyle(readerElement);
  const verticalPadding = parseFloat(readerStyle.paddingTop) + parseFloat(readerStyle.paddingBottom);
  const availableWidth = Math.max(1, pagesElement.clientWidth);
  const availableHeight = Math.max(1, window.innerHeight - headerHeight() - status.offsetHeight - verticalPadding - 24);
  return Math.max(0.5, Math.min(3, Math.min(availableWidth / displayWidth, availableHeight / displayHeight)));
}

// scrollToDestinationX 让目标横向位置对齐到阅读区左边缘（存在横向溢出时才生效）。
function scrollToDestinationX(index, targetX) {
  const card = pageCards[index];
  if (!card) return;
  const cardRect = card.getBoundingClientRect();
  const pagesRect = pagesElement.getBoundingClientRect();
  const desired = pagesElement.scrollLeft + (cardRect.left - pagesRect.left) + targetX - 8;
  pagesElement.scrollLeft = Math.max(0, desired);
}

// goToDestination 跳转到指定页；有目标位置时按 Dest 的 Top/Left/Zoom 精确定位，
// FitR 先按矩形适配缩放。旋转页面按显示方向换算坐标。
function goToDestination(index, dest) {
  if (!Number.isInteger(index) || index < 0 || index >= pageInfos.length) return;
  const info = pageInfos[index];
  if (!dest || !info || info.width <= 0) {
    goTo(index);
    return;
  }
  const type = String(dest.type || '').toUpperCase();
  if (type === 'FITR') {
    const fit = fitRectZoom(info, dest);
    if (fit) setZoom(fit, 'manual');
  } else {
    const destZoom = normalizeDestZoom(dest.zoom);
    if (destZoom) setZoom(destZoom, 'manual');
  }
  const position = pageSpreadPositionForPage(index);
  mountPageSpread(position);
  setCurrent(index);
  const point = destPagePoint(info, dest);
  const offset = pageSpreadOffset(position) + point.y * zoom;
  const trackTop = pageVirtualTrack.getBoundingClientRect().top + window.scrollY;
  window.scrollTo({ top: Math.max(0, trackTop + pageTrackScrollFromContent(offset)), behavior: 'smooth' });
  schedulePageVirtualTranslate();
  requestAnimationFrame(() => scrollToDestinationX(index, point.x * zoom));
}

// updateOutlineActive 高亮当前页对应的最近大纲项（页码不超过当前页的最后一项）。
function updateOutlineActive() {
  const panels = [outlineElement, bookmarksElement].filter(panel => panel && !panel.hidden);
  panels.forEach(panel => {
    const labels = panel.querySelectorAll('.outline-label[data-page]');
    let active = null;
    let activePage = -1;
    labels.forEach(label => {
      label.classList.remove('active');
      const page = Number(label.dataset.page);
      if (page <= current && page >= activePage) {
        activePage = page;
        active = label;
      }
    });
    if (!active) return;
    active.classList.add('active');
    const box = panel.getBoundingClientRect();
    const rect = active.getBoundingClientRect();
    if (rect.top < box.top) panel.scrollTop -= box.top - rect.top;
    else if (rect.bottom > box.bottom) panel.scrollTop += rect.bottom - box.bottom;
  });
}

function setRenderFormat(value) {
  if (!['png', 'jpg', 'svg'].includes(value)) value = 'png';
  const changed = renderFormat !== value;
  renderFormat = value;
  renderFormatSelect.value = value;
  if (!imageRenderFormat()) {
    clarityPriority = false;
    clarityPrioritySelect.checked = false;
    try {
      localStorage.setItem(clarityPriorityStorageKey, 'false');
    } catch (_) {}
  }
  updateNavigation();
  try {
    localStorage.setItem(renderFormatStorageKey, value);
  } catch (_) {}
  if (!changed || !pageInfos.length) return;
  cancelRequests(pageRequests);
  pageCache.clear();
  resetRenderProgress();
  pageCards.forEach(card => {
    card.classList.add('loading');
    const image = card.querySelector('.page-image');
    image.hidden = true;
    image.removeAttribute('src');
  });
  pageCards.forEach((card, index) => {
    if (!card) return;
    const bounds = card.getBoundingClientRect();
    if (bounds.top < window.innerHeight + 800 && bounds.bottom > -800) loadPage(index);
  });
  scheduleVirtualUpdate();
}

function setClarityPriority(enabled) {
  const previousDPI = pageDPI();
  clarityPriority = imageRenderFormat() && !!enabled;
  clarityPrioritySelect.checked = clarityPriority;
  try {
    localStorage.setItem(clarityPriorityStorageKey, String(clarityPriority));
  } catch (_) {}
  if (!pageInfos.length || previousDPI === pageDPI()) return;
  reloadPageImages();
  scheduleVirtualUpdate();
}

function setPagePillVisible(visible) {
  pagePillVisible = visible;
  showPagePill.checked = visible;
  try {
    localStorage.setItem(pagePillStorageKey, String(visible));
  } catch (_) {}
  updateNavigation();
}

function setTextLayerVisible(visible) {
  textLayerVisible = visible;
  document.body.classList.toggle('hide-text-layer', !visible);
  if (visible) {
    pageCards.forEach((card, index) => {
      if (card && textCache.has(index)) buildTextLayer(index);
    });
  } else {
    pageCards.forEach(card => card?.querySelector('.text-layer')?.replaceChildren());
  }
}

function setDarkReadingVisible(visible) {
  darkReadingVisible = visible;
  document.body.classList.toggle('dark-reading', visible);
  try {
    localStorage.setItem('ofd-dark-reading', String(visible));
  } catch (_) {
    // 隐私浏览环境可能无法使用存储功能。
  }
}

function validDocumentBackgroundColor(value) {
  return typeof value === 'string' && /^#[0-9a-f]{6}$/i.test(value);
}

function setDocumentBackground(mode, color = documentBackgroundCustomColor) {
  if (!['white', 'transparent', 'custom'].includes(mode) && !documentBackgroundThemes[mode]) mode = 'white';
  if (!validDocumentBackgroundColor(color)) color = '#ffffff';
  documentBackgroundMode = mode;
  documentBackgroundCustomColor = color.toLowerCase();
  const value = mode === 'transparent'
    ? 'transparent'
    : mode === 'custom'
      ? documentBackgroundCustomColor
      : documentBackgroundThemes[mode] || '#ffffff';
  document.documentElement.style.setProperty('--document-background', value);
  documentBackground.value = mode;
  documentBackgroundColorPicker.value = documentBackgroundCustomColor;
  documentBackgroundColorPicker.hidden = mode !== 'custom';
  try {
    localStorage.setItem(documentBackgroundModeStorageKey, mode);
    localStorage.setItem(documentBackgroundColorStorageKey, documentBackgroundCustomColor);
  } catch (_) {
    // 隐私浏览环境可能无法使用存储功能。
  }
}

try {
  showThumbnails.checked = thumbnailsVisible;
  setThumbnailsVisible(thumbnailsVisible);
} catch (_) {
  document.body.classList.toggle('hide-thumbnails', !thumbnailsVisible);
}
try {
  setRenderFormat(renderFormat);
} catch (_) {}
try {
  setClarityPriority(clarityPriority);
} catch (_) {}
try {
  darkReading.checked = localStorage.getItem('ofd-dark-reading') === 'true';
  setDarkReadingVisible(darkReading.checked);
} catch (_) {}
try {
  const mode = localStorage.getItem(documentBackgroundModeStorageKey) || 'white';
  const color = localStorage.getItem(documentBackgroundColorStorageKey) || '#ffffff';
  setDocumentBackground(mode, color);
} catch (_) {
  setDocumentBackground('white');
}

file.addEventListener('change', loadFile);
cancelOpen.addEventListener('click', cancelOpening);
recentToggle.addEventListener('click', () => setRecentPanelOpen(recentPanel.hidden));
recentClear.addEventListener('click', clearRecentFiles);
for (const eventName of ['dragenter', 'dragover']) {
  pagesElement.addEventListener(eventName, event => {
    event.preventDefault();
    if (!pageInfos.length) {
      empty.classList.add('drag-over');
      dropHint.style.display = 'block';
    }
  });
}
for (const eventName of ['dragleave', 'drop']) {
  pagesElement.addEventListener(eventName, event => {
    event.preventDefault();
    empty.classList.remove('drag-over');
    dropHint.style.display = 'none';
  });
}
pagesElement.addEventListener('drop', event => {
  if (pageInfos.length) return;
  const dropped = Array.from(event.dataTransfer?.files || [])
    .find(candidate => /\.ofd$/i.test(candidate.name) || candidate.type === 'application/ofd');
  if (!dropped) {
    setStatus('请拖入 OFD 文件。');
    return;
  }
  openSelectedFile(dropped);
});
previous.addEventListener('click', () => navigatePage(-1));
next.addEventListener('click', () => navigatePage(1));
cancelAction.addEventListener('click', cancelDocumentAction);
documentMenuToggle.addEventListener('click', () => setDocumentMenuOpen(documentMenu.hidden));
documentMenu.addEventListener('click', event => { if (event.target.closest('button')) setDocumentMenuOpen(false); });
printPage.addEventListener('click', openPrintDialog);
exportDocument.addEventListener('click', openExportDialog);
printForm.addEventListener('change', updatePrintRangeControl);
printForm.addEventListener('submit', event => {
  event.preventDefault();
  try {
    const indexes = selectedPrintIndexes();
    if (!indexes.length) {
      printError.textContent = '请选择至少一页';
      printError.hidden = false;
      return;
    }
    closePrintDialog();
    void printSelectedPages(indexes);
  } catch (error) {
    printError.textContent = error.message;
    printError.hidden = false;
    if (printForm.elements.namedItem('print-range').value === 'custom') printCustomRange.focus();
  }
});
printCancel.addEventListener('click', closePrintDialog);
exportRange.addEventListener('change', updateExportRangeControl);
exportFormat.addEventListener('change', updateExportFormatControl);
exportForm.addEventListener('submit', event => {
  event.preventDefault();
  void startExport();
});
exportCancel.addEventListener('click', closeExportDialog);
aboutLink.addEventListener('click', openAboutDialog);
aboutClose.addEventListener('click', closeAboutDialog);
infoToggle.addEventListener('click', () => setInfoPanelOpen(infoPanel.hidden));
infoClose.addEventListener('click', () => setInfoPanelOpen(false));
 copyPageText.addEventListener('click', copyCurrentPageText);
 copyAllTextButton.addEventListener('click', copyDocumentText);
pageNumber.addEventListener('change', () => {
  if (!pageInfos.length) return;
  const value = Number(pageNumber.value);
  const page = Number.isFinite(value) ? Math.round(value) : current + 1;
  const target = Math.max(1, Math.min(pageInfos.length, page));
  pageNumber.value = target;
  goTo(target - 1);
});
searchButton.addEventListener('click', searchDocument);
searchInput.addEventListener('keydown', event => { if (event.key === 'Enter') searchDocument(); });
searchToggle.addEventListener('click', () => setSearchPanelOpen(searchPanel.hidden));
searchPrevious.addEventListener('click', () => moveSearchResult(-1));
searchNext.addEventListener('click', () => moveSearchResult(1));
zoomOut.addEventListener('click', () => setZoom(zoom - 0.25));
zoomIn.addEventListener('click', () => setZoom(zoom + 0.25));
zoomMenuToggle.addEventListener('click', () => setZoomMenuOpen(zoomMenu.hidden));
zoomMenu.addEventListener('click', event => {
  const button = event.target.closest('button');
  if (!button) return;
  if (button.dataset.zoom) setZoom(Number(button.dataset.zoom), 'manual');
  else if (button.dataset.zoomFit === 'width') fitWidthZoom();
  else if (button.dataset.zoomFit === 'page') fitPageZoom();
  setZoomMenuOpen(false);
});
rotatePageButton.addEventListener('click', rotatePage);
readingMode.addEventListener('click', () => setReadingMode(!document.body.classList.contains('reading-mode')));
viewToggle.addEventListener('click', () => setViewPanelOpen(viewPanel.hidden));
mobileToolbarToggle.addEventListener('click', () => {
  setMobileToolbarExpanded(!mobileToolbarToggle.matches('[aria-expanded="true"]'));
});
showThumbnails.addEventListener('change', () => setThumbnailsVisible(showThumbnails.checked));
sidebarTabThumbnails?.addEventListener('click', () => setSidebarTab('thumbnails'));
sidebarTabOutline?.addEventListener('click', () => setSidebarTab('outline'));
sidebarTabBookmarks?.addEventListener('click', () => setSidebarTab('bookmarks'));
sidebarTabMore?.addEventListener('click', () => setSidebarMoreOpen(sidebarMoreMenu?.hidden));
sidebarMoreFonts?.addEventListener('click', () => setSidebarTab('fonts'));
sidebarMoreAttachments?.addEventListener('click', () => setSidebarTab('attachments'));
sidebarMoreMedia?.addEventListener('click', () => setSidebarTab('media'));
sidebarMoreAnnotations?.addEventListener('click', () => setSidebarTab('annotations'));
sidebarMoreSignatures?.addEventListener('click', () => setSidebarTab('signatures'));
if (thumbnailSizeSlider) {
  thumbnailSizeSlider.addEventListener('input', () => setThumbnailSize(Number(thumbnailSizeSlider.value)));
  thumbnailSizeSlider.addEventListener('change', persistThumbnailSize);
}
outlineExpandAll?.addEventListener('click', () => setAllOutlineExpanded(true));
outlineCollapseAll?.addEventListener('click', () => setAllOutlineExpanded(false));
sidebarTabsElement?.addEventListener('keydown', event => {
  if (!['ArrowLeft', 'ArrowRight'].includes(event.key)) return;
  const tabs = [
    [sidebarTabThumbnails, 'thumbnails'],
    [sidebarTabOutline, 'outline'],
    [sidebarTabBookmarks, 'bookmarks'],
    [sidebarTabMore, ''],
  ].filter(([button]) => button && !button.hidden);
  const index = tabs.findIndex(([button]) => button === document.activeElement);
  if (index < 0) return;
  event.preventDefault();
  const step = event.key === 'ArrowRight' ? 1 : -1;
  const next = tabs[(index + step + tabs.length) % tabs.length];
  next[0].focus();
  if (next[1]) setSidebarTab(next[1]);
});
if (sidebarFilter) {
  sidebarFilter.addEventListener('input', () => {
    sidebarFilterValue = sidebarFilter.value.trim().toLowerCase();
    if (activeSidebarTab === 'outline') renderOutline();
    else if (activeSidebarTab === 'bookmarks') renderBookmarks();
    else if (activeSidebarTab === 'fonts') renderFonts();
    else if (activeSidebarTab === 'attachments') renderAttachments();
    else if (activeSidebarTab === 'media') renderMedia();
    else if (activeSidebarTab === 'annotations') renderAnnotations();
    else if (activeSidebarTab === 'signatures') renderSignatures();
  });
}
if (sidebarResizer) {
  let sidebarDrag = null;
  sidebarResizer.addEventListener('pointerdown', event => {
    if (window.matchMedia('(max-width: 620px)').matches) return;
    sidebarDrag = { startX: event.clientX, startWidth: sidebarWidth };
    sidebarResizer.classList.add('dragging');
    try { sidebarResizer.setPointerCapture(event.pointerId); } catch (_) {}
    event.preventDefault();
  });
  sidebarResizer.addEventListener('pointermove', event => {
    if (!sidebarDrag) return;
    setSidebarWidth(sidebarDrag.startWidth + (event.clientX - sidebarDrag.startX));
  });
  const stopSidebarDrag = event => {
    if (!sidebarDrag) return;
    sidebarDrag = null;
    sidebarResizer.classList.remove('dragging');
    try { sidebarResizer.releasePointerCapture(event.pointerId); } catch (_) {}
    finishSidebarResize();
  };
  sidebarResizer.addEventListener('pointerup', stopSidebarDrag);
  sidebarResizer.addEventListener('pointercancel', stopSidebarDrag);
  sidebarResizer.addEventListener('keydown', event => {
    if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
    event.preventDefault();
    if (event.key === 'ArrowLeft') setSidebarWidth(sidebarWidth - 16);
    else if (event.key === 'ArrowRight') setSidebarWidth(sidebarWidth + 16);
    else if (event.key === 'Home') setSidebarWidth(140);
    else setSidebarWidth(window.innerWidth);
    finishSidebarResize();
  });
  applySidebarWidth();
}
sidebarScrollPanels.outline = outlineElement;
sidebarScrollPanels.bookmarks = bookmarksElement;
sidebarScrollPanels.fonts = fontsElement;
sidebarScrollPanels.attachments = attachmentsElement;
sidebarScrollPanels.media = mediaElement;
sidebarScrollPanels.annotations = annotationsElement;
sidebarScrollPanels.signatures = signaturesElement;
Object.entries(sidebarScrollPanels).forEach(([panel, element]) => {
  element?.addEventListener('scroll', () => {
    sidebarScroll[panel] = element.scrollTop;
    persistSidebarScroll();
  }, { passive: true });
});
applySidebarPanels();
showTextLayer.addEventListener('change', () => setTextLayerVisible(showTextLayer.checked));
showPagePill.addEventListener('change', () => setPagePillVisible(showPagePill.checked));
pillHide.addEventListener('click', () => setPagePillVisible(false));
showPagePill.checked = pagePillVisible;
darkReading.addEventListener('change', () => setDarkReadingVisible(darkReading.checked));
documentBackground.addEventListener('change', () => setDocumentBackground(documentBackground.value));
documentBackgroundColorPicker.addEventListener('input', () => {
  if (documentBackgroundMode === 'custom') setDocumentBackground('custom', documentBackgroundColorPicker.value);
});
pageLayoutSelect.addEventListener('change', () => setPageLayout(pageLayoutSelect.value));
clarityPrioritySelect.addEventListener('change', () => setClarityPriority(clarityPrioritySelect.checked));
renderFormatSelect.addEventListener('change', () => setRenderFormat(renderFormatSelect.value));
backToTop.addEventListener('click', scrollToTop);
window.addEventListener('scroll', updateBackToTop, { passive: true });
window.addEventListener('scroll', schedulePageVirtualUpdate, { passive: true });
window.addEventListener('scroll', schedulePageVirtualTranslate, { passive: true });
thumbnailsElement.addEventListener('scroll', scheduleThumbnailVirtualUpdate, { passive: true });
window.addEventListener('resize', () => {
  updateThumbnailMetrics();
  updatePageVirtualMetrics();
  if (zoomMode === 'fit') fitWidthZoom();
  else if (zoomMode === 'page') fitPageZoom();
  scheduleVirtualUpdate();
});
updateBackToTop();
if ('serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('service-worker.js', { updateViaCache: 'none' }).catch(error => {
      console.warn('[OFD] Service Worker 注册失败', error);
    });
  });
  window.__readMemory = trigger => reportMemory(trigger || '手动');
  void reportMemory('启动');
}
if ('launchQueue' in window && typeof window.launchQueue.setConsumer === 'function') {
  window.launchQueue.setConsumer(async launchParams => {
    const [fileHandle] = launchParams.files || [];
    if (!fileHandle || typeof fileHandle.getFile !== 'function') return;
    try {
      await openSelectedFile(await fileHandle.getFile());
    } catch (error) {
      setStatus(`打开系统文件失败：${error.message}`);
    }
  });
}
pagesElement.addEventListener('touchstart', handleTouchStart, { passive: true });
pagesElement.addEventListener('touchmove', handleTouchMove, { passive: false });
pagesElement.addEventListener('touchend', handleTouchEnd, { passive: true });
document.addEventListener('fullscreenchange', () => {
  if (!document.fullscreenElement && document.body.classList.contains('reading-mode')) {
    document.body.classList.remove('reading-mode');
    readingMode.setAttribute('aria-pressed', 'false');
    readingMode.title = '阅读模式';
    readingMode.setAttribute('aria-label', '阅读模式');
  }
});
window.addEventListener('keydown', event => {
  const target = event.target;
  const editing = target instanceof HTMLElement &&
    (target.matches('input, textarea, select') || target.isContentEditable);
  const modifier = event.ctrlKey || event.metaKey;
  const key = event.key.toLowerCase();
  if (modifier && event.shiftKey && key === 'c') {
    if (!editing && pageInfos.length) {
      event.preventDefault();
      copyCurrentPageText();
    }
    return;
  }
  if (modifier && key === 'f') {
    if (!pageInfos.length) return;
    event.preventDefault();
    setSearchPanelOpen(true);
    return;
  }
  if (modifier && key === 'b') {
    if (!editing) {
      event.preventDefault();
      toggleSidebar();
    }
    return;
  }
  if (editing) {
    if (event.key === 'Escape' && target === searchInput) setSearchPanelOpen(false);
    return;
  }
  if (event.key === 'Escape') {
    if (!searchPanel.hidden) setSearchPanelOpen(false);
    else if (!viewPanel.hidden) setViewPanelOpen(false);
    else if (zoomMenu && !zoomMenu.hidden) setZoomMenuOpen(false);
    else if (documentMenu && !documentMenu.hidden) setDocumentMenuOpen(false);
    else if (sidebarMoreMenu && !sidebarMoreMenu.hidden) setSidebarMoreOpen(false);
    else if (document.body.classList.contains('reading-mode')) setReadingMode(false);
    return;
  }
  if (event.key === '[') {
    event.preventDefault();
    cycleSidebarPanel(-1);
    return;
  }
  if (event.key === ']') {
    event.preventDefault();
    cycleSidebarPanel(1);
    return;
  }
  if (!pageInfos.length) return;
  switch (event.key) {
    case 'ArrowLeft':
    case 'PageUp':
      event.preventDefault();
      navigatePage(-1);
      break;
    case 'ArrowRight':
    case 'PageDown':
      event.preventDefault();
      navigatePage(1);
      break;
    case 'Home':
      event.preventDefault();
      goTo(0);
      break;
    case 'End':
      event.preventDefault();
      goTo(pageInfos.length - 1);
      break;
    case '+':
    case '=':
      event.preventDefault();
      setZoom(zoom + 0.25);
      break;
    case '-':
      event.preventDefault();
      setZoom(zoom - 0.25);
      break;
    case '0':
      event.preventDefault();
      setZoom(1);
      break;
    case 'f':
      event.preventDefault();
      fitWidthZoom();
      break;
    case 'F':
      event.preventDefault();
      fitPageZoom();
      break;
  }
});
document.addEventListener('click', event => {
  if (!recentPanel.hidden && !event.target.closest('.recent-group')) setRecentPanelOpen(false);
  if (!viewPanel.hidden && !event.target.closest('.view-group')) setViewPanelOpen(false);
  if (!searchPanel.hidden && !event.target.closest('.search-group')) setSearchPanelOpen(false);
  if (zoomMenu && !zoomMenu.hidden && !event.target.closest('.zoom-group')) setZoomMenuOpen(false);
  if (documentMenu && !documentMenu.hidden && !event.target.closest('.page-group')) setDocumentMenuOpen(false);
  if (!infoPanel.hidden && !event.target.closest('.info-group')) setInfoPanelOpen(false);
  if (sidebarMoreMenu && !sidebarMoreMenu.hidden && !event.target.closest('#sidebar-tabs') && !event.target.closest('#sidebar-more-menu')) setSidebarMoreOpen(false);
});
document.addEventListener('copy', () => {
  const selection = window.getSelection();
  if (!selection?.toString().trim() || !selection.anchorNode?.parentElement?.closest('.text-layer')) return;
  showCopyFeedback(`已复制 ${selection.toString().length} 个字符`);
});
window.addEventListener('beforeunload', () => {
  pageCache.clear();
  thumbnailCache.clear();
  engine.worker.terminate();
});

engine.ready.then(() => {
  startupWasmReady = true;
  updateStartupProgress();
}).catch(error => {
  if (startupProgressActive) {
    startupProgressLabel.textContent = '[1/2] WASM 模块加载失败';
    startupMessage.textContent = `WASM Worker 加载失败：${error.message}`;
    setStatus(`WASM Worker 加载失败：${error.message}`);
  }
});

function setRecentPanelOpen(open) {
  recentPanel.hidden = !open;
  recentToggle.setAttribute('aria-expanded', String(open));
  if (open) {
    setZoomMenuOpen(false);
    setDocumentMenuOpen(false);
    void refreshRecentFiles();
  }
}

void refreshRecentFiles();
