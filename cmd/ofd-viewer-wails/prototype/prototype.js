import { NativePageClient } from './native-page.js';

await import('/wails/runtime.js');

const open = document.getElementById('open');
const status = document.getElementById('status');
const pages = document.getElementById('pages');
const searchForm = document.getElementById('search-form');
const searchQuery = document.getElementById('search-query');
const searchResults = document.getElementById('search-results');
let currentClient;
let generation = 0;
let searchGeneration = 0;
let searchRequest;
const pendingText = new Set();
let pageCards = [];

function clearSearch() {
  searchGeneration++;
  searchRequest?.cancel();
  searchRequest = undefined;
  searchResults.replaceChildren();
  for (const card of pageCards) card.querySelector('.highlight-layer')?.replaceChildren();
}

searchQuery.addEventListener('input', clearSearch);
searchForm.addEventListener('submit', event => {
  event.preventDefault();
  clearSearch();
  const query = searchQuery.value.trim();
  if (!currentClient || !query) return;
  const client = currentClient;
  const documentGeneration = generation;
  const requestGeneration = searchGeneration;
  const request = client.search(query);
  searchRequest = request;
  status.textContent = '正在搜索…';
  request.then(report => {
    if (currentClient !== client || generation !== documentGeneration || searchGeneration !== requestGeneration || open.disabled) return;
    status.textContent = `找到 ${report.results.length} 条${report.truncated ? '（结果已截断）' : ''}`;
    for (const match of report.results.slice(0, 100)) {
      const button = document.createElement('button');
      button.type = 'button';
      // 不在结果按钮缓存整段文档文字，避免权限过期后仍可复制旧摘要。
      button.textContent = `第 ${match.page + 1} 页 · 匹配`;
      button.addEventListener('click', () => {
        const card = pageCards[match.page];
        if (!card) return;
        for (const item of pageCards) item.querySelector('.highlight-layer')?.replaceChildren();
        const layer = card.querySelector('.highlight-layer');
        for (const rect of match.rects) {
          const mark = document.createElement('span');
          mark.className = 'search-highlight';
          mark.style.left = `${rect.x / card.pageWidth * 100}%`;
          mark.style.top = `${rect.y / card.pageHeight * 100}%`;
          mark.style.width = `${rect.width / card.pageWidth * 100}%`;
          mark.style.height = `${rect.height / card.pageHeight * 100}%`;
          if (rect.angle) { mark.style.transformOrigin = 'top left'; mark.style.transform = `rotate(${rect.angle}deg)`; }
          layer.append(mark);
        }
        card.scrollIntoView({ block: 'start' });
      });
      searchResults.append(button);
    }
    if (report.results.length > 100) status.textContent += '，仅显示前 100 条';
  }).catch(error => {
    if (currentClient === client && generation === documentGeneration && searchGeneration === requestGeneration && error.name !== 'AbortError') {
      status.textContent = `搜索失败：${error.message || error}`;
    }
  }).finally(() => { if (searchRequest === request) searchRequest = undefined; });
});

// 旧原型保留独立布局快照，仅用于历史效果对比；正式两端共用 viewer.js。
function textLayerSegments(runs) {
  const segments = [];
  for (const run of runs) {
    if (!run.text) continue;
    const angle = run.glyphs?.[0]?.angle ?? run.charDirection ?? 0;
    const previous = segments[segments.length - 1];
    const tolerance = previous ? Math.max(1, Math.min(run.height || 1, previous.height || 1) * 0.5) : 0;
    const mergeable = angle === 0 && previous && previous.angle === 0 &&
      previous.fontFamily === (run.fontFamily || '') && previous.weight === run.weight &&
      previous.bold === run.bold && previous.italic === run.italic && previous.size === run.size &&
      Math.abs(run.y - previous.y) <= tolerance &&
      run.x <= previous.x + previous.width + Math.max(run.height || 1, 1) * 2;
    if (mergeable) {
      previous.text += run.text;
      previous.width = Math.max(previous.width, run.x + run.width - previous.x);
      previous.height = Math.max(previous.height, run.height);
      continue;
    }
    segments.push({ x: run.x, y: run.y, width: run.width, height: run.height, text: run.text,
      fontFamily: run.fontFamily || '', weight: run.weight, bold: run.bold,
      italic: run.italic, size: run.size, angle });
  }
  return segments;
}

