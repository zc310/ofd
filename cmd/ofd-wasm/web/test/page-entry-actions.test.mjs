import { sliceFunction, load, createSuite } from './harness.mjs';

const suite = createSuite('页面进入动作');
const opened = [];
const played = [];
const dialog = {
  open: false,
  showModal() { this.open = true; },
  close() { this.open = false; },
  setAttribute() { this.open = true; },
  removeAttribute() { this.open = false; },
};
const state = {
  pageEntryActions: [
    { page: 26, event: 'PO', uri: 'https://www.ofdspec.org/' },
    { page: 26, event: 'PO', media_kind: 'sound', media_id: 4 },
    { page: 0, event: 'DO', uri: 'https://example.com/document-open' },
  ],
  window: { open: (...args) => opened.push(args) },
  pageURIDialog: dialog,
  pageURITarget: { textContent: '' },
  pageURIOpen: { focus() {} },
  pendingPageEntryURIs: [],
  activePageEntryURI: '',
  playMediaAction: action => played.push(action),
};
load(state, [
  'queuePageEntryURI', 'showNextPageEntryURI', 'finishPageEntryURI', 'runPageEntryActions',
].map(sliceFunction).join('\n'), {
  expose: {
    run: 'runPageEntryActions',
    closePrompt: 'finishPageEntryURI',
  },
});

state.run(25);
suite.check(opened.length === 0 && played.length === 0 && !dialog.open, '未进入动作所属页面时不触发');
state.run(26);
suite.check(dialog.open && state.pageURITarget.textContent === 'https://www.ofdspec.org/' && opened.length === 0, '进入第 27 页时先显示目标地址确认');
suite.check(played.length === 1 && played[0].media_id === 4, '页面进入媒体动作仍正常分发');
state.closePrompt(false);
suite.check(!dialog.open && opened.length === 0, '取消确认后不打开外部链接');
state.run(26);
state.closePrompt(true);
suite.check(opened.length === 1 && opened[0][0] === 'https://www.ofdspec.org/' && opened[0][1] === '_blank', '确认后在新标签页打开链接');
state.run(0);
suite.check(dialog.open && state.pageURITarget.textContent === 'https://example.com/document-open', '文档打开动作在首页显示确认');
state.closePrompt(true);
suite.check(opened.length === 2 && opened[1][0] === 'https://example.com/document-open', '确认后打开文档级链接');

process.exit(suite.report() ? 1 : 0);
