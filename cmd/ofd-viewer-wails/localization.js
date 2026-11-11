// 只在阅读器脚本加载前处理静态外壳；不监听或翻译后续文档 DOM。
(() => {
  const english = globalThis.ofdReaderLanguage === 'en';
  const messages = globalThis.ofdReaderMessages;
  const text = source => english ? (messages[source] ?? source.replace(/\p{Script=Han}+/gu, part => messages[part] ?? part)) : source;
  globalThis.ofdDesktopText = text;
  document.documentElement.lang = english ? 'en' : 'zh-CN';
  document.title = english ? 'OFD Reader' : 'OFD 阅读器';
  if (!english) return;
  const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  const nodes = [];
  while (walker.nextNode()) nodes.push(walker.currentNode);
  for (const node of nodes) {
    if (!node.parentElement.closest('script, style, .material-symbols-outlined')) node.nodeValue = text(node.nodeValue);
  }
  for (const element of document.body.querySelectorAll('*')) {
    for (const name of ['title', 'aria-label', 'placeholder']) {
      if (element.hasAttribute(name)) element.setAttribute(name, text(element.getAttribute(name)));
    }
  }
})();
