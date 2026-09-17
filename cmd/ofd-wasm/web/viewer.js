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
}

const status = document.querySelector('#status');
const statusMessage = document.querySelector('#status-message');
const cancelAction = document.querySelector('#cancel-action');
const renderProgress = document.querySelector('#render-progress');
const renderProgressLabel = document.querySelector('#render-progress-label');
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
const previous = document.querySelector('#previous');
const next = document.querySelector('#next');
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
const zoomFit = document.querySelector('#zoom-fit');
const zoomFitPage = document.querySelector('#zoom-fit-page');
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
const engine = new OFDWorkerClient();
const pageCache = new BlobURLCache(256 << 20, url =>
  Array.from(document.querySelectorAll('.page-image')).some(image => !image.hidden && image.src === url));
const thumbnailCache = new BlobURLCache(64 << 20, url =>
  Array.from(document.querySelectorAll('.thumbnail img')).some(image => !image.hidden && image.src === url));
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
let pageInfos = [];
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
let renderedPages = new Set();
let failedPages = new Set();
let copyFeedbackTimer;
let statusBeforeCopy;
let recentFiles = [];
let currentDocumentKey = '';
let pageSpreads = [];
let pageVirtualTrack;
let thumbnailVirtualTrack;
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

function updatePageVirtualMetrics() {
  if (!pageVirtualTrack) return;
  let offset = 0;
  const gap = 18 * zoom;
  pageSpreads.forEach((spread, position) => {
    const dimensions = spreadDimensions(position);
    spread.offset = offset;
    spread.height = dimensions.height * zoom;
    spread.width = dimensions.width * zoom;
    if (spread.element) {
      spread.element.style.top = `${offset}px`;
      spread.element.style.width = `${spread.width}px`;
      spread.element.style.minHeight = `${spread.height}px`;
      spread.element.style.gap = `${gap}px`;
    }
    offset += spread.height + gap;
  });
  pageVirtualTrack.style.height = `${Math.max(0, offset - gap)}px`;
}

function pageSpreadPositionForPage(index) {
  return spreadPositionForPage(index);
}

function thumbnailSlotsForLayout() {
  if (!pageLayoutIsDouble()) return pageInfos.map((_, index) => index);
  return pageSpreads.flatMap(spread => spread.pages);
}

function updateThumbnailMetrics() {
  if (!thumbnailVirtualTrack) return;
  const mobile = window.matchMedia('(max-width: 620px)').matches;
  const double = pageLayoutIsDouble();
  const columns = mobile ? 1 : double ? 2 : 1;
  const gap = mobile ? 8 : double ? 8 : 10;
  const itemWidth = mobile
    ? 72
    : Math.max(1, (thumbnailVirtualTrack.clientWidth - gap * (columns - 1)) / columns);
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
  thumbnailVirtualTrack.style.width = mobile ? `${thumbnailSlots.length * itemWidth + Math.max(0, thumbnailSlots.length - 1) * gap}px` : '100%';
  thumbnailVirtualTrack.style.height = mobile
    ? `${maxHeight}px`
    : `${Math.max(0, offset - gap)}px`;
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
  if (!spread || spread.element || !pageVirtualTrack) return;
  // 打开新文档或打开失败时，延迟的虚拟列表回调可能暂时看到旧 spread。
  // 不要用新文档的 pageInfos 去挂载旧页面索引。
  if (spread.pages.some(index => index >= 0 && !pageInfos[index])) return;
  const element = document.createElement('div');
  element.className = 'page-spread';
  element.dataset.position = position;
  spread.element = element;
  pageVirtualTrack.append(element);
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
      if (card) resizeObserver?.unobserve(card);
      delete pageCards[index];
    }
  });
  spread.element.remove();
  spread.element = undefined;
}

