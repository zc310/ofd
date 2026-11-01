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
	pageEntryActionDepth: 0,
	documentGeneration: 1,
	documentActionGeneration: -1,
	completedDocumentActions: new Set(),
	pageEntryNavigationCount: 0,
	pageEntryNavigationTarget: -1,
	current: 25,
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
  hideLinkPreview() {},
  pageInfos: Array.from({ length: 30 }, () => ({})),
  goToDestination: (page, dest) => { state.current = page; state.lastDestination = { page, dest }; },
  openAttachmentLink: async action => { state.lastAttachment = action; },
};
load(state, [
  'queuePageEntryURI', 'showNextPageEntryURI', 'finishPageEntryURI', 'runPageEntryActions', 'openPageLink',
].map(sliceFunction).join('\n'), {
  expose: {
    run: 'runPageEntryActions',
    closePrompt: 'finishPageEntryURI',
  },
});

await state.run(25);
suite.check(opened.length === 0 && played.length === 0 && !dialog.open, '未进入动作所属页面时不触发');
state.current = 26;
await state.run(26);
suite.check(dialog.open && state.pageURITarget.textContent === 'https://www.ofdspec.org/' && opened.length === 0, '进入第 27 页时先显示目标地址确认');
suite.check(played.length === 1 && played[0].media_id === 4, '页面进入媒体动作仍正常分发');
state.closePrompt(false);
suite.check(!dialog.open && opened.length === 0, '取消确认后不打开外部链接');
await state.run(26);
state.closePrompt(true);
suite.check(opened.length === 1 && opened[0][0] === 'https://www.ofdspec.org/' && opened[0][1] === '_blank', '确认后在新标签页打开链接');
state.current = 0;
await state.run(0);
suite.check(dialog.open && state.pageURITarget.textContent === 'https://example.com/document-open', '文档打开动作在首页显示确认');
state.closePrompt(true);
suite.check(opened.length === 2 && opened[1][0] === 'https://example.com/document-open', '确认后打开文档级链接');
await state.run(0);
suite.check(!dialog.open, '再次回到文档首页不重复执行 DO');

state.pageEntryActions = [{page:3,event:'DO',uri:'https://example.com/second-body'}];
state.current = 3;
await state.run(3);
suite.check(dialog.open && state.pageURITarget.textContent.endsWith('/second-body'), '第二文档体的 DO 按所属首页执行，不写死全局首页');
state.closePrompt(false);
state.pageEntryActions = [{page:3,event:'PO',target_page:8,dest:{type:'Fit'}}];
await state.run(3);
suite.check(state.current === 8 && state.lastDestination.dest.type === 'Fit', '页面打开 Goto 保留目标与定位');
suite.check(state.pageEntryActionDepth === 0, '完成后释放动作递归深度');

state.pageEntryActions = [{page:8,event:'PO',uri:'https://example.com/no-loop'}];
state.pageEntryActionDepth = 32;
await state.run(8);
suite.check(!dialog.open, '自动跳转达到预算时停止，防止 PO 环');
state.pageEntryActionDepth = 0;
state.pageEntryNavigationCount = 32;
state.pageEntryNavigationTarget = 8;
state.pageEntryActions = [{page:8,event:'PO',target_page:3}];
await state.run(8);
suite.check(state.current === 8, '延迟加载下一页后仍保留自动跳转预算');
state.current = 10;
state.pageEntryActions = [{page:10,event:'PO',target_page:11}];
await state.run(10);
suite.check(state.current === 11, '主动进入另一页面可开始新的自动跳转链');

process.exit(suite.report() ? 1 : 0);
