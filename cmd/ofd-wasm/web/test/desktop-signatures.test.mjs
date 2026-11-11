import assert from 'node:assert/strict';
import { load, sliceFunction } from './harness.mjs';

function element() {
  return { children: [], append(...items) { this.children.push(...items); }, replaceChildren(...items) { this.children = items; },
    setAttribute() {}, addEventListener(type, callback) { this[type] = callback; } };
}
const pending = [];
let cancelled = 0;
const panel = element();
const flush = () => new Promise(resolve => setImmediate(resolve));
const state = {
  engine: { nativeDesktop: true, signatureDetails: () => {
    const promise = new Promise((resolve, reject) => pending.push({ resolve, reject }));
    promise.cancel = () => { cancelled++; };
    return promise;
  }, checkPermission: async (action, pages) => { assert.equal(action, 'access'); assert.deepEqual(pages, [0]); if (state.deny) throw new Error('权限过期'); } },
  desktopSignatureRequest: undefined,
  signaturesElement: panel, signaturesRenderRequest: 0, documentGeneration: 1, pageInfos: [{}], sidebarFilterValue: '',
  document: { createElement: element }, revokeMediaURLs() {}, outlineEmptyMessage: text => text,
  signatureMethodLabel: value => value, goTo: page => { state.page = page; }, collapseSidebarAfterJump() {}, restorePanelScroll() {},
  scrollToCustomTagTargets: target => { state.target = target; }, setStatus: message => { state.status = message; },
  signatureSealIcon: () => element(), observeSealThumbnails() {},
  goToAnnotation: target => { state.page = target.page; },
  indexSealStamps: list => { state.indexed = list; }, applySealHotspots: () => { state.hotspots = true; },
};
load(state, ['buildSignatureSummary', 'buildSignatureStamp', 'loadDesktopSignatureSummaries', 'cancelDesktopSignatureRequest', 'renderSignatures'].map(sliceFunction).join('\n'),
  { expose: { render: 'renderSignatures', cancel: 'cancelDesktopSignatureRequest', buildSignatureItem: 'buildSignatureSummary' } });
state.render();
state.render();
assert.equal(pending.length, 1, '重复筛选复用进行中的请求');
pending[0].resolve([{ id: 's1', scope: 0, provider: '测试', version: '1.0', stamps: [{ id: 'a', page: 0, visible_boundary: { x: 1, y: 2, width: 3, height: 4 } }, { id: 'b', page: 0 }, { id: 'c', page: -1 }] }]);
await flush();
assert.match(panel.children[0], /未完成的检查不代表通过/);
assert.equal(state.hotspots, true, '侧栏重新读取可恢复页面热区');
const item = panel.children[1];
assert.equal(item.children[0].textContent, '测试');
await item.children[2].click();
assert.deepEqual(state.target, { page: 0, x: 1, y: 2, width: 3, height: 4 });
await item.children[3].click();
assert.equal(state.page, 0, '无边界时只跳转页面');
state.deny = true;
state.target = null;
await item.children[2].click();
assert.equal(state.target, null, '权限过期不能定位');
assert.match(state.status, /权限过期/);
state.deny = false;
let finishCheck;
state.engine.checkPermission = () => new Promise(resolve => { finishCheck = resolve; });
const staleJump = item.children[2].click();
state.documentGeneration++;
finishCheck();
await staleJump;
assert.equal(state.target, null, '切换文档后不能定位旧签章');
assert.match(item.children[1].textContent, /提供者版本：1.0/);
assert.match(item.children[2].children[1].textContent, /签章 a.*位置 1, 2.*3 × 4 mm/);
assert.match(item.children[3].children[1].textContent, /签章 b.*第 1 页/);
assert.equal(item.children[4].disabled, true, '未解析的页码不能跳转');
assert.equal(item.children.length, 5, '不提供证书、印章或签名值操作');
state.render();
assert.equal(pending.length, 2, '重新读取时必须重新检查访问权限');
state.documentGeneration++;
state.cancel();
assert.equal(cancelled, 1, '文档切换应取消请求');
panel.replaceChildren('新文档');
pending[1].resolve([{ id: 'old', pages: [] }]);
await flush();
assert.deepEqual(panel.children, ['新文档']);
state.render();
pending[2].reject(new Error('访问被拒绝'));
await flush();
assert.deepEqual(panel.children, ['获取签名失败']);
state.render();
assert.equal(pending.length, 4, '失败后允许重试');
pending[3].resolve([]);
await flush();
console.log('桌面未验签声明、跳转与文档切换测试通过');
