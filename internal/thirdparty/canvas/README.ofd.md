# OFD 内置 canvas

本目录是供 OFD 使用的精简副本，不是独立 Go 模块。
后续仅在 OFD 仓库维护，不再同步维护外部 canvas 仓库。

- 原始上游：https://github.com/tdewolff/canvas ，版权及 MIT 许可见 `LICENSE.md`。
- 基于上游提交 `09804640d00c5753b00ee0a449c88ad2338d13cf` 复制并作项目适配，
  并非该提交的逐字副本。
- 包含渐变函数共享、独立文字画笔坐标、原生路径裁剪、字体及资源处理等修改与回归测试。
- 混合脚本的文字轮廓绘制共享文字坐标中的渐变，不在中英文 span 边界重新起算；
  仅变换字形副本，保留字体缓存、原生渐变类型及文字位置。
- PDF 目的地的 XYZ 缩放倍率不做毫米到点的换算（ISO 32000-1 §12.3.2 表151）。
- 导入路径改为 `github.com/zc310/ofd/internal/thirdparty/canvas`，统一使用 OFD 的模块依赖。
- 字体解析统一使用相邻的 `internal/thirdparty/font`，不依赖外部 font 模块。
- 系统字体匹配保留 TTC/OTC 字体面索引和原生样式；旧版磁盘字体索引自动重建。
  `LoadSystemFont` 返回实际原生样式，`FontFamily.LoadSystemFont` 也按原生样式入槽；
  请求 Bold 而仅匹配到 Regular 时，后续 `Face(..., FontBold)` 会合成粗体，不把正体冒充原生粗体。
- 合成粗体按每条轮廓的方向及 NonZero 填充关系扩张外轮廓、收缩孔洞，支持同一
  字形内多个方向相反的外轮廓，不再以整字形面积和推断方向；判定不额外 flatten 轮廓。
  字形绕数对水平重合台阶两端分别计半次穿越，避免宋体“变”等字的孔洞被误扩张。
- PDF 同时对子集能力支持的单 FD、多 FD CFF 字体进行子集化，保留原 CID 并重建
  FDSelect；共享全局子程序调用不同 FD 的局部子程序时保留原子程序索引，避免错字
  （Adobe 5176 §13、§18–19）。
- 保留几何、绘制记录、颜色、图像、文字及 PDF/SVG/光栅后端。
- 不迁移任何字体文件、演示资源、示例、命令、LaTeX 解析和 Markdown 排版；移除了
  `RichText.WriteLaTeX`；PS 和 TeX 输出后端也不迁移。
- 保留相关测试；DejaVu 字体测试改用已有 `goregular.TTF`，不复制字体二进制。
  依赖其他上游资源的 `path_test.go`、`text_test.go`、`text/harfbuzz_test.go`、`renderers/pdf/pdf_test.go`
  按实际资源依赖裁剪；OFD 自身的字体与几何回归测试继续保留。
  单 FD CFF 外部测试字体可通过 `OFD_CFF_SINGLE_FONT` 指定，多 FD 使用 `OFD_CFF_TEST_FONT`。
  上游精确字体度量测试需要显式设置 `OFD_DEJAVU_SERIF_FONT`；未设置时跳过，不能
  把 DejaVu 的固定度量期望直接套用到 Go Regular。

新增或修改的代码注释使用英文。不得把外部字体或样本作为迁移附带资源加入本目录。

验证：

```sh
go test ./internal/thirdparty/canvas/...
go test ./internal/render/... ./pkg/converter ./pkg/reader ./pkg/creator
```

迁移验证使用 `GOWORK=off`，不依赖仓库外的 checkout。迁入代码保留了上游的命名、
TODO、未使用代码等历史 lint 告警；本次不为消除这些告警重写几何算法。
业务适配层单独进行增量 lint 检查，第三方代码的历史告警不代表该检查已通过。
