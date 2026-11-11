import assert from 'node:assert/strict';
import { load, sliceFunction } from './harness.mjs';

const panel = { children: [], replaceChildren(...items) { this.children = items; }, append(...items) { this.children.push(...items); } };
let resolve, calls = 0, checks = 0, cancelled = 0, deny = false;
const flush = () => new Promise(resolve => setImmediate(resolve));
const state = {
  engine: { nativeDesktop: true, annotationReport: () => { calls++; const promise = new Promise(yes => { resolve = yes; }); promise.cancel = () => { cancelled++; }; return promise; },
    checkPermission: async (action, indices) => { checks++; assert.equal(action, 'access'); assert.deepEqual(indices, [0]); if (deny) throw new Error('权限已过期'); } },
  annotationsElement: panel, documentGeneration: 1, annotationsRenderRequest: 0, pageInfos: [{}], sidebarFilterValue: '', desktopAnnotationRequest: undefined,
  annotationInfos: new Map(), annotationsPromises: new Map(),
  outlineEmptyMessage: text => text, buildAnnotationItem: item => item.remark,
  restorePanelScroll() {}, loadAnnotationInfos: () => { throw new Error('桌面端不能扫描未经限制的清单'); },
};
load(state, ['loadDesktopAnnotationReport', 'cancelDesktopAnnotationRequest', 'renderAnnotations'].map(sliceFunction).join('\n'), { expose: { render: 'renderAnnotations', cancel: 'cancelDesktopAnnotationRequest' } });
state.render();
state.render();
assert.equal(calls, 1, '筛选时复用进行中的扫描');
resolve({ items: [{ remark: '备注内容' }], scanned: 1, truncated: true });
await flush();
assert.match(panel.children[0], /结果可能不完整/);
assert.equal(panel.children[1], '备注内容');
state.sidebarFilterValue = '未匹配';
state.render();
await flush();
assert.equal(calls, 1, '筛选时复用已有报告');
assert.equal(checks, 1, '复用前重新检查访问权限');
assert.match(panel.children[0], /结果可能不完整/);
assert.equal(panel.children[1], '无匹配结果');

deny = true;
state.render();
await flush();
assert.deepEqual(panel.children, ['获取注解失败'], '权限过期时不能显示缓存清单');
deny = false;
state.annotationInfos.clear();
state.render();
state.documentGeneration++;
state.cancel();
assert.equal(cancelled, 1, '文档切换时取消扫描');
state.annotationInfos.clear();
panel.replaceChildren('新文档');
resolve({ items: [{ remark: '旧文档' }], scanned: 1, truncated: false });
await flush();
assert.deepEqual(panel.children, ['新文档']);
assert.equal(state.annotationInfos.size, 0, '旧报告不能写入新文档缓存');
console.log('桌面注解扫描报告及文档切换测试通过');
