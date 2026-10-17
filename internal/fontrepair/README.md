# 内嵌字体修复

本包在 OFD 仓库内实现字体修复。目录读写、cmap、CFF 解析、Type 2 度量和 OTF
封装按以下公开格式规范编写；测试使用 Go 自带字体和独立构造的二进制样例。
运行环境为仓库使用的 Go 1.26。

| 实现 | 格式依据 |
|---|---|
| SFNT 目录、排序、对齐、校验和 | [OpenType 1.9.1 Font File](https://learn.microsoft.com/en-us/typography/opentype/spec/otff)、[head](https://learn.microsoft.com/en-us/typography/opentype/spec/head) |
| Unicode 映射、format 4/12 和字形地址 | [OpenType cmap](https://learn.microsoft.com/en-us/typography/opentype/spec/cmap) |
| 补齐表及 OTF 度量 | OpenType [OS/2](https://learn.microsoft.com/en-us/typography/opentype/spec/os2)、[name](https://learn.microsoft.com/en-us/typography/opentype/spec/name)、[post](https://learn.microsoft.com/en-us/typography/opentype/spec/post)、[hhea](https://learn.microsoft.com/en-us/typography/opentype/spec/hhea)、[hmtx](https://learn.microsoft.com/en-us/typography/opentype/spec/hmtx)、[maxp](https://learn.microsoft.com/en-us/typography/opentype/spec/maxp) |
| CFF INDEX/DICT、charset、Private、CID/FDSelect | [Adobe Technical Note 5176](https://adobe-type-tools.github.io/font-tech-notes/pdfs/5176.CFF.pdf)，§4–5、§9–10、§13–16、§18–19 |
| 首字宽、子程序、提示掩码 | [Adobe Technical Note 5177](https://adobe-type-tools.github.io/font-tech-notes/pdfs/5177.Type2.pdf)，§2.3、§4.1 注4、§4.3、§4.5 |

`Repair` 保留已有 SFNT 表，补齐缺失的 `OS/2`、`name`、`post` 和字形寻址 cmap。
原 cmap 子表保留，在 Windows Unicode 全字符集子表中补入 `U+F0000 + GID` 地址。
未知格式原样返回，已识别但结构损坏的输入返回原数据和错误。输入字节只读。
TTC 字体面提取由调用方完成。

`RepairWithGlyphs` 和 `NewRepairer(...).WithGlyphs` 接受文档的显式 Unicode/GID
对应关系。显式映射优先于自动私有地址，同一 GID 的旧 Unicode 别名被移除；保留
variation selector 子表，其他编码合并为 format 12，避免消费者通过旧编码重新命中
错误别名。修复器缓存只读目录和映射，可并发调用；输入在修复器存活期间必须保持不变。

裸 CFF 以完整字体程序封装为 OTF，不重排字形、FD 或子程序。`unitsPerEm` 从
FontMatrix 首项的倒数得到（缺省1000，限制在 OpenType 的16–16384范围），水平
度量使用相同设计单位。字宽按每个字形的 FDSelect、Private 的 defaultWidthX /
nominalWidthX 和首个清栈操作数求得；解析子程序有深度、栈和总指令预算。
异常轮廓无法求出宽度时采用所属 Private 的缺省宽度，轮廓错误留给字体消费者处理。
本包不实现完整的字体有效性认证，也不对 FontMatrix 做一般仿射归一化。

OFD 的兼容策略独立于格式要求：

- 稀疏 CID 子集使用 `U+F0000 + CID` 指向实际 GID；文档字形引用超出实际 GID
  范围时由绘制与校验调用方通过该地址恢复。`WithGlyphs` 仅接受实际 GID 范围内
  的显式引用，防止把保留的 CID 地址误当成 GID 再次改写。
- OFD 渲染通过 `WithGlyphReferences` 兼容保留原 CID 的字体：若存在超出实际 GID
  范围且能由原私有 cmap 定位的引用，则该字体的范围内引用也按原 CID 地址解释。
  例如 `zsbk.ofd` 页脚的 CID18/19 分别是数字1/2，不能误写成子集内 GID18/19
  对应的“时/使”。自动 GID 地址保留已有 CID 地址，显式码位覆盖仍然优先。
- Adobe-GB1 的字体 Unicode 映射来自 [internal/cidmap](../cidmap/README.md)，
  与 PDF 文字提取的标点、异体序列语义分开。命名字形通过现有 textlayout 依赖的
  glyphsnames 数据查询 Unicode。
- `intro.ofd` 中标记为 Adobe-Identity 的 GB1 子集仍使用 GB1 字体映射作为兼容
  回退。该映射是文档兼容策略，不能从 Identity 名称推导为规范要求；显式文档映射优先。
- 针对当前轮廓后端，将裸 CFF 的 `cntrmask` 转成同长度 `hintmask`，保留掩码字节、
  轮廓坐标和子程序偏移；操作只发生在字体副本上。该兼容处理可能改变小字号提示效果。
- PDF 字体可能携带旧式 Type1 私有字段 `ForceBoldThreshold`、`lenIV`，Go 轮廓解析器
  可忽略它们，但浏览器 OTS 会拒绝整个 CFF，导致 SVG 文字回退、大小和字形异常。
  修复时按 Adobe 5176 表23清理这两个私有项，将新 Private DICT 及原 Local Subrs
  追加至表尾，并在原编码容量足够时重定向引用；字形编号、轮廓程序和度量不变。
  无法保留引用编码长度或超出复制预算时保留原表。该处理同时作用于裸 CFF 封装及
  已有 OTF；PDF 导入和 OFD 渲染共用此入口。`GBT_38540-2020.ofd` 是对应兼容样例。
- 渲染调用顺序为修复后的内嵌字体、原始字体、外部回退；校验器保留
  `semantic.font_repaired` 警告语义。

`cff_identifiers.go` 仅记录规范附录A/C的标准字符串与预定义 charset 标识数据。
用 `pdftotext -layout 5176.CFF.pdf /tmp/5176.CFF.txt` 后执行
`python3 internal/fontrepair/generate_cff.py /tmp/5176.CFF.txt` 可重新生成，随后运行
`gofmt -w internal/fontrepair/cff_identifiers.go`。

验证入口：`go test ./internal/fontrepair ./internal/cidmap`；边界测试提供 `FuzzRepair`。
跨渲染、PDF、校验与 WASM 的回归仍由各调用包负责。
