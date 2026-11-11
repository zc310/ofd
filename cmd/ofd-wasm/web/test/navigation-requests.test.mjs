import assert from 'node:assert/strict';
import { load, sliceFunction } from './harness.mjs';

const requests = { tags: [], versions: [] };
const pending = kind => {
  let resolve, reject;
  const request = new Promise((done, fail) => { resolve = done; reject = fail; });
  const entry = { resolve, reject, cancelled: 0 };
  request.cancel = () => { entry.cancelled++; };
  requests[kind].push(entry);
  return request;
};
const panel = { children: [], append(...items) { this.children.push(...items); }, replaceChildren(...items) { this.children = items; } };
const state = {
  engine: { customTags: () => pending('tags'), versions: () => pending('versions') },
  documentGeneration: 1, customTagTrees: [], customTagsError: '', customTagsRequest: undefined, versionsRequest: undefined,
  pageInfos: [{}], versionSwitching: false, sidebarFilterValue: '', versionsElement: panel,
  outlineEmptyMessage: value => value, buildVersionItem: item => item.id, restorePanelScroll() {},
  renderCustomTags() { state.renders++; }, renders: 0,
};
load(state, ['loadCustomTags', 'renderVersions', 'cancelNavigationRequests'].map(sliceFunction).join('\n'),
  { expose: { tags: 'loadCustomTags', versions: 'renderVersions', cancel: 'cancelNavigationRequests' } });
const flush = () => new Promise(resolve => setImmediate(resolve));

const first = state.tags(), second = state.tags();
assert.equal(requests.tags[0].cancelled, 1, '重复读取标签取消旧任务');
requests.tags[1].resolve(['最新标签']); await second;
requests.tags[0].reject(new Error('旧 XML 失败')); await first;
assert.deepEqual(state.customTagTrees, ['最新标签']);
assert.equal(state.customTagsError, '');
assert.equal(state.renders, 1, '同文档迟到的标签失败不触发重绘');

state.versions(); await flush();
state.versions(); await flush();
assert.equal(requests.versions[0].cancelled, 1, '重复筛选版本取消旧任务');
requests.versions[1].resolve([{ id: 'v2' }]); await flush();
assert.deepEqual(panel.children, ['v2']);
requests.versions[0].reject(new Error('旧版本查询失败')); await flush();
assert.deepEqual(panel.children, ['v2'], '旧错误不能覆盖当前版本清单');

const tags = state.tags(); state.versions(); await flush();
state.cancel(); state.documentGeneration++;
assert.equal(requests.tags[2].cancelled, 1);
assert.equal(requests.versions[2].cancelled, 1);
requests.tags[2].resolve(['旧标签']); requests.versions[2].resolve([{ id: 'old' }]);
await tags; await flush();
assert.deepEqual(state.customTagTrees, ['最新标签']);
assert.ok(!panel.children.includes('old'), '切换后迟到结果不创建旧版本按钮');
assert.equal(state.customTagsRequest, undefined);
assert.equal(state.versionsRequest, undefined);

state.versions(); state.cancel(); await flush();
assert.equal(requests.versions.length, 3, '尚未发送的版本读取可直接取消');
state.versionSwitching = true; state.versions(); await flush();
assert.equal(requests.versions.length, 3);
assert.match(panel.children[0], /正在切换版本/);
console.log('标签与版本读取取消、同文档乱序响应、切换隔离及请求释放测试通过');