function applyPageWidthToSpread(spread) {
  if (!spread?.element) return;
  spread.element.style.top = `${spread.offset}px`;
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
  const trackTop = pageVirtualTrack.getBoundingClientRect().top + window.scrollY;
  const buffer = Math.max(window.innerHeight, 800);
  const viewTop = window.scrollY - trackTop - buffer;
  const viewBottom = window.scrollY - trackTop + window.innerHeight + buffer;
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
  const visible = pageSpreads
    .map((spread, position) => ({ spread, position }))
    .filter(({ spread }) => spread.element && spread.offset + spread.height >= window.scrollY - trackTop && spread.offset <= window.scrollY - trackTop + window.innerHeight)
    .sort((left, right) => Math.abs(left.spread.offset - (window.scrollY - trackTop)) - Math.abs(right.spread.offset - (window.scrollY - trackTop)));
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
  thumbnailVirtualTrack.append(thumbnail);
  resizeThumbnail(index, thumbnail);
  thumbnail.classList.toggle('active', index === current);
  if (index === current) thumbnail.setAttribute('aria-current', 'page');
  loadThumbnail(index);
}

function resizeThumbnail(index, thumbnail) {
  const slot = thumbnailSlotForPage(index);
  if (slot < 0) return;
  const { mobile, columns, gap, itemWidth, rowOffsets, maxHeight } = thumbnailMetrics;
  const width = mobile
    ? itemWidth
    : (thumbnailVirtualTrack.clientWidth - gap * (columns - 1)) / columns;
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
    thumbnail.style.top = `${rowOffsets[row] || 0}px`;
    thumbnail.style.width = `${width}px`;
    thumbnail.style.height = `${height}px`;
    thumbnail.style.minHeight = '0';
  }
}

