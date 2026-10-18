'use strict';

const selector = document.getElementById('target');
const only = document.getElementById('changed-only');
let current = -1;
let selectionSequence = 0;
const regionRequests = new WeakMap();

function activeDifferences() {
  const section = document.getElementById(selector.value);
  if (!section) return [];
  return Array.from(section.querySelectorAll('.page[data-changed="true"]')).flatMap(page => {
    const regions = Array.from(page.querySelectorAll('.region-button'));
    return regions.length ? regions : [page];
  });
}

function update() {
  selectionSequence++;
  document.querySelectorAll('.comparison').forEach(el => {
    el.classList.toggle('hidden', el.id !== selector.value);
    el.querySelectorAll('.page').forEach(page => {
      page.classList.toggle('hidden', only.checked && page.dataset.changed !== 'true');
      page.classList.remove('active');
    });
  });
  current = -1;
  document.getElementById('position').textContent = '';
}

function applyZoom(view) {
  const zoom = Number(view.querySelector('.region-zoom').value);
  view.querySelector('output').textContent = zoom + '×';
  view.querySelectorAll('canvas').forEach(canvas => {
    canvas.style.width = canvas.width * zoom + 'px';
    canvas.style.height = canvas.height * zoom + 'px';
  });
}

async function selectRegion(button) {
  const page = button.closest('.page');
  const view = page.querySelector('.region-view');
  // 每个局部视图独立处理异步请求，切换目标不会遗留被取消的加载状态。
  const request = ++selectionSequence;
  regionRequests.set(view, request);
  const bounds = button.dataset.bounds.split(',').map(Number);
  page.querySelectorAll('.region-button').forEach(el => el.setAttribute('aria-pressed', String(el === button)));
  page.querySelectorAll('.region-map rect').forEach(el => el.classList.toggle('selected', el.dataset.region === button.dataset.region));
  view.open = true;
  view.querySelector('.region-title').textContent = '· 区域 ' + (Number(button.dataset.region) + 1);
  const position = view.querySelector('.region-position');
  position.textContent = '正在加载局部图像…';
  const images = Array.from(page.querySelectorAll('.visuals img[data-image]'));
  try {
    // 源图延迟加载；用户选择区域后再解码该页的三幅图像。
    await Promise.all(images.map(img => { img.loading = 'eager'; return img.decode(); }));
    if (request !== regionRequests.get(view)) return;
    const width = Math.max(...images.map(img => img.naturalWidth));
    const height = Math.max(...images.map(img => img.naturalHeight));
    const x = Math.max(0, bounds[0] - 6), y = Math.max(0, bounds[1] - 6);
    const w = Math.min(width, bounds[2] + 6) - x, h = Math.min(height, bounds[3] + 6) - y;
    for (const canvas of view.querySelectorAll('canvas')) {
      canvas.width = w;
      canvas.height = h;
      const ctx = canvas.getContext('2d');
      ctx.fillStyle = 'white';
      ctx.fillRect(0, 0, w, h);
      const source = images.find(img => img.dataset.image === canvas.dataset.crop);
      // 页尺寸不同或一侧缺页时，超出源图的区域保留白底。
      if (source && x < source.naturalWidth && y < source.naturalHeight) {
        const sw = Math.min(w, source.naturalWidth - x), sh = Math.min(h, source.naturalHeight - y);
        ctx.drawImage(source, x, y, sw, sh, 0, 0, sw, sh);
      }
    }
    applyZoom(view);
    position.textContent = '像素范围 [' + bounds.join(', ') + ']';
    if (request === selectionSequence) view.scrollIntoView({behavior: 'smooth', block: 'nearest'});
  } catch (error) {
    if (request === regionRequests.get(view)) position.textContent = '局部图像加载失败：' + error.message;
  }
}

function focusDifference(target) {
  const page = target.closest('.page');
  document.querySelectorAll('.page.active').forEach(el => el.classList.remove('active'));
  page.classList.add('active');
  page.scrollIntoView({behavior: 'smooth', block: 'center'});
  if (target.classList.contains('region-button')) {
    selectRegion(target);
  } else {
    // 没有视觉区域时仍可浏览文字变化、移页和已过滤的小区域页面。
    selectionSequence++;
    const text = page.querySelector('.text-diff');
    if (text) text.open = true;
  }
}

