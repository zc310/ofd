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

if (suite.report()) process.exitCode = 1;
