import { NativePageClient, mapPermissions } from './prototype/native-page.js';
await import('/wails/runtime.js');

class DesktopEngine {
  constructor() {
    this.nativeDesktop = true;
    this.nativeAndroid = globalThis.ofdReaderAndroid === true;
    this.ready = Promise.resolve();
    this.thumbnailQueue = [];
    this.thumbnailActive = false;
    this.activeThumbnail = undefined;
    this.selectionPending = false;
    this.lifecycleRevision = 0;
  }
  async selectDocument(selection = {}) {
    if (this.selectionPending) throw new Error('文件选择正在进行');
    this.selectionPending = true;
    const revision = this.lifecycleRevision;
    try {
      const method = selection.launch ? 'OpenLaunch' : selection.dropToken ? 'OpenDropped' : selection.recentID ? 'OpenRecent' : 'Open';
      const args = selection.dropToken ? [selection.dropToken] : selection.recentID ? [selection.recentID] : [];
      let result = await wails.Call.ByName(`main.Desktop.${method}`, ...args);
      let challenge;
      try {
        while (result?.challenge) {
          challenge = result.challenge;
          if (revision !== this.lifecycleRevision || !selection.unlock) return null;
          const credentials = await selection.unlock(result.name, { code: result.code, message: result.message });
          if (!credentials) return null;
          let options;
          try {
            if (revision !== this.lifecycleRevision) return null;
            options = { method: credentials.method, password: credentials.password,
              user_name: credentials.userName, private_key_password: credentials.privateKeyPassword };
            result = await wails.Call.ByName('main.Desktop.Unlock', challenge, options);
          }
          finally {
            delete credentials.password; delete credentials.privateKeyPassword;
            if (options) { delete options.password; delete options.private_key_password; }
          }
        }
      } finally {
        if (challenge) await wails.Call.ByName('main.Desktop.CancelUnlock', challenge);
      }
      if (!result) return null;
      const client = new NativePageClient(result.page_prefix);
      if (revision !== this.lifecycleRevision) {
        // 只能关闭本次打开的会话，不能影响随后打开的文档。
        await client.call('Close', [], () => undefined);
        return null;
      }
      this.cancelThumbnails();
      this.client = client;
      return { name: result.name, pageCount: result.pages.length,
        size: result.size, lastModified: result.last_modified,
        pages: result.pages,
        permissions: mapPermissions(result.permissions) };
    } finally {
      this.selectionPending = false;
    }
  }
  async close() {
    this.lifecycleRevision++;
    const client = this.client;
    this.client = undefined;
    this.cancelThumbnails();
    if (client) await client.call('Close', [], () => undefined);
  }
  terminate() {}
  loadVersion() {
    if (this.version) return Promise.resolve(this.version);
    if (!this.versionRequest) {
      this.versionRequest = wails.Call.ByName('main.Desktop.Version').then(version => {
        if (typeof version !== 'string' || !version) throw new Error('发行版本为空');
        this.version = version;
        return version;
      }).finally(() => { this.versionRequest = undefined; });
    }
    return this.versionRequest;
  }
  // 应用自带链接使用固定地址，不读取文档内容或网页传入的 URL。
  openProjectWebsite() { return wails.Browser.OpenURL('https://github.com/zc310/ofd'); }
  async recentDocuments() {
    const records = await wails.Call.ByName('main.Desktop.RecentDocuments');
    return records.map(({ last_opened, ...record }) => ({ ...record, lastOpened: last_opened }));
  }
  clearRecentDocuments() { return wails.Call.ByName('main.Desktop.ClearRecentDocuments'); }
  nextLaunch() { return wails.Call.ByName('main.Desktop.PendingLaunch'); }
  renderPage(...args) { return this.client.renderPage(...args); }
  pageInfo(...args) { return this.client.pageInfo(...args); }
  text(...args) { return this.client.text(...args); }
  search(...args) { return this.client.search(...args); }
  outline(...args) { return this.client.outline(...args); }
  customTags(...args) { return this.client.customTags(...args); }
  versions(...args) { return this.client.versions(...args); }
  selectVersion(scope, id) {
    const client = this.client, revision = this.lifecycleRevision;
    this.cancelThumbnails();
    const request = client.selectVersion(scope, id).then(async result => {
      const next = new NativePageClient(result.page_prefix);
      if (revision !== this.lifecycleRevision || this.client !== client) {
        await next.call('Close', [], () => undefined);
        const error = new Error('文档已关闭或已切换'); error.name = 'AbortError'; throw error;
      }
      this.client = next;
      return { pages: result.pages, permissions: mapPermissions(result.permissions) };
    });
    request.cancel = () => {};
    return request;
  }
  preferences(...args) { return this.client.preferences(...args); }
  info(...args) { return this.client.info(...args); }
  stats(...args) { return this.client.stats(...args); }
  attachments(...args) { return this.client.attachments(...args); }
  media(...args) { return this.client.media(...args); }
  mediaImagePreview(...args) { return this.client.mediaImagePreview(...args); }
  mediaThumbnail(...args) { return this.client.mediaThumbnail(...args); }
  attachmentTextPreview(...args) { return this.client.attachmentTextPreview(...args); }
  attachmentImagePreview(...args) { return this.client.attachmentImagePreview(...args); }
  attachmentPlayback(...args) { return this.client.attachmentPlayback(...args); }
  mediaPlayback(...args) { return this.client.mediaPlayback(...args); }
  openExternalLink(...args) { return this.client.openExternalLink(...args); }
  checkDocumentPermission(...args) { return this.client.checkDocumentPermission(...args); }
  saveAttachment(...args) { return this.client.saveAttachment(...args); }
  saveMedia(...args) { return this.client.saveMedia(...args); }
  saveSignatureValue(...args) { return this.client.saveSignatureValue(...args); }
  saveSignatureCertificate(...args) { return this.client.saveSignatureCertificate(...args); }
  exportPages(...args) { return this.client.exportPages(...args); }
  printPages(...args) { return this.client.printPages(...args); }
  annotations(...args) { return this.client.annotations(...args); }
  pageLinksForPage(...args) { return this.client.pageLinksForPage(...args); }
  layers(...args) { return this.client.layers(...args); }
  signatureSummaries(...args) { return this.client.signatureSummaries(...args); }
  signatureDetails(...args) { return this.client.signatureDetails(...args); }
  signatureSealPreview(...args) { return this.client.signatureSealPreview(...args); }
  signatureSealThumbnail(...args) { return this.client.signatureSealThumbnail(...args); }
  setHiddenLayers(...args) { return this.client.setHiddenLayers(...args); }
  annotationReport(...args) { return this.client.annotationReport(...args); }
  fontUsage(...args) { return this.client.fontUsage(...args); }
  fontUsageAll(...args) { return this.client.fontUsageAll(...args); }
  checkPermission(...args) { return this.client.checkPermission(...args); }
  queueThumbnail(index, options) {
    const client = this.client;
    let resolveRequest, rejectRequest;
    const promise = new Promise((resolve, reject) => { resolveRequest = resolve; rejectRequest = reject; });
    const entry = { client, index, options, resolve: resolveRequest, reject: rejectRequest, cancelled: false };
    entry.cancel = promise.cancel = () => {
      if (entry.cancelled || entry.settled) return;
      entry.cancelled = true;
      entry.request?.cancel();
      const error = new Error('请求已取消');
      error.name = 'AbortError';
      entry.reject(error);
    };
    this.thumbnailQueue.push(entry);
    this.pumpThumbnails();
    return promise;
  }
  cancelThumbnails() {
    this.activeThumbnail?.cancel();
    for (const entry of this.thumbnailQueue) entry.cancel();
    this.thumbnailQueue = [];
  }
  pumpThumbnails() {
    if (this.thumbnailActive) return;
    let entry;
    do { entry = this.thumbnailQueue.shift(); } while (entry?.cancelled);
    if (!entry) return;
    this.thumbnailActive = true;
    this.activeThumbnail = entry;
    // 缩略图与正文共用原生渲染池，只允许一个后台缩略图占用名额。
    Promise.resolve().then(() => {
      if (entry.cancelled) return undefined;
      entry.request = entry.client.renderPage(entry.index, entry.options);
      return entry.request;
    }).then(value => {
      if (!entry.cancelled) { entry.settled = true; entry.resolve(value); }
    }, error => {
      if (!entry.cancelled) { entry.settled = true; entry.reject(error); }
    }).finally(() => {
      if (this.activeThumbnail === entry) this.activeThumbnail = undefined;
      this.thumbnailActive = false;
      this.pumpThumbnails();
    });
  }
  renderPages(indices, options) {
    const requests = indices.map(index => this.queueThumbnail(index, options));
    const result = Promise.all(requests).catch(error => {
      requests.forEach(request => request.cancel());
      throw error;
    });
    result.cancel = () => requests.forEach(request => request.cancel());
    return result;
  }
}

