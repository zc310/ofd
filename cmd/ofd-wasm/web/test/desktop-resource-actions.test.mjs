import assert from 'node:assert/strict';
import { load, sliceFunction } from './harness.mjs';

function node(tag = '') {
  const listeners = new Map();
  const element = { tag, children: [], style: { removeProperty(name) { delete this[name]; } }, isConnected: true,
    append(...items) { for (const item of items) { if (item.parent) item.parent.children = item.parent.children.filter(child => child !== item); this.children.push(item); item.parent = this; } },
    replaceWith(item) { const parent = this.parent; parent.children[parent.children.indexOf(this)] = item; item.parent = parent; this.parent = undefined; },
    setAttribute(name, value) { this[name] = value; }, removeAttribute(name) { delete this[name]; },
    addEventListener(type, callback) { if (!listeners.has(type)) listeners.set(type, new Set()); listeners.get(type).add(callback); },
    removeEventListener(type, callback) { listeners.get(type)?.delete(callback); },
    emit(type, extra = {}) { const event = { preventDefault() {}, stopPropagation() {}, ...extra }; for (const callback of [...(listeners.get(type) || [])]) callback(event); },
    show() { this.open = true; }, close() { this.open = false; },
    remove() { this.removed = true; if (this.parent) this.parent.children = this.parent.children.filter(child => child !== this); this.parent = undefined; },
    async play() { this.plays = (this.plays || 0) + 1; this.emit('play'); }, pause() { this.pauses = (this.pauses || 0) + 1; }, load() { this.loads = (this.loads || 0) + 1; },
  };
  return element;
}
const pending = [], urls = [], revoked = [], statuses = [], permissions = [], attachments = [], openedImages = [], saved = [], external = [];
const request = (kind, args) => {
  let resolve, reject;
  const promise = new Promise((done, fail) => { resolve = done; reject = fail; });
  const item = { kind, args, resolve, reject, cancelled: 0 };
  promise.cancel = () => { item.cancelled++; };
  pending.push(item); return promise;
};
const layer = node('layer');
const state = {
  documentGeneration: 1, activeMedia: new Map(), desktopMediaPlaybackRequests: new Map(),
  desktopExternalLinkRequest: undefined, desktopAttachmentLinkRequest: undefined, desktopAttachmentPreview: undefined,
  pageInfos: [{ width: 210, height: 297 }], pageCards: [{ querySelector: () => layer }],
  document: { createElement: node, body: node('body') }, Blob,
  URL: { createObjectURL: () => { const url = `blob:${urls.length}`; urls.push(url); return url; }, revokeObjectURL: url => revoked.push(url) },
  setStatus: value => statuses.push(value), mediaKind: item => String(item.type).toLowerCase(),
  engine: { nativeDesktop: true,
    mediaPlayback: (...args) => request('media', args), attachmentPlayback: (...args) => request('attachment', args),
    attachmentImagePreview: (...args) => { attachments.push(args); return Promise.resolve({ mime: 'image/png', data: new Uint8Array([1]) }); },
    checkDocumentPermission: async (...args) => { permissions.push(args); if (state.denied) throw new Error('权限失效'); },
    attachments: () => request('list', []), openExternalLink: (...args) => { external.push(args); return request('external', args); },
  },
  openDesktopAttachmentPreview: async item => { attachments.push(item.name); },
  openDesktopImagePreview: async (button, label, start) => { openedImages.push([button, label, await start()]); },
  saveDesktopResource: async item => { saved.push(item.name); },
  closeDesktopMediaPreview() {},
};
load(state, ['mediaActionKey', 'stopActiveMedia', 'checkDesktopMediaPermission', 'showDesktopMediaPopup', 'ensureDesktopMediaElement', 'openDesktopPlaybackPreview',
  'placeMediaElement', 'mediaPlacementTarget', 'rehomeEmbeddedMedia', 'ensureMediaElement', 'playMediaAction',
  'desktopAttachmentKind', 'closeDesktopAttachmentPreview', 'openDesktopAttachment', 'openDesktopAttachmentLink', 'openDesktopExternalLink'].map(sliceFunction).join('\n'),
{ expose: { preview: 'openDesktopPlaybackPreview', ensure: 'ensureDesktopMediaElement', stop: 'stopActiveMedia', play: 'playMediaAction',
  rehome: 'rehomeEmbeddedMedia', attachmentLink: 'openDesktopAttachmentLink', attachment: 'openDesktopAttachment', external: 'openDesktopExternalLink' } });
