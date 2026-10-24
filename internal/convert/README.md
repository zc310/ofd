# 外部转换工具

本目录存放通过外部程序生成 PDF 的适配包，负责可执行文件查找、上下文取消与超时、
临时目录管理以及转换结果检查。

| 包        | 外部工具                                    | 输入                                    |
|-----------|---------------------------------------------|-----------------------------------------|
| `browser` | Chrome/Chromium，通过 chromedp 调用打印接口 | HTML、MHTML                             |
| `office`  | LibreOffice                                 | DOCX、DOC、ODT、RTF、WPS、PPTX、XLSX 等 |
| `xps`     | xpstopdf（libgxps）                         | XPS                                     |

格式注册、输入适配和转换流程位于 `pkg/converter/import/<format>`。
这些导入器调用相应工具取得 PDF，再按目标格式返回 PDF 或交给 `internal/pdf2ofd`
生成 OFD。CLI 的外部任务并发限制由 `cmd/ofd-converter` 统一管理。

验证入口：

```sh
go test ./internal/convert/... ./pkg/converter/import/html ./pkg/converter/import/office ./pkg/converter/import/xps -count=1
go vet ./internal/convert/... ./pkg/converter/import/html ./pkg/converter/import/office ./pkg/converter/import/xps
```

外部工具未安装时，依赖真实工具的集成测试会跳过；模拟进程的取消、超时和输出校验
测试仍可运行。