function step(direction) {
  const differences = activeDifferences();
  if (!differences.length) {
    document.getElementById('position').textContent = '没有页面差异';
    return;
  }
  current = current < 0 ? (direction < 0 ? differences.length - 1 : 0) : (current + direction + differences.length) % differences.length;
  focusDifference(differences[current]);
  document.getElementById('position').textContent = (current + 1) + ' / ' + differences.length;
}

selector.addEventListener('change', update);
only.addEventListener('change', update);
document.getElementById('next').addEventListener('click', () => step(1));
document.getElementById('previous').addEventListener('click', () => step(-1));
document.querySelectorAll('.region-button').forEach(button => button.addEventListener('click', () => {
  const differences = activeDifferences();
  current = differences.indexOf(button);
  focusDifference(button);
  document.getElementById('position').textContent = (current + 1) + ' / ' + differences.length;
}));
document.querySelectorAll('.region-map rect').forEach(rect => {
  const activate = () => rect.closest('.page').querySelector('.region-button[data-region="' + rect.dataset.region + '"]').click();
  rect.addEventListener('click', activate);
  rect.addEventListener('keydown', event => {
    if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); activate(); }
  });
});
document.querySelectorAll('.region-zoom').forEach(el => el.addEventListener('input', () => applyZoom(el.closest('.region-view'))));
document.querySelectorAll('.region-view').forEach(view => {
  view.addEventListener('toggle', () => {
    const page = view.closest('.page');
    if (view.open && !page.querySelector('.region-button[aria-pressed="true"]')) page.querySelector('.region-button').click();
  });
  // 三幅局部图同步滚动，放大后仍可比较同一位置。
  let syncing = false;
  view.querySelectorAll('.region-scroll').forEach(el => el.addEventListener('scroll', () => {
    if (syncing) return;
    syncing = true;
    view.querySelectorAll('.region-scroll').forEach(other => { if (other !== el) { other.scrollLeft = el.scrollLeft; other.scrollTop = el.scrollTop; } });
    requestAnimationFrame(() => { syncing = false; });
  }));
});
document.querySelectorAll('.opacity').forEach(el => el.addEventListener('input', () => {
  el.closest('details').querySelector('.after').style.opacity = Number(el.value) / 100;
}));
document.querySelectorAll('.overlay-view').forEach(view => view.addEventListener('toggle', async () => {
  if (!view.open) return;
  const images = Array.from(view.querySelectorAll('.overlay img'));
  try {
    await Promise.all(images.map(img => { img.loading = 'eager'; return img.decode(); }));
    // 页尺寸不同时沿用实际像素比例，避免把目标页拉伸到基准页尺寸。
    const width = Math.max(...images.map(img => img.naturalWidth));
    const height = Math.max(...images.map(img => img.naturalHeight));
    const overlay = view.querySelector('.overlay');
    overlay.style.width = '100%';
    overlay.style.aspectRatio = width + ' / ' + height;
    images.forEach(img => { img.style.position = 'absolute'; img.style.left = '0'; img.style.top = '0'; img.style.width = img.naturalWidth / width * 100 + '%'; });
  } catch (error) {
    // 原图仍可在并排视图查看，叠加失败不影响其他差异导航。
    view.querySelector('summary').textContent = '透明叠加（图像加载失败）';
  }
}));