function buildText(layer, card, info, runs) {
  layer.replaceChildren();
  for (const segment of textLayerSegments(runs)) {
    const span = document.createElement('span');
    span.className = 'text-run';
    span.style.left = `${segment.x / info.width * 100}%`;
    span.style.top = `${segment.y / info.height * 100}%`;
    span.style.width = `${Math.max(segment.width / info.width * 100, 0.1)}%`;
    span.style.height = `${Math.max(segment.height / info.height * 100, 0.1)}%`;
    span.style.fontSize = `${Math.max(segment.size * card.clientWidth / info.width, 1)}px`;
    span.style.lineHeight = `${segment.height * card.clientWidth / info.width}px`;
    if (segment.fontFamily) span.style.fontFamily = `'${segment.fontFamily}', sans-serif`;
    span.style.fontWeight = segment.weight > 0 ? String(segment.weight) : (segment.bold ? '700' : '400');
    span.style.fontStyle = segment.italic ? 'italic' : 'normal';
    if (segment.angle) {
      span.style.transformOrigin = 'top left';
      span.style.transform = `rotate(${segment.angle}deg)`;
    }
    span.textContent = segment.text;
    layer.append(span);
  }
}

document.addEventListener('copy', event => {
  const selection = window.getSelection();
  if (!currentClient || !selection || selection.isCollapsed || !selection.rangeCount) return;
  const range = selection.getRangeAt(0);
  const indices = pageCards.flatMap((card, index) => range.intersectsNode(card) ? [index] : []);
  if (!indices.length) return;
  event.preventDefault();
  const value = selection.toString();
  const client = currentClient;
  const requestGeneration = generation;
  void client.checkPermission('copy', indices).then(async () => {
    if (generation !== requestGeneration || currentClient !== client || open.disabled) return;
    if (!navigator.clipboard?.writeText) throw new Error('当前 WebView 不支持安全剪贴板');
    await navigator.clipboard.writeText(value);
  }).catch(error => {
    if (generation !== requestGeneration) return;
    if (error.code === 'permission_denied') {
      generation++;
      for (const request of pendingText) request.cancel();
      pendingText.clear();
      for (const card of pageCards) {
        card.textObserver?.disconnect();
        card.querySelector('.text-layer')?.replaceChildren();
      }
      selection.removeAllRanges();
    }
    status.textContent = `复制失败：${error.message || error}`;
  });
});

open.addEventListener('click', async () => {
  open.disabled = true;
  try {
    const result = await globalThis.wails.Call.ByName('main.Desktop.Open');
    if (!result) return;
    clearSearch();
    generation++;
    for (const request of pendingText) request.cancel();
    pendingText.clear();
    status.textContent = `${result.name} · ${result.pages.length} 页`;
    const client = new NativePageClient(result.page_prefix);
    currentClient = client;
    for (const card of pageCards) card.textObserver?.disconnect();
    pageCards = [];
    pages.replaceChildren();
    for (let index = 0; index < result.pages.length; index++) {
      const info = { width: result.pages[index].width, height: result.pages[index].height };
      const card = document.createElement('div');
      card.className = 'page';
      card.pageWidth = info.width;
      card.pageHeight = info.height;
      card.style.aspectRatio = `${info.width} / ${info.height}`;
      const image = document.createElement('img');
      const layer = document.createElement('div');
      layer.className = 'text-layer';
      const highlights = document.createElement('div');
      highlights.className = 'highlight-layer';
      image.loading = 'lazy';
      image.alt = `第 ${index + 1} 页`;
      let loaded = false;
      image.addEventListener('load', () => {
        if (loaded || currentClient !== client) return;
        loaded = true;
        const requestGeneration = generation;
        const request = client.text(index);
        pendingText.add(request);
        request.then(runs => {
          if (currentClient !== client || generation !== requestGeneration) return;
          buildText(layer, card, info, runs);
          if (typeof ResizeObserver !== 'undefined') {
            const observer = new ResizeObserver(() => buildText(layer, card, info, runs));
            observer.observe(card);
            card.textObserver = observer;
          }
        }).catch(error => {
          if (error.name !== 'AbortError' && error.code !== 'permission_denied') console.warn('读取文字失败', error);
        }).finally(() => pendingText.delete(request));
      });
      image.src = client.pageURL(index, { dpi: 96 });
      card.append(image, layer, highlights);
      pageCards.push(card);
      pages.append(card);
    }
  } catch (error) {
    status.textContent = `打开失败：${error.message || error}`;
  } finally {
    open.disabled = false;
  }
});
