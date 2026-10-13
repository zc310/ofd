// 直接驱动版本按钮与页面筛选，确保无法显示的版本不会清空正文。
import { sliceFunction, load, createSuite } from './harness.mjs';

const suite = createSuite('文档版本');
const code = [sliceFunction('buildVersionItem'), sliceFunction('applyVersionPages')].join('\n');

function fakeButton() {
  const button = { disabled: false, textContent: '', title: '', listeners: new Map(), classes: new Set() };
  button.classList = { add: name => button.classes.add(name) };
  button.addEventListener = (name, handler) => button.listeners.set(name, handler);
  button.click = () => button.listeners.get('click')?.();
  return button;
}

function baseState() {
  const state = {
    selectedVersionID: '',
    versionPageFilter: null,
    pageInfos: [{}, {}],
    pageCards: [{ hidden: false }, { hidden: false }],
    thumbnailButtons: [],
    activeThumbnailButton: undefined,
    mountedThumbnailIndexes: new Set(),
    thumbnailSlots: [],
    thumbnailSlotByPage: [],
    document: { createElement: () => fakeButton() },
    updateThumbnailMetrics: () => {},
    updateThumbnailVirtualWindow: () => {},
    renderVersions: () => {},
    goTo: page => { state.lastPage = page; },
  };
  state.thumbnailSlotsForLayout = () => state.pageCards.map((card, index) => card.hidden ? -1 : index).filter(index => index >= 0);
  return load(state, code, { expose: { buildVersionItem: 'buildVersionItem' } });
}

suite.group('初始版本筛选一页，再次点击恢复全部页面');
{
  const state = baseState();
  const button = state.buildVersionItem({ id: 'v1', name: '初始版本', version: '1.0', pages: [0] });
  button.click();
  suite.check(!button.disabled, '有对应页面的版本可以选择');
  suite.check(!state.pageCards[0].hidden && state.pageCards[1].hidden, '只显示第一页');
  suite.check(state.selectedVersionID === 'v1' && state.lastPage === 0, '记录所选版本并定位第一页');
  button.click();
  suite.check(state.pageCards.every(card => !card.hidden), '再次点击恢复全部页面');
  suite.check(state.selectedVersionID === '' && state.versionPageFilter === null, '清除版本筛选');
}

suite.group('当前版本仍可以选择');
{
  const state = baseState();
  state.pageCards[1].hidden = true;
  const button = state.buildVersionItem({ id: 'v2', current: true, version: '1.0', pages: [0, 1] });
  button.click();
  suite.check(!button.disabled && button.textContent.includes('当前'), '当前标记不禁用按钮');
  suite.check(state.pageCards.every(card => !card.hidden), '当前版本显示两页');
}

suite.group('无法显示的版本不改变当前页面');
for (const pages of [[], undefined, [-1, 2, 999, '0', 0.5]]) {
  const state = baseState();
  state.selectedVersionID = 'v1';
  state.pageCards[1].hidden = true;
  const button = state.buildVersionItem({ id: 'historical', pages });
  button.click();
  suite.check(button.disabled, '没有有效页码时禁用按钮', pages);
  suite.check(button.textContent.includes('暂无法显示'), '说明此版本暂无法显示');
  suite.check(state.selectedVersionID === 'v1' && !state.pageCards[0].hidden && state.pageCards[1].hidden, '保留当前版本与页面');
}

process.exitCode = suite.report() ? 1 : 0;
