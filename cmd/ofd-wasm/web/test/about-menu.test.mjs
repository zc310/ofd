import assert from 'node:assert/strict';
import { sliceFunction, load } from './harness.mjs';

// 关于属于应用操作，不能随文档关闭或权限拒绝一起禁用；其余菜单项仍按权限控制。
for (const nativeDesktop of [false, true]) {
  const state = {
    engine: { nativeDesktop }, pageInfos: [], current: 0,
    documentActionBusy: false, layerUpdates: new Set(),
    documentPermissionSummary: { print: false, export: false, copy: false },
    pagePermissionAllowed: () => false, pageLayoutIsDouble: () => false,
    activeSidebarTab: 'thumbnails', searchResults: [], searchRequest: null,
    localFontsSelect: null, zoom: 1, pagePillVisible: true,
    setRecentPanelOpen() {}, setViewPanelOpen() {}, setZoomMenuOpen() {},
    documentMenu: { hidden: true },
  };
  for (const name of ['pageNumber', 'pageCount', 'previous', 'next', 'printPage', 'exportDocument',
    'copyPageText', 'copyAllTextButton', 'searchInput', 'searchToggle', 'searchButton',
    'searchPrevious', 'searchNext', 'zoomOut', 'zoomIn', 'zoomMenuToggle', 'documentMenuToggle',
    'rotatePageButton', 'readingMode', 'viewToggle', 'infoToggle', 'pageLayoutSelect',
    'renderFormatSelect', 'zoomLabel', 'pagePill']) {
    state[name] = { disabled: false, setAttribute(attribute, value) { state[name][attribute] = value; } };
  }
  load(state, ['updateNavigation', 'setDocumentMenuOpen'].map(sliceFunction).join('\n'),
    { expose: { update: 'updateNavigation', menu: 'setDocumentMenuOpen' } });
  for (const pages of [[], [{}], []]) {
    state.pageInfos = pages;
    state.update();
    assert.equal(state.documentMenuToggle.disabled, false, '打开、拒绝权限、关闭文档均不禁用更多菜单');
    assert.ok(state.printPage.disabled && state.exportDocument.disabled
      && state.copyPageText.disabled && state.copyAllTextButton.disabled, '应用级菜单可用不等于授予文档操作权限');
    state.menu(true);
    assert.equal(state.documentMenu.hidden, false);
    assert.equal(state.documentMenuToggle['aria-expanded'], 'true');
    state.menu(false);
    assert.equal(state.documentMenu.hidden, true);
    assert.equal(state.documentMenuToggle['aria-expanded'], 'false');
  }
}
console.log('浏览器与桌面关于菜单的空文档、权限拒绝和关闭回归通过');
