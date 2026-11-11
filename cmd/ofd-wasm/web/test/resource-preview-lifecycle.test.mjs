import assert from 'node:assert/strict';
import { load, sliceFunction } from './harness.mjs';

// 使用真实预览和清理函数；模拟不可立即中断的读取及异步媒体事件。
function node(tag = '') {
  const listeners = new Map(), captures = new Set();
  return { tag, children: [], style: {}, isConnected: true, clientWidth: 800, clientHeight: 600,
    classList: { add() {}, remove() {}, toggle() {} },
    append(...items) { for (const item of items) { item.remove(); item.parent = this; this.children.push(item); } },
    replaceWith(item) { const parent = this.parent; this.remove(); parent.append(item); },
    remove() { if (this.parent) this.parent.children = this.parent.children.filter(item => item !== this); this.parent = undefined; },
    setAttribute(name, value) { this[name] = value; }, removeAttribute(name) { delete this[name]; },
    addEventListener(type, callback) { if (!listeners.has(type)) listeners.set(type, new Set()); listeners.get(type).add(callback); },
    removeEventListener(type, callback) { listeners.get(type)?.delete(callback); },
    emit(type) { for (const callback of [...(listeners.get(type) || [])]) callback({ preventDefault() {}, stopPropagation() {} }); },
    show() { this.open = true; }, showModal() { this.open = true; }, close() { this.open = false; },
    pause() { this.paused = true; }, load() { this.loads = (this.loads || 0) + 1; },
    play() { this.paused = false; return Promise.resolve(); },
    hasPointerCapture: id => captures.has(id), releasePointerCapture: id => captures.delete(id),
  };
}
function deferred() {
  let resolve, reject, cancellations = 0;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  promise.cancel = () => { cancellations++; };
  return { promise, resolve, reject, get cancellations() { return cancellations; } };
}
function fixture() {
  const reads = [], checks = [], statuses = [], urls = new Set(), observers = new Set();
  let nextURL = 0;
  const read = () => { const item = deferred(); reads.push(item); return item.promise; };
  const state = {
    documentGeneration: 1, activeMedia: new Map(), desktopMediaPlaybackRequests: new Map(),
    desktopMediaPreviewRevision: 0, desktopMediaDialog: undefined, desktopAttachmentPreview: undefined,
    desktopAttachmentLinkRequest: undefined, mediaPreviewRequests: new Set(), pageInfos: [], pageCards: [],
    document: { createElement: node, body: node('body') }, Blob,
    ResizeObserver: class { observe() { observers.add(this); } disconnect() { observers.delete(this); } },
    URL: { createObjectURL() { const url = `blob:${nextURL++}`; urls.add(url); return url; },
      revokeObjectURL(url) { assert.ok(urls.delete(url), `URL 必须且仅能释放一次：${url}`); } },
    setStatus: text => statuses.push(text), mediaKind: () => 'audio',
    engine: { nativeDesktop: true, mediaPlayback: read, attachmentTextPreview: read,
      checkDocumentPermission() { const item = deferred(); checks.push(item); return item.promise; } },
  };
  load(state, ['mediaActionKey', 'stopActiveMedia', 'checkDesktopMediaPermission', 'showDesktopMediaPopup', 'ensureDesktopMediaElement',
    'openDesktopPlaybackPreview', 'placeMediaElement', 'mediaPlacementTarget', 'ensureMediaElement', 'playMediaAction',
    'closeDesktopAttachmentPreview', 'openDesktopAttachmentPreview', 'openDesktopImagePreview',
    'closeDesktopMediaPreview', 'createImagePreviewControls'].map(sliceFunction).join('\n'),
  { expose: { media: 'openDesktopPlaybackPreview', play: 'playMediaAction', stop: 'stopActiveMedia',
    image: 'openDesktopImagePreview', text: 'openDesktopAttachmentPreview', closeImage: 'closeDesktopMediaPreview', closeText: 'closeDesktopAttachmentPreview' } });
  const clean = () => { state.stop(); state.closeImage(); state.closeText(); };
  const empty = () => {
    assert.equal(state.activeMedia.size, 0); assert.equal(state.desktopMediaPlaybackRequests.size, 0);
    assert.equal(state.mediaPreviewRequests.size, 0); assert.equal(state.document.body.children.length, 0);
    assert.equal(urls.size, 0); assert.equal(observers.size, 0);
  };
  return { state, reads, checks, statuses, urls, observers, clean, empty };
}
const audio = { scope: 0, id: 7, type: 'Audio', name: '声音.wav' };
const sound = { scope: 0, media_id: 7, media_kind: 'sound' };
const image = { mime: 'image/png', data: new Uint8Array([1]) };
const wav = { mime: 'audio/wave', data: new Uint8Array([1]) };
const flush = () => new Promise(resolve => setImmediate(resolve));

