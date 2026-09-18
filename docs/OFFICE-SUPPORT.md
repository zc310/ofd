# Office 文档转 OFD/PDF 支持清单

本文档描述 `pkg/converter/officeimport` 与 `internal/office` 对 Office 文档
（doc/docx/odt/rtf/wps/pptx/xlsx 等）转换为 PDF 和 OFD 的支持范围。

实现方式：通过命令行调用 **LibreOffice**（`soffice --headless`）完成格式转换，
不引入任何 Go 依赖。转换链路：

- Office → PDF：LibreOffice 直接输出 PDF。
- Office → OFD：LibreOffice 先转 PDF，再复用 `internal/pdf2ofd`（见
  [`PDF-SUPPORT.md`](PDF-SUPPORT.md)）生成 OFD，**保留文字层**。

本文档描述的是项目当前的实现范围，不代表完整实现或通过任何一致性认证。Office
文档排版复杂，转换保真度受 LibreOffice 版本、系统字体和源文档特性影响，使用前
应以目标业务样本验证。

## 运行依赖

| 项目        | 说明                                                                                                                                                          |
|-------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------|
| LibreOffice | 必须已安装（`libreoffice-nogui` 或完整版均可）。程序查找顺序：`--soffice` → 环境变量 `OFD_SOFFICE` → `PATH` 中的 `soffice`/`libreoffice` → 平台常见安装路径。 |
| 未安装行为  | 返回明确错误（`未找到 LibreOffice…`），不会静默降级。                                                                                                         |
| 字体        | 目标机器需安装源文档使用的字体，否则 LibreOffice 会回退，导致版式偏移。                                                                                       |

## 支持的输入格式

| 类别     | 格式                                                                             | 当前状态 |
|----------|----------------------------------------------------------------------------------|----------|
| 文字文档 | `docx`、`docm`、`dotx`、`dotm`、`doc`、`dot`、`odt`、`ott`、`fodt`、`rtf`、`wps` | 已支持   |
| 演示文稿 | `pptx`、`pptm`、`ppsx`、`potx`、`ppt`、`pps`、`pot`、`odp`、`otp`、`fodp`        | 已支持   |
| 电子表格 | `xlsx`、`xlsm`、`xltx`、`xltm`、`xls`、`xlt`、`ods`、`ots`、`fods`               | 已支持   |

> 具体格式能否解析由 LibreOffice 决定；本包只负责把文件交给 LibreOffice 并读取
> 生成的 PDF。

## 支持的输出格式

| 目标 | 当前状态 | 说明                                                                    |
|------|----------|-------------------------------------------------------------------------|
| PDF  | 已支持   | LibreOffice 直接输出，保真度最高；`converter.Convert(from, "pdf", …)`。 |
| OFD  | 已支持   | 先转 PDF，再经 `pdf2ofd`；文字可搜索，版式取决于 PDF 转换结果。         |
| 其他 | 已支持   | 先导入为 OFD 再走现有导出器（如 Office→html、Office→png）。             |

## 转换行为

| 项目           | 说明                                                                                              |
|----------------|---------------------------------------------------------------------------------------------------|
| 并发           | 每次转换使用独立临时目录与独立 LibreOffice 用户配置目录（`-env:UserInstallation`），互不干扰。    |
| 超时           | 默认 120 秒（`--office-timeout` 可调）；超时会结束整个进程组，避免残留 `soffice.bin`。            |
| 返回值校验     | LibreOffice 有时失败仍返回 0，因此以"输出 PDF 存在且非空"为准，不信任退出码。                     |
| 临时文件       | 输入复制到临时目录，转换结束后全部清理；临时目录默认为系统临时目录，可用 `--temp-dir` 指定。      |
| 安全           | Office 文档可能携带宏；建议对不可信文档在沙箱中运行。本包不改变 LibreOffice 的宏安全设置。        |
| 确定性         | 输出 PDF 含创建时间与生产者信息，不同 LibreOffice 版本或字体环境结果可能不同。                    |

## 已知限制

| 范围     | 说明                                                            |
|----------|-----------------------------------------------------------------|
| 外部依赖 | 必须安装 LibreOffice，无法在纯 Go 环境运行。                    |
| 版本差异 | 不同 LibreOffice 版本的排版与导出结果存在差异。                 |
| 复杂版式 | 公式、图表、SmartArt、嵌入对象、宏等按 LibreOffice 的能力转换。 |
| 性能     | 每次启动 LibreOffice 约 1–3 秒，批量转换较慢；默认并发为 2。    |
| 字体缺失 | 系统缺字体时回退，可能出现换行与位置偏差。                      |

## 使用方式

库：

```go
import (
    "github.com/zc310/ofd/pkg/converter"
    _ "github.com/zc310/ofd/pkg/converter/officeimport"
)

converter.Convert("docx", "pdf", "report.docx", w) // 直接转 PDF
converter.Convert("docx", "ofd", "report.docx", w) // 转 OFD
converter.Convert("", "ofd", "report.docx", w)     // 按扩展名识别
```

命令行：

```bash
ofd-converter report.docx report.pdf
ofd-converter --from docx --format ofd report.docx report.ofd
ofd-converter report.doc report.ofd
ofd-converter --soffice /opt/libreoffice/program/soffice --office-timeout 300 report.pptx report.ofd
```

## 验证方式

```bash
go test ./pkg/converter/officeimport -count=1
go test ./internal/office -count=1
go run ./cmd/ofd-validator --format text --mode strict output.ofd
```

集成测试在 `PATH` 中找不到 `soffice` 时会自动跳过。