const flush = () => new Promise(resolve => setImmediate(resolve));
const wav = { mime: 'audio/wave', data: new Uint8Array([1, 2, 3]) };
const audio = { scope: 2, id: 7, type: 'Audio', name: '测试.wav', exists: true };

// 读取中的播放器可关闭；不依赖底层是否真的中断，不发布迟到结果。
const loading = state.preview(audio);
const loadingEntry = state.desktopMediaPlaybackRequests.get('2:7');
assert.equal(loadingEntry.popup.open, true);
loadingEntry.popup.children[0].emit('click');
assert.equal(pending[0].cancelled, 1); assert.equal(loadingEntry.popup.removed, true);
pending[0].resolve(wav); await loading;
assert.equal(urls.length, 0); assert.equal(statuses.length, 0);

const first = state.preview(audio), duplicate = state.preview(audio);
assert.equal(pending.length, 2, '同一媒体并发读取复用请求');
pending[1].resolve(wav); await first; await duplicate;
let entry = state.activeMedia.get('2:7');
assert.equal(urls.length, 1); assert.equal(entry.element.plays || 0, 0, '资源预览不自动播放');
assert.equal(entry.element.controls, true); assert.equal(entry.element.preload, 'metadata');
assert.equal(entry.source, undefined); assert.equal(entry.promise, undefined, '不保留已完成读取的字节 Promise');
await state.preview(audio); assert.deepEqual(permissions.at(-1), [2, 'access'], '缓存重用也检查所属文档体访问权限');
entry.element.emit('play'); await flush(); assert.deepEqual(permissions.at(-1), [2, 'access']);
state.denied = true; entry.element.emit('play'); await flush();
assert.equal(state.activeMedia.size, 0); assert.equal(entry.element.src, undefined); assert.equal(entry.element.loads, 1);
assert.equal(entry.popup.removed, true); assert.deepEqual(revoked, ['blob:0']); assert.match(statuses.at(-1), /权限失效/);
state.denied = false;

// 沿用标准动作调度：缺省音量 100，同步声音等待结束；关闭也解除等待。
let completed = false;
const sync = state.play({ scope: 2, media_id: 7, media_kind: 'sound', volume: null, synchronous: true }).then(() => { completed = true; });
pending[2].resolve(wav); await flush(); entry = state.activeMedia.get('2:7');
assert.equal(entry.element.volume, 1); assert.equal(entry.element.plays, 1); assert.equal(completed, false);
entry.element.emit('ended'); await sync; assert.equal(completed, true);
const again = state.play({ scope: 2, media_id: 7, media_kind: 'sound', synchronous: true });
await flush(); state.stop(); await again; assert.equal(state.activeMedia.size, 0);

const movie = state.play({ scope: 2, template_id: 3, media_id: 9, media_kind: 'movie', page: 0,
  appearance_boundary: { x: 21, y: 29.7, width: 105, height: 148.5 } });
assert.deepEqual(pending[3].args, [2, 9, 3]);
pending[3].resolve({ ...wav, mime: 'video/mp4' }); await movie;
entry = state.activeMedia.get('2:template:3:9');
assert.equal(entry.placement, 'embedded'); assert.equal(entry.element.parent, layer);
assert.equal(entry.element.style.width, '50%'); assert.equal(entry.popup, undefined);
state.rehome(0); assert.equal(entry.placement, 'popup'); assert.equal(entry.popup.open, true, '页面回收后保留可关闭的播放器');
entry.element.emit('error'); assert.equal(state.activeMedia.size, 0); assert.match(statuses.at(-1), /无法解码/);

