import assert from 'node:assert/strict';
import { load, sliceFunction } from './harness.mjs';

const calls = [];
const state = {
  engine: { nativeDesktop: true }, documentGeneration: 1, pageInfos: [{}, {}],
  hideLinkPreview() {}, setStatus: text => calls.push(text),
  goToDestination: (page, dest) => calls.push({ page, dest }),
  window: { open: () => { throw new Error('桌面不能直接导航 WebView'); } },
  openDesktopExternalLink: uri => calls.push(`confirm:${uri}`),
  playMediaAction: () => calls.push('media'),
  openAttachmentLink: link => calls.push(`attachment:${link.attachment_id}`),
};
load(state, sliceFunction('openPageLink'), { expose: { open: 'openPageLink' } });
const dest = { type: 'XYZ', top: 12 };
await state.open({ actions: [{ uri: 'https://example.test' }, { target_page: 1, dest }, { attachment_id: 'a' }, { media_kind: 'sound' }] });
assert.equal(calls[0], 'confirm:https://example.test');
assert.deepEqual(calls[1], { page: 1, dest });
assert.equal(calls[2], 'attachment:a');
assert.equal(calls[3], 'media');
console.log('桌面组合链接复用附件、确认外链及音视频动作测试通过');
