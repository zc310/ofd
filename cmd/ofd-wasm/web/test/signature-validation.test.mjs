import { sliceFunction, load, createSuite } from './harness.mjs';

const suite = createSuite('完整签章验证');
const element = tag => ({ tagName: tag, children: [], textContent: '', append(...nodes) { this.children.push(...nodes); }, addEventListener() {} });
const state = load({
  document: { createElement: element },
  engine: { nativeDesktop: false },
  signatureMethodLabel: value => value || '',
  attachmentBadge(text, className) { const badge = element('span'); badge.textContent = text; badge.className = className; return badge; },
}, ['buildCertificateDetail', 'buildSignatureItem'].map(sliceFunction).join('\n'), { expose: { buildSignatureItem: 'buildSignatureItem' } });
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
state.engine.nativeDesktop = true;
const desktop = state.buildSignatureItem({ id: 'S1', has_digest: true, digest_valid: false,
  has_verification: true, verified: true, trust_checked: false,
  validation: { status: 'incomplete', checks: [{ code: 'certificates.trust', status: 'incomplete' }] },
  references: [{ file_ref: 'Doc/Page.xml', exists: true, match: false }],
  certificates: [{ slot: '外层', slot_key: 'outer', subject: '测试主体', signature_valid: true, certificate_valid: true, trust_checked: false }],
}, { skipStamps: true });
const content = text(desktop);
suite.check(content.includes('摘要不一致') && content.includes('签名值通过') && content.includes('完整验证未完成'), '桌面分别展示摘要、签名值和完整验证');
suite.check(content.includes('未校验证书链') && content.includes('测试主体') && content.includes('Doc/Page.xml'), '桌面展示证书及签名范围');
suite.check(content.includes('未检查吊销状态'), '没有吊销检查时明确标注');
suite.check(content.includes('导出 DER') && content.includes('导出 PEM') && content.includes('导出签名值'), '桌面提供证书与签名值原生导出入口');
const unavailable = text(state.buildSignatureItem({ id: 'empty' }, { skipStamps: true }));
suite.check(unavailable.includes('未获得摘要检查结果') && unavailable.includes('未获得签名值检查结果'), '缺失检查结果不能被误认为通过');
process.exit(suite.report() ? 1 : 0);
