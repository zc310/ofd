import assert from 'node:assert/strict';
import { load, sliceFunction } from './harness.mjs';

function panel() {
  return {
    children: [],
    replaceChildren(...children) { this.children = children; },
    append(...children) { this.children.push(...children); },
  };
}
function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
const flush = async () => { await Promise.resolve(); await Promise.resolve(); await Promise.resolve(); };

for (const kind of ['attachments', 'fonts']) {
  const requests = [];
  const next = () => { const request = deferred(); requests.push(request); return request.promise; };
  const target = panel();
  const state = {
    attachmentsElement: target, fontsElement: target, documentGeneration: 1,
    attachmentsRenderRequest: 0, fontsRenderRequest: 0, pageInfos: [{}], sidebarFilterValue: '',
    engine: { attachments: next }, loadDocumentInfo: next,
    outlineEmptyMessage: text => text, restorePanelScroll() {}, applyFontUsageLabels() {},
    document: { createElement: () => panel() },
    buildAttachmentItem: item => item.name, buildFontItem: item => item.name,
  };
  const functionName = kind === 'fonts' ? 'renderFonts' : 'renderAttachments';
  const data = name => kind === 'fonts' ? { fonts: [{ name }] } : [{ name }];
  load(state, sliceFunction(functionName), { expose: { render: functionName } });

  state.render();
  state.render();
  requests[1].resolve(data('新结果'));
  await flush();
  const current = target.children;
  requests[0].reject(new Error('旧请求失败'));
  await flush();
  assert.equal(target.children, current, `${kind}: 旧错误不能覆盖新结果`);

  state.render();
  state.render();
  requests[3].reject(new Error('新请求失败'));
  await flush();
  assert.match(target.children[0], /失败/);
  requests[2].resolve(data('旧结果'));
  await flush();
  assert.match(target.children[0], /失败/, `${kind}: 旧结果不能覆盖新错误`);

  state.render();
  state.documentGeneration++;
  target.replaceChildren('新文档');
  requests[4].resolve(data('旧文档'));
  await flush();
  assert.deepEqual(target.children, ['新文档'], `${kind}: 旧文档结果不能覆盖新文档`);

  state.render();
  state.pageInfos = [];
  state.render();
  requests[5].resolve(data('已关闭'));
  await flush();
  assert.deepEqual(target.children, ['未打开文档'], `${kind}: 清空面板后不能显示旧结果`);
}
console.log('字体和附件面板请求乱序测试通过');
