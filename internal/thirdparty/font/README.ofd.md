# OFD 内置 font

本目录是供 OFD 使用的源码副本，不是独立 Go 模块。
后续仅在 OFD 仓库维护，不再同步维护外部 font 仓库。

- 原始上游：https://github.com/tdewolff/font ，版权及 MIT 许可见 `LICENSE.md`。
- 基于上游提交 `88a7f6b5691888bf23e957963fc9e49ff39dc16d` 复制并作项目适配，
  并非该提交的逐字副本。
- 导入路径为 `github.com/zc310/ofd/internal/thirdparty/font`，统一使用 OFD 的模块依赖。
- 保留根包的完整实现与测试，包括 SFNT/TrueType/CFF、字体面提取、子集化、合并、
  AFM、EOT、WOFF/WOFF2 和系统字体查询；迁移不修改字体算法。
- SFNT 写出按表目录及 4 字节对齐后的总长度预分配缓冲，避免大字体反复扩容复制；
  不改变表内容、顺序、校验和或时间戳，由独立的对齐/校验和回归测试保护。
- SFNT 名称记录遇到尚未实现的编码时保留原始字节，不调用空解码器；覆盖 Android
  字体中 Macintosh 简体中文名称触发缺字回退时的空指针，已支持的名称解码保持不变。
- 按 OpenType 1.9.1 name 表的 `storageOffset` 读取字符串，允许版本 0/1 的记录区与
  字符串区之间存在填充；继续拒绝与记录区重叠或越界的存储偏移。
- TrueType 的单个 off-curve 点退化轮廓不生成线段，避免宋体 TTC 空格在平移、
  合成粗体后产生整段文字下方的横线；空格步进宽度保持不变。
- 系统索引枚举 TTC/OTC 全部字体面，保留本地化族名、完整名、PostScript 名、原生字重、
  倾斜样式及字体面索引；集合目录可共享、乱序，表偏移按整个文件解释。
  同名同样式优先保留独立字体，避免新增集合扫描改变已有 TTF/OTF 匹配结果。
- 不迁入独立模块文件、示例、辅助脚本、resources、testdata 或字体二进制。
- 新增的无外部资源测试使用已有依赖中的 `goregular.TTF`，覆盖解析、第三方字体对象转换、
  子集化和 WOFF2 往返；TrueType 确定性与时间戳测试也使用 Go Regular。
- 依赖上游精确样本的测试保留原有断言，通过 `OFD_FONT_TEST_ROOT` 指定包含原始
  `resources/` 和 `testdata/` 的外部目录；未设置时按包目录查找。
  仅资源不存在时跳过，并报告缺失路径；其他读取或解析错误必须失败。
  DejaVu、EB Garamond、CID CFF 和 W3C WOFF/WOFF2 样本不由本仓库附带。

验证不依赖外部 workspace：

```sh
GOWORK=off go test ./internal/thirdparty/font ./internal/thirdparty/canvas/...
GOWORK=off go test ./internal/fontrepair ./internal/pdf2ofd ./internal/pdfcpuwriter ./internal/render/... ./internal/report ./pkg/converter ./pkg/reader ./pkg/creator/... ./pkg/validator
# 可选：准备具有原目录结构的上游测试资源，再运行完整样本测试。
GOWORK=off OFD_FONT_TEST_ROOT=/path/to/font go test ./internal/thirdparty/font -count=1
```

保留上游的命名、注释、TODO 和历史 lint 告警，不为通过 lint 改写字体算法。
业务调用方单独做增量 lint；检查通过不代表迁入源码的历史告警已清理。
例如 `sfnt_cff.go` 保留了上游的不可达代码，直接运行 `go vet` 会报告该问题；
canvas 也有既有的不可达代码与非具名结构体初始化告警。
