import { sliceFunction, load, createSuite } from './harness.mjs';

const suite = createSuite('完整签章验证');
const element = tag => ({ tagName: tag, children: [], textContent: '', append(...nodes) { this.children.push(...nodes); }, addEventListener() {} });
const state = load({
  document: { createElement: element },
  signatureMethodLabel: value => value || '',
  attachmentBadge(text, className) { const badge = element('span'); badge.textContent = text; badge.className = className; return badge; },
}, sliceFunction('buildSignatureItem'), { expose: { buildSignatureItem: 'buildSignatureItem' } });
const text = node => [node.textContent, ...node.children.map(text)].join(' ');

for (const [status, label] of [['valid', '完整验证通过'], ['invalid', '完整验证失败'], ['incomplete', '完整验证未完成']]) {
  const node = state.buildSignatureItem({
    id: 'S1', has_verification: true, verified: true,
    validation: { status, checks: [{ code: 'signer.authorization', status, message: '用章授权检查结果' }, { code: 'timestamp.signing_time', status, message: '签章者证书时间检查结果' }] },
  });
  const content = text(node);
  suite.check(content.includes(label), `${status} 显示对应完整结论`);
  suite.check(content.includes('签名值通过'), '保留独立数学签名结果');
  suite.check(content.includes('用章授权检查结果'), '展示完整验证明细');
  suite.check(content.includes('时间戳时刻的签章者证书') && content.includes('签章者证书时间检查结果'), '展示时间戳时刻的签章者证书检查');
  if (status !== 'valid') suite.check(!content.includes('完整验证通过'), '数学签名通过不能覆盖完整验签失败或未完成');
}
process.exit(suite.report() ? 1 : 0);