const old = state.preview(audio), recent = state.preview({ ...audio, id: 8 });
assert.equal(pending[4].cancelled, 1, '新媒体撤销旧读取');
pending[5].resolve(wav); await recent;
const before = statuses.length;
pending[4].reject(new Error('旧媒体读取失败')); await old;
assert.equal(statuses.length, before, '同文档迟到失败不覆盖新播放器状态');
assert.equal(state.activeMedia.size, 1); assert.ok(state.activeMedia.has('2:8'));
state.stop();
const stale = state.preview(audio); state.documentGeneration++; state.stop();
pending[6].resolve(wav); await stale; assert.equal(state.activeMedia.size, 0);
assert.deepEqual(revoked, urls, '关闭、解码失败和文档切换释放全部已发布 URL');

await state.attachment({ scope: 2, id: 'pic', name: '照片.webp', exists: true });
assert.equal(openedImages[0][0], null, '正文链接不需要伪造已连接的按钮');
assert.deepEqual(attachments.at(-1), [2, 'pic']);
await state.attachment({ scope: 2, id: 'text', name: '说明.html', exists: true });
assert.equal(attachments.at(-1), '说明.html', 'HTML 附件只进纯文本预览');
await state.attachment({ scope: 2, id: 'pdf', name: '附件.pdf', exists: true });
assert.deepEqual(saved, ['附件.pdf'], '未接入的格式仅通过原生另存为，不执行附件');
const attachmentPlayback = state.attachment({ scope: 2, id: 'song', name: '音乐.ogg', exists: true });
assert.equal(pending[7].kind, 'attachment'); pending[7].resolve(wav); await attachmentPlayback;
entry = state.activeMedia.get('2:attachment:song'); entry.element.emit('play'); await flush();
assert.deepEqual(permissions.at(-1), [2, 'export'], '附件播放使用导出权限'); state.stop();

const attachmentLink = state.attachmentLink({ scope: 2, attachment_id: 'text', attachment_name: '伪造.exe' });
pending[8].resolve([{ scope: 2, id: 'text', name: '真实.txt', exists: true }]); await attachmentLink;
assert.equal(attachments.at(-1), '真实.txt');
const missing = state.attachmentLink({ scope: 2, attachment_id: 'missing', attachment_name: '伪造.txt' });
pending[9].resolve([]); await missing; assert.match(statuses.at(-1), /附件不存在/);
const older = state.attachmentLink({ scope: 2, attachment_id: 'old' });
const newer = state.attachmentLink({ scope: 2, attachment_id: 'new' });
pending[11].resolve([{ scope: 2, id: 'new', name: '最新.txt', exists: true }]); await newer;
const statusCount = statuses.length;
pending[10].reject(new Error('旧清单失败')); await older;
assert.equal(pending[10].cancelled, 1); assert.equal(statuses.length, statusCount); assert.equal(attachments.at(-1), '最新.txt');

const externalLink = state.external('https://example.test', 2);
await state.external('https://duplicate.test', 2);
assert.equal(external.length, 1, '同时只显示一个原生外链确认');
assert.deepEqual(external[0], ['https://example.test', 2]);
state.documentGeneration++; pending[12].resolve(true); await externalLink;
assert.notEqual(statuses.at(-1), '已在系统浏览器打开链接', '旧文档确认不报告新文档操作成功');
const cancelled = state.external('https://example.test', 2); pending[13].resolve(false); await cancelled;
assert.match(statuses.at(-1), /已取消/); assert.equal(state.desktopExternalLinkRequest, undefined);
console.log('正文附件链接、原生外链确认、音视频动作与预览、模板资源、权限复核及关闭取消测试通过');
