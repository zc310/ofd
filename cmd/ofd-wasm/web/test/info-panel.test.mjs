import assert from 'node:assert/strict';
import { createSuite, load, sliceFunction } from './harness.mjs';

const suite = createSuite('文档信息面板');
const tick = () => new Promise(resolve => setImmediate(resolve));
const element = () => ({ append() {}, appendChild() {} });
const state = {
  documentGeneration: 1,
  pageInfos: [{}],
  infoBody: { innerHTML: '', append() {} },
  loadDocumentInfo: async () => ({
    title: '测试文档',
    fonts: [],
    fonts_error: '第 8 页路径数据解析失败',
  }),
  escapeHTML: value => value,
  engine: { stats: async () => ({ fonts: 1, attachments: 0, media: 0, annotation_pages: 0, signatures: 0 }) },
  document: { createElement: element },
};
load(state, sliceFunction('updateDocumentInfo'), { expose: { update: 'updateDocumentInfo' } });
state.update();
await tick();
await tick();
assert.match(state.infoBody.innerHTML, /测试文档/);
assert.match(state.infoBody.innerHTML, /字体列表读取失败：第 8 页路径数据解析失败/);
suite.check(true, '字体资源扫描失败仍展示文档信息和具体诊断');
process.exitCode = suite.report() ? 1 : 0;
