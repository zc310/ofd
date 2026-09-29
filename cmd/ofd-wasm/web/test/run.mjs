// 依次运行同目录下的 *.test.mjs，汇总结果；任一文件失败则整体以非零码退出。
//
// 用法：node cmd/ofd-wasm/web/test/run.mjs
// 或：  make test-wasm-web
import { readdirSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const files = readdirSync(here)
  .filter(name => name.endsWith('.test.mjs'))
  .sort();

if (!files.length) {
  console.error('没有找到 *.test.mjs');
  process.exit(1);
}

let failed = 0;
for (const file of files) {
  console.log(`\n=== ${file} ===`);
  // 给每个文件 60 秒上限：某个用例若因产品代码被改坏而永远不返回，runner 要能
  // 报出"超时"并继续跑其余文件，而不是整个挂在这里。
  const result = spawnSync(process.execPath, [join(here, file)],
    { stdio: 'inherit', timeout: 60_000 });
  if (result.error) {
    console.log(`\n${file} 未能完成：${result.error.message}`);
    failed++;
  } else if (result.status !== 0) {
    failed++;
  }
}

console.log(`\n${'='.repeat(48)}`);
console.log(failed === 0
  ? `全部通过：${files.length} 个测试文件`
  : `${failed}/${files.length} 个测试文件失败`);
process.exit(failed === 0 ? 0 : 1);