{
  const f = fixture(), s = f.state;
  const media = s.media(audio); f.reads.at(-1).resolve(wav); await media;
  const old = s.activeMedia.get('0:7');
  const pendingImage = deferred(), showingImage = s.image(null, '图片', () => pendingImage.promise);
  assert.equal(old.element.paused, true, '图片预览替换声音时暂停旧播放器');
  assert.equal(old.element.src, undefined); assert.equal(f.urls.size, 0);
  const text = s.text({ id: 'a', name: '说明.txt' });
  assert.equal(pendingImage.cancellations, 1, '文本预览撤销未完成的图片读取');
  pendingImage.resolve(image); await showingImage;
  assert.equal(s.desktopMediaDialog, undefined, '迟到图片不能覆盖文本');
  const readingText = f.reads.at(-1);
  const next = s.media(audio);
  assert.equal(readingText.cancellations, 1, '音视频预览撤销未完成的文本读取');
  readingText.resolve('旧文本'); await text;
  assert.equal(s.desktopAttachmentPreview, undefined);
  f.reads.at(-1).resolve(wav); await next;
  assert.equal(s.document.body.children.length, 1, '不同类型预览不叠加');
  f.clean(); f.empty();
}
{
  const f = fixture(), s = f.state, button = node('button'), pending = deferred();
  const preview = s.image(button, '图片', () => pending.promise);
  button.isConnected = false; pending.resolve(image); await preview;
  assert.equal(f.urls.size, 1, '侧栏重建后图片仍能显示，不永久停在读取占位');
  assert.equal(f.observers.size, 1);
  const dialog = s.desktopMediaDialog;
  dialog.close(); dialog.emit('close');
  f.empty();
  const text = s.text({ id: 'a', name: '说明.txt' }), textDialog = s.desktopAttachmentPreview.dialog;
  textDialog.close(); textDialog.emit('close');
  f.reads.at(-1).resolve('迟到文本'); await text;
  f.empty();
}
{
  const f = fixture(), s = f.state;
  const initial = s.media(audio); f.reads.at(-1).resolve(wav); await initial;
  const reused = s.media(audio), check = f.checks.at(-1);
  const replacement = s.media({ ...audio, id: 8 });
  f.reads.at(-1).resolve(wav); await replacement;
  const before = f.statuses.length;
  check.reject(new Error('旧媒体权限过期')); await reused;
  assert.equal(f.statuses.length, before, '旧媒体权限检查迟到失败不覆盖新媒体状态');
  assert.equal(check.cancellations, 1, '替换时取消待处理权限复核');
  assert.ok(s.activeMedia.has('0:8'));
  f.clean(); f.empty();
}
{
  const f = fixture(), s = f.state;
  const initial = s.media(audio); f.reads.at(-1).resolve(wav); await initial;
  const entry = s.activeMedia.get('0:7'), playback = deferred();
  entry.element.play = () => playback.promise;
  const playing = s.play(sound); f.checks.at(-1).resolve(true); await flush();
  const replacement = s.media({ ...audio, id: 8 }); f.reads.at(-1).resolve(wav); await replacement;
  const before = f.statuses.length;
  playback.reject(new Error('旧播放器无法播放')); await playing;
  assert.equal(f.statuses.length, before, '旧 play() 迟到失败不覆盖新状态');
  assert.ok(s.activeMedia.has('0:8'));
  f.clean(); f.empty();
}
{
  const f = fixture(), s = f.state;
  const initial = s.media(audio); f.reads.at(-1).resolve(wav); await initial;
  const entry = s.activeMedia.get('0:7');
  entry.element.emit('play'); entry.element.emit('play');
  const reuse = s.media(audio);
  assert.equal(f.checks.length, 1, '重复播放事件和复用只占用一个权限请求');
  f.checks[0].reject(new Error('访问权限过期')); await reuse; await flush();
  assert.equal(s.activeMedia.size, 0); assert.equal(f.statuses.length, 1);
  const retry = s.media(audio); f.reads.at(-1).resolve(wav); await retry;
  const next = s.activeMedia.get('0:7');
  next.element.play = () => Promise.reject(new Error('需要用户手势'));
  const playing = s.play({ ...sound, synchronous: true });
  f.checks.at(-1).resolve(true); await playing;
  assert.match(f.statuses.at(-1), /需要用户手势/);
  assert.equal(next.completionWaiters.size, 0, '播放被拒绝也必须释放同步完成监听');
  assert.equal(s.activeMedia.get('0:7'), next, '播放被拒绝时保留可重试控件');
  f.clean(); f.empty();
}
{
  const f = fixture(), s = f.state;
  for (let index = 0; index < 100; index++) {
    const preview = s.media(audio), read = f.reads.at(-1);
    if (index % 3 === 0) { s.documentGeneration++; s.stop(); read.reject(new Error('旧文档损坏')); }
    else read.resolve(wav);
    await preview;
    const entry = s.activeMedia.get('0:7');
    if (entry) {
      if (index % 3 === 1) entry.element.emit('error');
      else { entry.element.emit('play'); s.stop(); f.checks.at(-1).reject(new Error('关闭后权限失败')); }
    }
    f.clean(); await flush(); f.empty();
    const pending = deferred(), show = s.image(null, '图片', () => pending.promise);
    if (index % 2) { s.closeImage(); pending.reject(new Error('已关闭的图片损坏')); }
    else pending.resolve(image);
    await show;
    const text = s.text({ id: 'a', name: '说明.txt' });
    f.reads.at(-1).resolve('文本'); await text;
    f.clean(); f.empty();
  }
  assert.equal(f.statuses.length, 33, '只有仍然有效的解码失败可以报告错误');
}
console.log('跨类型预览替换、迟到权限与播放失败、侧栏重建、直接关闭及 100 轮生命周期回收测试通过');