function updateThumbnailVirtualWindow(targetSlot = -1) {
  if (!thumbnailVirtualTrack || !thumbnailSlots.length || thumbnailsElement.hidden) return;
  const { mobile, columns, itemWidth, gap, rowHeights, rowOffsets } = thumbnailMetrics;
  const buffer = mobile ? thumbnailsElement.clientWidth * 2 : thumbnailsElement.clientHeight * 2;
  const cell = itemWidth + (mobile ? gap : 0);
  let start;
  let end;
  if (mobile) {
    start = Math.max(0, Math.floor((thumbnailsElement.scrollLeft - buffer) / cell));
    end = Math.min(thumbnailSlots.length, Math.ceil((thumbnailsElement.scrollLeft + thumbnailsElement.clientWidth + buffer) / cell));
  } else {
    const top = thumbnailsElement.scrollTop - buffer;
    const bottom = thumbnailsElement.scrollTop + thumbnailsElement.clientHeight + buffer;
    let startRow = 0;
    while (startRow < rowHeights.length && rowOffsets[startRow] + rowHeights[startRow] < top) startRow += 1;
    let endRow = startRow;
    while (endRow < rowHeights.length && rowOffsets[endRow] < bottom) endRow += 1;
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
  load.catch(() => fallbackFontLoads.delete(key));
  return load;
}

// 阅读器空闲时开始下载字体。之后打开文档时，可以在首个页面渲染前
// 将已加载的字体数据传给 WASM。
void preloadFallbackFonts().catch(error => {
  setStatus(`默认中文字体加载失败：${error.message}`);
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
  zoomFit.disabled = pageInfos.length === 0;
  zoomFitPage.disabled = pageInfos.length === 0;
  rotatePageButton.disabled = pageInfos.length === 0;
  readingMode.disabled = pageInfos.length === 0;
  viewToggle.disabled = pageInfos.length === 0;
  infoToggle.disabled = pageInfos.length === 0;
  pageLayoutSelect.disabled = documentActionBusy || pageInfos.length === 0;
  renderFormatSelect.disabled = documentActionBusy || pageInfos.length === 0;
  clarityPrioritySelect.disabled = documentActionBusy || pageInfos.length === 0 || !imageRenderFormat();
  zoomLabel.textContent = `${Math.round(zoom * 100)}%`;
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
  const start = thumbnailMetrics.mobile ? button.offsetLeft : button.offsetTop;
  const size = thumbnailMetrics.mobile ? button.offsetWidth : button.offsetHeight;
  const visibleStart = thumbnailMetrics.mobile ? thumbnailsElement.scrollLeft : thumbnailsElement.scrollTop;
  const visibleSize = thumbnailMetrics.mobile ? thumbnailsElement.clientWidth : thumbnailsElement.clientHeight;
  const offset = start < visibleStart ? start : start + size > visibleStart + visibleSize
    ? start + size - visibleSize
    : -1;
  if (offset < 0) return;
  if (thumbnailMetrics.mobile) thumbnailsElement.scrollLeft = offset;
  else thumbnailsElement.scrollTop = offset;
}

function setCurrent(index, syncThumbnail = true) {
  if (index < 0 || index >= pageInfos.length) return;
  const changed = current !== index;
  current = index;
  saveReadingPosition();
  // 图片可能已经从缓存显示，但此前的文字请求可能在虚拟页面卸载时被取消。
  // 当前页切换时主动补发一次，避免正文层因请求竞态缺失。
  if (changed && pageCards[index]) void loadText(index);
  if (changed && syncThumbnail) updateThumbnailVirtualWindow(thumbnailSlotForPage(index));
  thumbnailButtons.forEach((button, buttonIndex) => {
    if (!button) return;
    const active = buttonIndex === current;
    button.classList.toggle('active', active);
    if (active) {
      button.setAttribute('aria-current', 'page');
      if (changed && syncThumbnail) keepThumbnailVisible(button);
    } else {
      button.removeAttribute('aria-current');
    }
  });
  if (zoomMode === 'page') fitPageZoom();
  updateNavigation();
}

function goTo(index) {
  if (index < 0 || index >= pageInfos.length) return;
  const position = pageSpreadPositionForPage(index);
  mountPageSpread(position);
  setCurrent(index);
  const trackTop = pageVirtualTrack.getBoundingClientRect().top + window.scrollY;
  window.scrollTo({ top: trackTop + pageSpreadOffset(position), behavior: 'smooth' });
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

function buildTextLayer(index) {
  const card = pageCards[index];
  const runs = textCache.get(index);
  const info = pageInfos[index];
  if (!card || !runs || !info) return;

  const layer = card.querySelector('.text-layer') || document.createElement('div');
  layer.className = 'text-layer';
  layer.replaceChildren();
  runs.forEach((run, runIndex) => {
    const element = document.createElement('span');
    element.className = 'text-run';
    element.style.left = `${run.x / info.width * 100}%`;
    // TextRun 坐标已经是以页面左上角为原点的覆盖层坐标。
    // 渲染器只在画布内部翻转 Y 轴，因此这里不能再次翻转文字层。
    element.style.top = `${run.y / info.height * 100}%`;
    element.style.width = `${Math.max(run.width / info.width * 100, 0.1)}%`;
    element.style.height = `${Math.max(run.height / info.height * 100, 0.1)}%`;
    element.style.fontSize = `${Math.max(run.size * card.clientWidth / info.width, 1)}px`;
    if (run.fontFamily) element.style.fontFamily = `'${run.fontFamily}', sans-serif`;
    element.style.fontWeight = run.weight > 0 ? String(run.weight) : (run.bold ? '700' : '400');
    element.style.fontStyle = run.italic ? 'italic' : 'normal';
    const runAngle = run.glyphs?.[0]?.angle ?? run.charDirection ?? 0;
    element.style.transformOrigin = 'top left';
    element.style.transform = `rotate(${runAngle}deg)`;
    element.textContent = run.text;
    layer.append(element);
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
  if (!layer.parentElement) card.append(layer);
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
  thumbnailsElement.hidden = !thumbnailsVisible;
  readerElement.classList.toggle('hide-thumbnails', !thumbnailsVisible);
  resizeObserver?.disconnect();
  pagesElement.replaceChildren();
  thumbnailsElement.replaceChildren();
  pageCards = [];
  thumbnailButtons = [];
  pageSpreads = pageSpreadGroups().map(pages => ({ pages, offset: 0, height: 0, width: 0 }));
  pageVirtualTrack = document.createElement('div');
  pageVirtualTrack.className = 'page-virtual-track';
  pagesElement.append(pageVirtualTrack);
  thumbnailVirtualTrack = document.createElement('div');
  thumbnailVirtualTrack.className = 'thumbnail-virtual-track';
  thumbnailsElement.append(thumbnailVirtualTrack);
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
      window.scrollTo({ top: Math.max(0, trackTop + pageSpreadOffset(pageSpreadPositionForPage(current)) - headerHeight()), behavior: 'auto' });
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
  thumbnailVirtualTrack = undefined;
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
    openRequest = engine.open(data);
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
    current = restoreReadingPosition(selected, pageInfos.length);
    restorePageRotation();
    buildPages();
    setStatus(`${selected.name}，共 ${pageInfos.length} 页。页面进入附近区域时才会渲染。`);
    updateRenderProgress();
    void saveRecentFile(selected);
    void reportMemory('打开文档');
  } catch (error) {
    if (generation !== documentGeneration || isCancelledError(error)) return;
    pageInfos = [];
    pageSpreads = [];
    pageVirtualTrack = undefined;
    thumbnailVirtualTrack = undefined;
    pagesElement.replaceChildren();
    thumbnailsElement.replaceChildren();
    pagesElement.append(empty);
    empty.hidden = false;
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
  thumbnailVirtualTrack = undefined;
  thumbnailSlots = [];
  thumbnailSlotByPage = [];
  currentDocumentKey = '';
  current = 0;
  searchGeneration++;
  pagesElement.replaceChildren(empty);
  empty.hidden = false;
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
  engine.info().then(info => {
    if (generation !== documentGeneration) return;
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
    if (rows.length === 0) {
      infoBody.innerHTML = '<p class="info-empty">无文档信息</p>';
      return;
    }
    infoBody.innerHTML = rows.map(([label, value]) =>
      `<div class="info-row"><span class="info-label">${label}</span><span class="info-value">${escapeHTML(value)}</span></div>`
    ).join('');
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
    await loadText(index, true);
    throwIfDocumentActionCancelled(generation);
    if (!textCache.has(index)) {
      failed += 1;
      continue;
    }
    const text = pageText(index);
    if (text) parts.push(text);
  }
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

function setThumbnailsVisible(visible) {
  thumbnailsVisible = visible;
  showThumbnails.checked = visible;
  thumbnailsElement.hidden = !visible;
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

function setTextLayerVisible(visible) {
  textLayerVisible = visible;
  document.body.classList.toggle('hide-text-layer', !visible);
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
zoomFit.addEventListener('click', fitWidthZoom);
zoomFitPage.addEventListener('click', fitPageZoom);
rotatePageButton.addEventListener('click', rotatePage);
readingMode.addEventListener('click', () => setReadingMode(!document.body.classList.contains('reading-mode')));
viewToggle.addEventListener('click', () => setViewPanelOpen(viewPanel.hidden));
mobileToolbarToggle.addEventListener('click', () => {
  setMobileToolbarExpanded(!mobileToolbarToggle.matches('[aria-expanded="true"]'));
});
showThumbnails.addEventListener('change', () => setThumbnailsVisible(showThumbnails.checked));
showTextLayer.addEventListener('change', () => setTextLayerVisible(showTextLayer.checked));
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
  if (editing) {
    if (event.key === 'Escape' && target === searchInput) setSearchPanelOpen(false);
    return;
  }
  if (event.key === 'Escape') {
    if (!searchPanel.hidden) setSearchPanelOpen(false);
    else if (!viewPanel.hidden) setViewPanelOpen(false);
    else if (document.body.classList.contains('reading-mode')) setReadingMode(false);
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
  }
});
document.addEventListener('click', event => {
  if (!recentPanel.hidden && !event.target.closest('.recent-group')) setRecentPanelOpen(false);
  if (!viewPanel.hidden && !event.target.closest('.view-group')) setViewPanelOpen(false);
  if (!searchPanel.hidden && !event.target.closest('.search-group')) setSearchPanelOpen(false);
  if (!infoPanel.hidden && !event.target.closest('.info-group')) setInfoPanelOpen(false);
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

engine.ready.then(() => setStatus('WASM Worker 已就绪，选择一个 OFD 文件开始阅读。'))
  .catch(error => setStatus(`WASM Worker 加载失败：${error.message}`));

function setRecentPanelOpen(open) {
  recentPanel.hidden = !open;
  recentToggle.setAttribute('aria-expanded', String(open));
  if (open) void refreshRecentFiles();
}

void refreshRecentFiles();
