import { sliceFunction, load, createSuite } from './harness.mjs';

const suite = createSuite('图元动作序列与同步声音');
const calls = [];
let finishSound;
const state = {
  documentGeneration: 1,
  hideLinkPreview() {},
  window: { open: uri => calls.push(uri) },
  pageInfos: [{}, {}],
  playMediaAction: action => {
    calls.push(`sound:${action.media_id}`);
    return new Promise(resolve => { finishSound = resolve; });
  },
  openAttachmentLink: async action => calls.push(`attachment:${action.attachment_id}`),
  goToDestination: page => calls.push(`page:${page}`),
};
load(state, sliceFunction('openPageLink'), { expose: { open: 'openPageLink' } });
const pending = state.open({actions:[
  {media_kind:'sound',media_id:7,synchronous:true},
  {attachment_id:'a'},
  {target_page:1},
  {uri:'https://example.test/last'},
]});
suite.check(calls.join(',') === 'sound:7', '同步声音尚未结束时不执行后续动作');
finishSound();
await pending;
suite.check(calls.join(',') === 'sound:7,attachment:a,page:1,https://example.test/last', '声音结束后按 XML 顺序执行附件、跳转和 URI');

calls.length = 0;
const old = state.open({actions:[{media_kind:'sound',media_id:7},{uri:'https://example.test/stale'}]});
state.documentGeneration++;
finishSound();
await old;
suite.check(calls.join(',') === 'sound:7', '切换文档后不再执行旧动作');

// 驱动实际媒体调度，不依赖音视频解码或系统设备。
const listeners = new Map();
const element = {
  addEventListener(name, callback) { listeners.set(name, callback); },
  removeEventListener(name) { listeners.delete(name); },
  async play() {},
  pause() {}, remove() {},
};
const entry = { element };
const media = {
  engine: { nativeDesktop: false },
  documentGeneration: 1,
  activeMedia: new Map([['0:7',entry]]),
  mediaActionKey: action => `${action.scope}:${action.media_id}`,
  ensureMediaElement: async () => entry,
  URL: {revokeObjectURL() {}},
  setStatus: message => { throw new Error(message); },
};
load(media, [sliceFunction('playMediaAction'),sliceFunction('stopActiveMedia')].join('\n'), {expose:{play:'playMediaAction',stop:'stopActiveMedia'}});
let ended = false;
const sync = media.play({scope:0,media_id:7,media_kind:'sound',synchronous:true}).then(() => { ended = true; });
await Promise.resolve();
await Promise.resolve();
suite.check(!ended && listeners.has('ended'), '实际调度注册完成监听并等待 ended');
listeners.get('ended')();
await sync;
suite.check(ended && !listeners.has('ended') && !entry.completionWaiters.size, '结束后释放监听及等待者');
ended = false;
const stopped = media.play({scope:0,media_id:7,media_kind:'sound',synchronous:true}).then(() => { ended = true; });
await Promise.resolve();
await Promise.resolve();
media.stop('0:7');
await stopped;
suite.check(ended && !media.activeMedia.size, '关闭文档或停止媒体也释放同步等待');
await media.play({scope:0,media_id:7,media_kind:'sound',synchronous:true,repeat:true});
suite.check(!listeners.has('ended'), '循环播放按表57忽略 Synchronous，不无限等待');

process.exit(suite.report() ? 1 : 0);