// 编辑器按目标独立初始化，避免大文档在打开报告时遍历所有输入框。
document.querySelectorAll('.page-map-editor').forEach(editor => {
  let initialized = false;
  editor.addEventListener('toggle', () => {
    if (!editor.open || initialized) return;
    initialized = true;
    const targetPages = new Set(editor.dataset.targetPages.split(',').filter(Boolean).map(Number));
    const rows = Array.from(editor.querySelectorAll('.page-map-row')).map(row => ({
      row, before: Number(row.dataset.before), input: row.querySelector('.page-map-after'),
      error: row.querySelector('.page-map-row-error'),
    }));
    const output = editor.querySelector('.page-map-output');
    const errors = editor.querySelector('.page-map-errors');
    const message = editor.querySelector('.page-map-message');
    const copy = editor.querySelector('.page-map-copy');
    const download = editor.querySelector('.page-map-download');
    let revision = 0;
    function refresh() {
      revision++;
      const sourcePages = new Set();
      const targets = new Map();
      const pairs = [];
      let invalid = 0;
      message.textContent = '';
      const mark = (item, text) => { item.error.textContent = text; item.input.setAttribute('aria-invalid', 'true'); };
      for (const item of rows) {
        item.error.textContent = '';
        item.input.setAttribute('aria-invalid', 'false');
        const value = item.input.value.trim();
        if (!Number.isSafeInteger(item.before) || item.before < 1 || sourcePages.has(item.before)) {
          mark(item, '基准页码无效或重复');
          continue;
        }
        sourcePages.add(item.before);
        if (value === '') continue;
        const after = Number(value);
        if (!/^\d+$/.test(value) || !Number.isSafeInteger(after) || after < 1) {
          mark(item, '请输入正整数页码，或清空以自动匹配');
        } else if (!targetPages.has(after)) {
          mark(item, '该目标页不在本报告的选页范围内');
        } else {
          if (!targets.has(after)) targets.set(after, []);
          targets.get(after).push(item);
          pairs.push([item.before, after]);
        }
      }
      for (const [after, items] of targets) {
        if (items.length > 1) items.forEach(item => mark(item, '目标第 ' + after + ' 页重复使用，请调整对应行'));
      }
      invalid = rows.filter(item => item.input.getAttribute('aria-invalid') === 'true').length;
      // 页码范围只允许数字、区间和分隔符，输出中不拼接输入文件名。
      const scope = editor.dataset.pages.trim();
      const scopeValid = scope === '' || /^\d+(?:\s*-\s*\d+)?(?:\s*,\s*\d+(?:\s*-\s*\d+)?)*$/.test(scope);
      const problem = invalid ? '有 ' + invalid + ' 行映射无效，请修正标红的输入框。' :
        !scopeValid ? '报告中的页码范围无效，无法生成参数。' : '';
      errors.textContent = problem;
      errors.classList.toggle('hidden', problem === '');
      pairs.sort((a, b) => a[0] - b[0]);
      const valid = problem === '' && pairs.length > 0;
      output.value = valid ? '--page-match auto' + (scope ? ' --pages "' + scope.replace(/\s+/g, '') + '"' : '') +
        ' --page-map "' + pairs.map(pair => pair.join(':')).join(',') + '"' : '';
      copy.disabled = download.disabled = !valid;
      editor.querySelector('.page-map-count').textContent = pairs.length + ' / ' + rows.length + ' 页已指定映射';
      if (!problem && pairs.length === 0) message.textContent = '未指定映射，重新运行时全部自动匹配。';
    }
    editor.querySelector('.page-map-rows').addEventListener('input', refresh);
    editor.querySelector('.page-map-reset').addEventListener('click', () => {
      rows.forEach(item => { item.input.value = item.input.defaultValue; });
      refresh();
    });
    editor.querySelector('.page-map-clear').addEventListener('click', () => {
      rows.forEach(item => { item.input.value = ''; });
      refresh();
    });
    copy.addEventListener('click', async () => {
      const text = output.value;
      const request = revision;
      try {
        if (!navigator.clipboard || !navigator.clipboard.writeText) throw new Error('clipboard unavailable');
        await navigator.clipboard.writeText(text);
        if (request === revision) message.textContent = '参数已复制，重新运行对比后生效。';
      } catch (error) {
        if (request !== revision) return;
        const active = document.activeElement;
        output.focus();
        output.select();
        let copied = false;
        try { copied = document.execCommand('copy'); } catch (fallbackError) { /* 保留选区供手工复制。 */ }
        message.textContent = copied ? '参数已复制，重新运行对比后生效。' : '参数已选中，请按 Ctrl+C 或 Command+C 复制。';
        if (copied && active) active.focus({preventScroll: true});
      }
    });
    download.addEventListener('click', () => {
      const uri = URL.createObjectURL(new Blob([output.value + '\n'], {type: 'text/plain;charset=utf-8'}));
      const link = document.createElement('a');
      link.href = uri;
      link.download = 'ofd-page-map-target-' + editor.dataset.target + '.txt';
      document.body.append(link);
      link.click();
      link.remove();
      setTimeout(() => URL.revokeObjectURL(uri), 1000);
      message.textContent = '已生成参数下载，重新运行对比后生效。';
    });
    refresh();
  });
});

