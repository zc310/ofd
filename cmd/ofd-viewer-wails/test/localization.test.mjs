import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';

const messages = JSON.parse(readFileSync(new URL('../locales/en.json', import.meta.url)));
const source = readFileSync(new URL('../localization.js', import.meta.url), 'utf8');
function run(language) {
  const nodes = ['打开文件', '搜索', '打开文件', '打开文件'].map((nodeValue, index) => ({
    nodeValue, parentElement: { closest: () => index >= 2 ? {} : null },
  }));
  const attributes = { title: '打开文件', 'aria-label': '搜索', placeholder: '搜索文字' };
  const element = { hasAttribute: name => name in attributes,
    getAttribute: name => attributes[name], setAttribute: (name, value) => { attributes[name] = value; } };
  const document = { documentElement: {}, body: { querySelectorAll: () => [element] },
    createTreeWalker() { let i = 0; return { nextNode() { this.currentNode = nodes[i++]; return !!this.currentNode; } }; } };
  const context = vm.createContext({ document, NodeFilter: { SHOW_TEXT: 4 },
    ofdReaderLanguage: language, ofdReaderMessages: messages });
  vm.runInContext(source, context);
  return { nodes, attributes, document, context };
}
const english = run('en');
assert.equal(english.document.documentElement.lang, 'en');
assert.equal(english.nodes[0].nodeValue, 'Open file');
assert.equal(english.nodes[1].nodeValue, 'Search');
assert.equal(english.nodes[2].nodeValue, '打开文件', '不能翻译脚本、样式或图标');
assert.equal(english.attributes.placeholder, 'Search text');
english.nodes.push({ nodeValue: '打开文件' });
assert.equal(english.nodes.at(-1).nodeValue, '打开文件', '后续加载的正文不进入界面翻译器');
const chinese = run('zh');
assert.equal(chinese.document.documentElement.lang, 'zh-CN');
assert.equal(chinese.nodes[0].nodeValue, '打开文件');
console.log('中英文静态界面、无障碍标签和文档内容隔离测试通过');
