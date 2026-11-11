import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';

function element() {
  return { style: {}, children: [], listeners: {}, clientWidth: 200,
    addEventListener(type, callback) { this.listeners[type] = callback; },
    replaceChildren(...children) { this.children = children; },
    append(...children) { this.children.push(...children); },
    scrollIntoView() { this.scrolled = true; },
    querySelector(selector) { return this.children.find(child => `.${child.className}` === selector); },
  };
}
const elements = { open: element(), status: element(), pages: element(),
  'search-form': element(), 'search-query': element(), 'search-results': element() };
const listeners = {};
const prefix = `/reader/${'a'.repeat(64)}/pages/`;
let result = { name: '测试.ofd', page_prefix: prefix, pages: [{ width: 100, height: 200 }, { width: 100, height: 200 }] };
let denied = false;
let permissionWait;
let searchWait;
const copied = [];
const selection = { isCollapsed: false, rangeCount: 1, toString: () => 'Example',
  getRangeAt: () => ({ intersectsNode: () => true }), removeAllRanges() { this.removed = true; } };
class NativePageClient {
  constructor(token) { assert.equal(token, prefix); }
  pageURL(index) { return `${prefix}${index}`; }
  text() {
    const request = Promise.resolve([{ text: 'Example', x: 1, y: 2, width: 10, height: 4, size: 3 }]);
    request.cancel = () => {};
    return request;
  }
  async checkPermission(action, indices) {
    assert.equal(action, 'copy');
    assert.deepEqual(Array.from(indices), [0, 1]);
    if (permissionWait) await permissionWait;
    if (denied) throw Object.assign(new Error('禁止复制'), { code: 'permission_denied' });
  }
  search(query) {
    assert.equal(query, 'Example');
    const request = searchWait || Promise.resolve({ scanned: 2, truncated: false, results: [{ page: 0, text: 'Example',
      rects: [{ x: 1, y: 2, width: 10, height: 4, angle: 0 }] }] });
    request.cancel = () => {};
    return request;
  }
}
const context = vm.createContext({
  document: { getElementById: id => elements[id], createElement: element,
    addEventListener(type, callback) { listeners[type] = callback; } },
  window: { getSelection: () => selection }, navigator: { clipboard: { writeText: async text => copied.push(text) } },
  NativePageClient, console,
  wails: { Call: { ByName: async name => { assert.equal(name, 'main.Desktop.Open'); return result; } } },
});
const source = readFileSync(new URL('../prototype/prototype.js', import.meta.url), 'utf8');
vm.runInContext(source.replace("import { NativePageClient } from './native-page.js';", '')
  .replace("await import('/wails/runtime.js');", ''), context);
await elements.open.listeners.click();
const card = elements.pages.children[0];
const [image, layer] = card.children;
assert.equal(image.src, `${prefix}0`);
image.listeners.load();
await new Promise(resolve => setImmediate(resolve));
assert.equal(layer.children[0].textContent, 'Example');
assert.equal(layer.children[0].style.left, '1%');
assert.equal(layer.children[0].style.fontSize, '6px');
elements['search-query'].value = 'Example';
elements['search-form'].listeners.submit({ preventDefault() {} });
await new Promise(resolve => setImmediate(resolve));
assert.equal(elements['search-results'].children[0].textContent, '第 1 页 · 匹配');
elements['search-results'].children[0].listeners.click();
assert.equal(card.scrolled, true);
assert.equal(card.querySelector('.highlight-layer').children[0].style.left, '1%');
let prevented = false;
listeners.copy({ preventDefault() { prevented = true; } });
await new Promise(resolve => setImmediate(resolve));
assert.equal(prevented, true);
assert.deepEqual(copied, ['Example']);
let resume;
permissionWait = new Promise(resolve => { resume = resolve; });
listeners.copy({ preventDefault() {} });
let finishSearch;
searchWait = new Promise(resolve => { finishSearch = resolve; });
elements['search-form'].listeners.submit({ preventDefault() {} });
await elements.open.listeners.click();
resume();
finishSearch({ results: [{ page: 0, text: '旧文档', rects: [] }], truncated: false });
await new Promise(resolve => setImmediate(resolve));
assert.deepEqual(copied, ['Example'], '切换文档后旧复制不能写入剪贴板');
assert.equal(elements['search-results'].children.length, 0, '切换文档后旧搜索不能更新结果');
permissionWait = undefined;
denied = true;
listeners.copy({ preventDefault() {} });
await new Promise(resolve => setImmediate(resolve));
assert.deepEqual(copied, ['Example']);
assert.equal(elements.pages.children[0].querySelector('.text-layer').children.length, 0);
assert.equal(selection.removed, true);
result = null;
await elements.open.listeners.click();
assert.equal(elements.pages.children.length, 2, '取消文件对话框时保留当前文档');
console.log('Wails 旧原型独立文字布局与复制权限测试通过');
