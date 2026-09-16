# ofd-archive

`ofd-archive` 是面向 GB/T 42133-2022 相关档案处理场景的 OFD 技术预检和归档准备工具。它组合现有 `ofd-validator` 与 `ofd-analyzer`，并生成档案状态、技术清单、附件记录和 SHA-256 固定性信息。

本工具不修改输入 OFD，不自动删除动作、视频、注释或附件，不嵌入字体，不重签名，不自动解密，也不替代档案管理系统、密码学验证或人工合规判断。原始签名文件原样保存在准备目录中。

## 用法

无子命令时默认执行 `check`：

```bash
ofd-archive check --format text document.ofd
ofd-archive check --format markdown --output check.md document.ofd
ofd-archive check --format json --pretty document.ofd > check.json
ofd-archive manifest --format json document.ofd > manifest.json
ofd-archive matrix --format markdown document.ofd > matrix.md
ofd-archive matrix --format xlsx --output matrix.xlsx document.ofd
# 输出路径为 .xlsx 时，也可以省略 --format xlsx
ofd-archive matrix --output matrix.xlsx document.ofd
ofd-archive prepare document.ofd --metadata archive.json --output archive-directory/
ofd-archive verify archive-directory/
```

`archive.json` 使用 JSON，也可以使用 YAML；可选 profile 使用 `required_fields` 声明档案字段，例如：

```json
{
  "archive_code": "A-2026-001",
  "fonds_code": "F-001",
  "retention_period": "永久",
  "security_classification": "内部",
  "open_status": "开放"
}
```

附件提取默认限制为 64 MiB，可通过 `--max-attachment-size` 调整；超过限制时保留原始 OFD 中的附件记录，但不复制附件副本。

使用 profile 时，将 `--profile profile.json` 加到 `prepare` 或 `manifest` 命令中；profile 文件会在 `prepare` 结果的 `metadata/` 中原样保存并纳入固定性清单。

准备目录结构：

```text
archive-directory/
├── original/document.ofd
├── metadata/archive.json
├── metadata/manifest.json
├── reports/check.json
├── reports/analysis.json
├── fixity/manifest.sha256
└── attachments/
```

状态为 `passed`、`warning` 或 `failed`。退出码 `0` 表示通过或有普通警告，`1` 表示失败或指定 `--fail-on-warning`，`2` 表示参数错误，`3` 表示输出或准备目录失败。

默认情况下，只有错误会使命令返回退出码 `1`；如果希望把普通警告也作为失败处理，可使用：

```bash
ofd-archive check --fail-on-warning document.ofd
```

`verify` 除了校验归档目录中的 SHA-256 固定性清单、文件登记和路径安全外，还会严格解析
`reports/check.json` 与 `reports/analysis.json`，检查报告版本、工具信息以及输入文件信息是否与
`metadata/manifest.json` 一致。
