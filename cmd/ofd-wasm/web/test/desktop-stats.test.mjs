import assert from 'node:assert/strict';
import { load, sliceFunction } from './harness.mjs';

let resolveStats;
const state = {
  engine: { nativeDesktop: true, stats: () => new Promise(resolve => { resolveStats = resolve; }) },
  pageInfos: [{}], documentGeneration: 1, infoRenderRequest: 0, infoRenderedGeneration: -1,
  loadDocumentInfo: async () => ({ fonts: [] }),
  infoBasicContent: { innerHTML: '' }, infoTabPanels: { security: {}, fonts: {} }, infoStats: { innerHTML: '' },
  documentSecurityHTML: () => '', escapeHTML: text => text, resetDocumentInfo: () => {},
};
load(state, sliceFunction('updateDocumentInfo'), { expose: { update: 'updateDocumentInfo' } });
state.update();
await Promise.resolve();
assert.equal(typeof resolveStats, 'function', '桌面应请求资源统计');
resolveStats({ fonts: 2, attachments: 1, media: 3, annotation_pages: 4, signatures: 5 });
await Promise.resolve();
assert.match(state.infoStats.innerHTML, /资源统计/);
assert.match(state.infoStats.innerHTML, /注解页/);
assert.match(state.infoStats.innerHTML, />4</);

state.update();
await Promise.resolve();
state.documentGeneration++;
state.infoStats.innerHTML = '新文档';
resolveStats({ fonts: 99 });
await Promise.resolve();
assert.equal(state.infoStats.innerHTML, '新文档', '旧文档的统计不能覆盖新文档');
console.log('桌面资源统计与文档切换测试通过');
