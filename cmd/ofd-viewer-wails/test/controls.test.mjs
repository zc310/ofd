import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';

const messages = JSON.parse(readFileSync(new URL('../locales/en.json', import.meta.url)));
const source = readFileSync(new URL('../desktop.js', import.meta.url), 'utf8')
  .replace("import { NativePageClient, mapPermissions } from './prototype/native-page.js';", '')
  .replace("await import('/wails/runtime.js');", '');
function node(tag) {
  return { tag, children: [], style: {}, events: {}, append(...items) { this.children.push(...items); },
    setAttribute() {}, addEventListener(name, fn) { this.events[name] = fn; } };
}
async function setup(state = 'other') {
  const menu = node('menu'), calls = [], errors = [];
  let reloads = 0, failSetting = false;
  const context = vm.createContext({
    console, ofdReaderLanguage: 'en', ofdDesktopText: value => messages[value] ?? value,
    wails: { Events: { On() {} }, Call: { async ByName(method, ...args) {
      calls.push([method, ...args]);
      if (method.endsWith('.DefaultReader')) return { state };
      if (failSetting) throw new Error('setting failed');
      if (method.endsWith('.SetDefaultReader')) state = 'default';
    } } },
    window: { location: { reload() { reloads++; } }, alert: text => errors.push(text) },
    document: { createElement: node, getElementById: () => menu, body: { append() {} } },
  });
  vm.runInContext(source, context);
  await vm.runInContext('configureDesktopControls()', context);
  return { menu, calls, errors, get reloads() { return reloads; }, fail() { failSetting = true; } };
}
const normal = await setup();
const language = normal.menu.children[0].children[0];
assert.deepEqual(language.children.map(option => option.value), ['zh', 'en']);
assert.equal(language.value, 'en');
language.value = 'zh';
await language.events.change();
assert.deepEqual(normal.calls.at(-1), ['main.Desktop.SetLanguage', 'zh']);
assert.equal(normal.reloads, 1, '设置成功后才刷新界面');
const button = normal.menu.children[1];
assert.equal(button.textContent, 'Set as default reader');
await button.events.click();
assert.equal(button.textContent, 'Already the default reader');
assert.equal(button.disabled, true);

const failed = await setup();
failed.fail();
const failedLanguage = failed.menu.children[0].children[0];
failedLanguage.value = 'zh'; await failedLanguage.events.change();
assert.equal(failedLanguage.value, 'en');
assert.equal(failedLanguage.disabled, false);
assert.equal(failed.reloads, 0, '保存失败不能刷新或假装更改成功');
await failed.menu.children[1].events.click();
assert.equal(failed.menu.children[1].disabled, false);
assert.equal(failed.menu.children[1].textContent, 'Set as default reader');
assert.equal(failed.errors.length, 2);
const unavailable = await setup('unavailable');
assert.equal(unavailable.menu.children.length, 1, '不可用时仅保留语言入口');
console.log('桌面中英文控制、保存失败恢复与默认阅读器确认测试通过');