document.querySelectorAll('.xml-view').forEach(view => {
  const common = Array.from(view.querySelectorAll('.xml-common'));
  const expand = view.querySelector('.xml-expand');
  const collapse = view.querySelector('.xml-collapse');
  const status = view.querySelector('.xml-fold-status');
  function refresh() {
    const closed = common.filter(el => !el.open).length;
    expand.disabled = closed === 0;
    collapse.disabled = closed === common.length;
    status.textContent = !common.length ? '没有相同行可折叠' :
      closed === 0 ? '相同内容已全部展开' : '已折叠 ' + closed + ' / ' + common.length + ' 处相同内容';
  }
  function setExpanded(open) {
    // 短片段及上下文同样参与按钮操作，长片段仍按默认策略初始折叠。
    common.forEach(el => { el.open = open; });
    refresh();
  }
  expand.addEventListener('click', () => setExpanded(true));
  collapse.addEventListener('click', () => setExpanded(false));
  common.forEach(el => el.addEventListener('toggle', refresh));
  refresh();
});

// 每份目标保留自己的筛选条件；只筛选清单，不改变实际差异及页面导航。
document.querySelectorAll('.file-list').forEach(list => {
  const search = list.querySelector('.file-search');
  const changed = list.querySelector('.files-changed-only');
  const status = list.querySelector('.file-status');
  const type = list.querySelector('.file-type');
  const rows = Array.from(list.querySelectorAll('.file-row')).map(row => ({
    row,
    paths: Array.from(row.querySelectorAll('code')).map(code => code.textContent.toLowerCase()),
  }));
  const detailRows = new Map();
  for (const {row} of rows) {
    const link = row.querySelector('.file-detail-link');
    if (link) detailRows.set(link.hash.slice(1), row);
  }
  list.closest('.comparison').querySelectorAll('.file-back').forEach(button => button.addEventListener('click', () => {
    selectionSequence++;
    const row = detailRows.get(button.closest('.file').id);
    list.open = true;
    list.querySelectorAll('.file-returned').forEach(el => el.classList.remove('file-returned'));
    if (row && !row.classList.contains('hidden')) {
      row.classList.add('file-returned');
      // 聚焦容器，避免键盘抬键时意外再次进入链接或折叠清单。
      row.tabIndex = -1;
      row.focus({preventScroll: true});
      row.scrollIntoView({behavior: 'smooth', block: 'center'});
    } else {
      search.focus({preventScroll: true});
      list.scrollIntoView({behavior: 'smooth', block: 'start'});
    }
  }));
  function filterFiles() {
    const query = search.value.trim().toLowerCase();
    let visible = 0;
    for (const {row, paths} of rows) {
      const match = (!query || paths.some(path => path.includes(query))) &&
        (!changed.checked || row.dataset.status !== 'unchanged') &&
        (!status.value || row.dataset.status === status.value) &&
        (!type.value || row.dataset.beforeKind === type.value || row.dataset.afterKind === type.value);
      row.classList.toggle('hidden', !match);
      if (match) visible++;
    }
    list.querySelector('.file-count').textContent = '显示 ' + visible + ' / ' + rows.length + ' 个文件';
    list.querySelector('.file-empty').classList.toggle('hidden', visible !== 0 || rows.length === 0);
  }
  search.addEventListener('input', filterFiles);
  [changed, status, type].forEach(control => control.addEventListener('change', filterFiles));
  list.querySelector('.file-filter-reset').addEventListener('click', () => {
    search.value = status.value = type.value = '';
    changed.checked = false;
    filterFiles();
    search.focus();
  });
  // 使用数值锚点定位详情，路径中的引号和特殊字符不会参与选择器构造。
  list.querySelector('.file-comparison').addEventListener('click', event => {
    if (event.button !== 0 || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
    const row = event.target.closest('.file-row');
    const link = row && row.querySelector('.file-detail-link');
    if (!link || (!event.target.closest('a') && window.getSelection().toString())) return;
    const detail = document.getElementById(link.hash.slice(1));
    if (!detail) return;
    event.preventDefault();
    selectionSequence++;
    list.querySelectorAll('.file-returned').forEach(el => el.classList.remove('file-returned'));
    document.querySelectorAll('.file.active').forEach(el => el.classList.remove('active'));
    detail.open = true;
    detail.classList.add('active');
    // 焦点移至详情容器，避免 Enter 的抬键事件再次触发 summary 并关闭详情。
    detail.focus({preventScroll: true});
    detail.scrollIntoView({behavior: 'smooth', block: 'start'});
  });
  filterFiles();
});
update();