// 尚未接入的功能返回明确错误，不能假称文档中没有这些内容。
for (const name of ['embeddedFonts',
  'pageEntryActionsForPage', 'signatures', 'memStats', 'gc',
  'attachmentData', 'mediaData',
  'signatureSeal', 'signatureCertificate', 'signatureValue', 'addFallbackFont', 'removeFallbackFont', 'renderStream']) {
  DesktopEngine.prototype[name] = () => {
    const request = Promise.reject(new Error('桌面版暂不支持此功能'));
    request.cancel = () => {};
    return request;
  };
}
window.ofdReaderEngineFactory = () => new DesktopEngine();
wails.Events.On('ofd-file-drop', event => {
  window.dispatchEvent(new CustomEvent('ofd-native-drop', { detail: event.data }));
});
const script = document.createElement('script');
script.src = 'viewer.js';
script.onload = () => { void configureDesktopControls(); };
document.body.append(script);

async function configureDesktopControls() {
  const menu = document.getElementById('document-menu');
  const text = globalThis.ofdDesktopText ?? (value => value);
  const languageLabel = document.createElement('label');
  languageLabel.textContent = '语言 / Language';
  languageLabel.style.padding = '8px';
  const language = document.createElement('select');
  language.style.marginInlineStart = '8px';
  language.setAttribute('aria-label', '语言 / Language');
  for (const [value, title] of [['zh', '简体中文'], ['en', 'English']]) {
    const option = document.createElement('option');
    option.value = value; option.textContent = title; language.append(option);
  }
  language.value = globalThis.ofdReaderLanguage === 'en' ? 'en' : 'zh';
  language.title = text('语言切换会刷新界面并重新打开当前文件；加密文件需要重新解锁。');
  language.addEventListener('change', async () => {
    language.disabled = true;
    try {
      await wails.Call.ByName('main.Desktop.SetLanguage', language.value);
      window.location.reload();
    } catch (error) {
      language.disabled = false;
      language.value = globalThis.ofdReaderLanguage;
      window.alert(error.message || String(error));
    }
  });
  languageLabel.append(language); menu.append(languageLabel);
  try {
    const state = await wails.Call.ByName('main.Desktop.DefaultReader');
    if (state.state === 'unavailable' && !state.settings) return;
    const button = document.createElement('button');
    const refresh = current => {
      button.textContent = text(state.settings ? '默认阅读器设置' : current === 'default' ? '已是默认阅读器' : '设置为默认阅读器');
      button.disabled = current === 'default';
      button.title = current === 'unknown' ? text('默认阅读器状态未知') : '';
    };
    refresh(state.state);
    button.addEventListener('click', async () => {
      button.disabled = true;
      try {
        if (state.settings) {
          await wails.Call.ByName('main.Desktop.OpenDefaultReaderSettings');
          button.disabled = false;
          return;
        }
        await wails.Call.ByName('main.Desktop.SetDefaultReader');
        refresh((await wails.Call.ByName('main.Desktop.DefaultReader')).state);
      } catch (error) { button.disabled = false; window.alert(error.message || String(error)); }
    });
    menu.append(button);
  } catch (error) { console.warn('默认阅读器查询失败', error); }
}
