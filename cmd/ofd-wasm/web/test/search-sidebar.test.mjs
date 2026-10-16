// 搜索侧栏回归测试：搜索入口、结果列表和侧栏页签必须保持同一套交互状态。
import { readFileSync } from 'node:fs';
import { load, sliceFunction, viewerPath, viewerSource, createSuite } from './harness.mjs';
import { dirname, join } from 'node:path';

const suite = createSuite('搜索侧栏');
const source = viewerSource();
const index = readFileSync(join(dirname(viewerPath), 'index.html'), 'utf8');

suite.group('搜索侧栏结构');
suite.check(index.includes('aria-controls="search-panel"') && index.includes('<aside id="search-panel"'),
  '顶部搜索入口和侧栏面板共用搜索面板标识');
suite.check((index.match(/id="search"/g) || []).length === 1,
  '搜索输入框 id 唯一');
suite.check(index.includes('id="sidebar-tab-search"') && source.includes("sidebarTabSearch?.addEventListener('click'"),
  '搜索页签有独立按钮和事件');
suite.check(source.includes("const sidebarTabs = ['thumbnails', 'outline', 'bookmarks', 'search'"),
  '搜索页签参与侧栏循环切换');

suite.group('结果呈现约束');
suite.check(source.includes('const searchResultDOMLimit = 200'),
  '搜索结果 DOM 有上限');
suite.check(source.includes('snippet.textContent = searchResultSnippet(result)'),
  '结果摘要使用文本节点避免把文档文字当 HTML');
suite.check(source.includes('selectSearchResult(Number(button.dataset.index))'),
  '点击结果复用统一跳转函数');
suite.check(!source.includes("event.target.closest('.search-group')") && source.includes("if (activeSidebarTab === 'search') setSearchPanelOpen(false)"),
  '搜索页签不会被顶部点击外部逻辑误关闭');

suite.group('表单交互');
suite.check(source.includes("searchForm.addEventListener('submit'") && source.includes('event.preventDefault();'),
  '提交搜索表单不会刷新页面');
suite.check(source.includes("searchInput.addEventListener('input'") && source.includes('searchRequest?.cancel();'),
  '修改查询会取消旧请求并清理旧高亮');

suite.group('搜索结果摘要');
const snippetState = load({ searchInput: { value: 'world' } }, sliceFunction('searchResultSnippet'), {
  expose: { snippet: 'searchResultSnippet' },
});
suite.check(snippetState.snippet({ text: 'hello world from an OFD document' }) === 'hello world from an OFD document',
  '短摘要保留结果原文');
suite.check(snippetState.snippet({ text: `prefix ${'x'.repeat(80)} world suffix` }).includes('world'),
  '长摘要包含命中词');

suite.group('命中的相邻文字合并为一个高亮区域');
const mergeState = load({}, sliceFunction('mergeHighlightRects'), {
  expose: { merge: 'mergeHighlightRects' },
});
{
  const merged = mergeState.merge([
    { x: 10, y: 20, width: 5, height: 8 },
    { x: 15, y: 20, width: 5, height: 8 },
    { x: 20, y: 20, width: 5, height: 8 },
  ]);
  suite.check(merged.length === 1, '同一行相邻字形合并为一个矩形', merged.length);
  suite.check(merged[0].x === 10 && merged[0].width === 15, '合并后覆盖整段文字', JSON.stringify(merged[0]));

  const wrapped = mergeState.merge([
    { x: 100, y: 20, width: 5, height: 8 },
    { x: 10, y: 30, width: 5, height: 8 },
  ]);
  suite.check(wrapped.length === 2, '换行后的字形不合并', wrapped.length);

  const rotated = mergeState.merge([
    { x: 10, y: 20, width: 5, height: 8, angle: 90 },
    { x: 10, y: 25, width: 5, height: 8, angle: 90 },
  ]);
  suite.check(rotated.length === 2, '旋转文字保持逐字矩形', rotated.length);

  const filtered = mergeState.merge([
    { x: 10, y: 20, width: 0, height: 8 },
    { x: Number.NaN, y: 20, width: 5, height: 8 },
    null,
  ]);
  suite.check(filtered.length === 0, '无效矩形被丢弃', filtered.length);

  const mixed = mergeState.merge([
    { x: 10, y: 20, width: 5, height: 8 },
    { x: 15, y: 21, width: 5, height: 8 },
    { x: 30, y: 20, width: 5, height: 8 },
  ]);
  suite.check(mixed.length === 2, '竖向重叠仍视为同一行，按出现位置分组', mixed.length);

  for (const [name, rects, count] of [
    ['相距很远的向左跳转', [{ x: 100, y: 20, width: 5, height: 8 }, { x: 10, y: 20, width: 5, height: 8 }], 2],
    ['相邻的从右向左文字', [{ x: 15, y: 20, width: 5, height: 8 }, { x: 10, y: 20, width: 5, height: 8 }], 1],
    ['紧行距换行', [{ x: 10, y: 20, width: 5, height: 8 }, { x: 15, y: 27.5, width: 5, height: 8 }], 2],
    ['连续小幅偏移不能跨行串联', [{ x: 10, y: 20, width: 5, height: 8 }, { x: 15, y: 22, width: 5, height: 8 }, { x: 20, y: 24, width: 5, height: 8 }], 2],
    ['非有限尺寸', [{ x: 10, y: 20, width: Infinity, height: 8 }], 0],
  ]) {
    const snapshot = JSON.stringify(rects);
    const result = mergeState.merge(rects);
    suite.check(result.length === count, name, result);
    suite.check(JSON.stringify(rects) === snapshot, `${name}不修改输入`);
  }
}

suite.group('文字层按字体排版高度设置行高');
{
  const layer = { parentElement: {}, children: [], replaceChildren() { this.children = []; }, append(node) { this.children.push(node); } };
  const state = load({
    pageCards: [{ clientWidth: 200, querySelector: () => layer }],
    pageInfos: [{ width: 100, height: 100 }],
    textCache: new Map([[0, [{ text: 'Hg', x: 10, y: 15, width: 10, height: 6, size: 5 }]]]),
    textLayerVisible: true,
    matchingSearchResults: () => [],
    document: { createElement: () => ({ style: {} }) },
  }, [sliceFunction('textLayerSegments'), sliceFunction('buildTextLayer')].join('\n'), {
    expose: { build: 'buildTextLayer' },
  });
  state.build(0);
  const text = layer.children[0];
  suite.check(text.style.fontSize === '10px', '字号按 Size 缩放', text.style.fontSize);
  suite.check(text.style.lineHeight === '12px', '行高按 Height 缩放，允许大于字号', text.style.lineHeight);
  suite.check(text.style.height === '6%', '覆盖层保留完整排版高度', text.style.height);
}

if (suite.report()) process.exitCode = 1;
