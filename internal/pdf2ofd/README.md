# PDF 转换维护说明

本包解释 PDF 页面内容，并通过 `pkg/creator` 生成 OFD。
用户可依赖的能力范围、近似行为、资源预算和失败处理统一记录在
[PDF 支持清单](../../docs/PDF-SUPPORT.md)。本文件用于定位实现、运行回归和维护依赖。

## 文档与代码注释的分工

| 内容 | 维护位置 |
|------|----------|
| 支持或不支持什么、近似结果、使用条件和错误策略 | [PDF 支持清单](../../docs/PDF-SUPPORT.md)。 |
| 跨文件的转换链路、不变量、测试入口、外部工具及依赖补丁 | 本文件。 |
| 某个函数为何这样处理、坐标/单位约定、不能改变的局部规则 | 对应实现旁的代码注释。 |
| 一个测试保护的缺陷、独立参考值来源、读者需要理解的样例差异 | 对应测试及 fixture 旁的注释。 |
| Adobe CMap 数据来源、版权和重建方法 | [cmaps/README.md](cmaps/README.md)。 |
| 公开 API、CLI 或配置的变更 | [CHANGELOG.md](../../CHANGELOG.md) 和相应接口/命令文档。 |

测试名称和断言是具体覆盖行为的主要记录。本文件只保留按主题查找的入口，
不逐次追加所有测试用例、临时输出路径或一次验证的数值结果。

## 实现入口与不变量

| 主题 | 入口 | 维护时需要保留的规则 |
|------|------|----------------------|
| 转换流程 | [convert.go](convert.go)、[pdf_content.go](pdf_content.go) | 输入字节在调用期间只读借用；解析页面、绘制注解、合并单字图元后再固定语义层定位；预算或 Strict 失败先于输出。 |
| 预算与取消 | [control.go](control.go)、[config/options.go](config/options.go)、[image_control.go](image_control.go) | 分配/展开前预留预算；缓存命中不重复计算基础解码；取消原样返回 context 错误。预算不是峰值内存保证。 |
| 页面和路径坐标 | [path.go](path.go)、[image.go](image.go) | PDF 使用点和左下原点，OFD 使用毫米和左上原点；同时处理 CTM、页面 Rotate、Box 原点及 UserUnit。Clips 的 TransFlag 不应重复施加图元 CTM。 |
| 字体代码、CID 和 GID | [font.go](font.go)、[font_cmap.go](font_cmap.go)、[font_cmap_predefined.go](font_cmap_predefined.go) | 修复后的字体只解析一次，字形映射和字宽复用 font.sfnt。ToUnicode 查询源代码；嵌入 Encoding CMap 的代码还包含字节长度；W/W2、CIDToGIDMap 查询 CID，字体程序使用实际 GID。internal/fontrepair 私有区映射用于找字形，不能作为原文；CFF 包装的 unitsPerEm 必须与 FontMatrix 一致。 |
| CMap 区间 | [font_cmap.go](font_cmap.go) | 保留继承、本地最后覆盖、notdef 和字典优先级；区间排序后查询，不展开巨大 CID 范围；损坏代码需保留后续有效代码。 |
| 普通文字与裁剪 | [text.go](text.go)、[text_clip.go](text_clip.go) | 字形与推进共同处理 Tm/CTM/Tz/Ts；BT 内累积字形并集，ET 时与已有裁剪求交。Form 的文本、路径和资源状态与外层隔离。 |
| 竖排 | [text_vertical.go](text_vertical.go)、[font_vertical_glyph.go](font_vertical_glyph.go) | 竖排 TJ 和 W2 原点偏移不额外乘 Tz；嵌入字体保留指定字形，替代字体优先 vrt2 再 vert，文字层保留源标点。 |
| Type3 与语义字体 | [font_type3.go](font_type3.go)、[font_semantic.go](font_semantic.go) | CharProc 绘制可见内容，空轮廓字体承载 Unicode；每个码位使用独立 GID，保留参考字宽，显式增量在图元合并后固定为逐字定位。 |
| 原图像分量 | [image.go](image.go)、[image_jpx.go](image_jpx.go)、[image_jpeg_decode.go](image_jpeg_decode.go) | JPEG 优先读取有效 Content，Raw 用于尚未解码的流；Adobe 反相、PDF Decode 和颜色转换有固定顺序。普通 JPX 忽略 PDF Decode/字典位深，先解释码流颜色与 cdef。高位深不能提前降到 8 位。 |
| 图像 Mask/SMask/Matte | [image_mask.go](image_mask.go)、[image_softmask.go](image_softmask.go)、[image_matte.go](image_matte.go) 及相应压缩格式文件 | 颜色键在 Decode/颜色转换前匹配；Matte 在原颜色空间还原；SMask 覆盖 Mask，图像关联软掩码覆盖图形状态软掩码。不同尺寸按各轴最大分辨率合成，Matte 仍要求同尺寸。 |
| 图像通道与颜色 | [image_jpx_colour.go](image_jpx_colour.go)、[image_mask_coordinates.go](image_mask_coordinates.go) | cdef 按 Association 解释，条目顺序不改变映射；最近邻坐标乘法使用 64 位整数。 |
| 透明合成 | [transparency.go](transparency.go)、[transparency_blend.go](transparency_blend.go) | 非隔离组分别保留初始背景和组自身 alpha，结束时撤销背景贡献；挖空按形状覆盖率处理，不能把旧内容衰减两次。嵌套组复用同一像素网格。 |
| 图形状态软掩码 | [transparency_mask.go](transparency_mask.go) | 捕获 gs 时的 CTM；G 的资源优先、缺省继承；直接执行 G，不能插入覆盖源资源的临时 XObject 名。BC 决定组边界外的初始值，TR 作用于组内。 |
| 着色与图案 | [shading.go](shading.go)、[mesh.go](mesh.go)、[pattern.go](pattern.go)、[function_type4.go](function_type4.go) | 网格流可能存函数输入而非颜色；矢量/栅格回退、函数求值和图块展开都要遵守预算。 |
| 注解和跳转 | [annotation.go](annotation.go)、[link.go](link.go)、[outline.go](outline.go) | 外观按 BBox/Matrix 映射至 Rect；透明外观复用 XObject 路径，避免重复应用 Matrix；目标位置使用目标页的几何。 |

## 回归入口

下表提供主题入口。组合场景、预算、取消和诊断的具体断言以测试源码为准。

| 主题 | 测试入口 |
|------|----------|
| 页面几何、内容流、基础图像和文字合并 | [pdf_to_ofd_test.go](pdf_to_ofd_test.go)、[pdf_content_test.go](pdf_content_test.go)、[inline_image_test.go](inline_image_test.go)、[content_fidelity_test.go](content_fidelity_test.go)、[path_fidelity_test.go](path_fidelity_test.go)、[graphics_state_test.go](graphics_state_test.go)、[dash_test.go](dash_test.go)、[form_test.go](form_test.go)。 |
| 字体样式、字形裁剪、仿射基线、竖排和 Type3 | [text_test.go](text_test.go)、[font_loading_test.go](font_loading_test.go)、[text_fidelity_test.go](text_fidelity_test.go)、[text_vertical_test.go](text_vertical_test.go)、[font_type3_test.go](font_type3_test.go)。 |
| 预定义/嵌入 CMap、竖排替代和缺字体处理 | [font_cmap_predefined_test.go](font_cmap_predefined_test.go)、[font_cmap_embedded_test.go](font_cmap_embedded_test.go)、[font_vertical_glyph_test.go](font_vertical_glyph_test.go)、[font_vertical_missing_test.go](font_vertical_missing_test.go)。 |
| 语义字体字宽、完整单词和逐字符位置 | [font_semantic_test.go](font_semantic_test.go)；创建器的独立空格定位见 [creator_test.go](../../pkg/creator/creator_test.go)。 |
| 着色、图案、颜色空间和函数 | [shading_test.go](shading_test.go)、[mesh_test.go](mesh_test.go)、[pattern_test.go](pattern_test.go)、[colorspace_test.go](colorspace_test.go)、[function_type4_test.go](function_type4_test.go)。 |
| 混合、透明组、图形状态软掩码和同名资源 | [transparency_test.go](transparency_test.go)、[transparency_mask_resources_test.go](transparency_mask_resources_test.go)、[blend_group_test.go](blend_group_test.go)、[multiply_test.go](multiply_test.go)、[blendmask_test.go](blendmask_test.go)。 |
| 图片仿射变换 | [image_transform_test.go](image_transform_test.go)。 |
| 普通/压缩颜色键及掩码优先级 | [image_mask_test.go](image_mask_test.go)、[image_mask_compressed_test.go](image_mask_compressed_test.go)、[image_mask_jpx_test.go](image_mask_jpx_test.go)、[image_mask_priority_test.go](image_mask_priority_test.go)、[image_mask_conflict_test.go](image_mask_conflict_test.go)。 |
| 高位深软掩码、分辨率和坐标安全 | [image_softmask_test.go](image_softmask_test.go)、[image_softmask_resolution_test.go](image_softmask_resolution_test.go)、[image_mask_coordinates_test.go](image_mask_coordinates_test.go)。 |
| Matte 与压缩底图/掩码 | [image_matte_test.go](image_matte_test.go)、[image_matte_jpeg_test.go](image_matte_jpeg_test.go)、[image_matte_jbig2_test.go](image_matte_jbig2_test.go)、[image_matte_jpx_test.go](image_matte_jpx_test.go)。 |
| JPEG/JBIG2 解码与 Decode | [image_jpeg_decode_test.go](image_jpeg_decode_test.go)、[image_jbig2_test.go](image_jbig2_test.go)。 |
| JPX 位深、透明度、通道、颜色和截断 MQ | [image_jpx_test.go](image_jpx_test.go)、[image_jpx_precision_test.go](image_jpx_precision_test.go)、[image_jpx_channels_test.go](image_jpx_channels_test.go)、[image_jpx_colour_test.go](image_jpx_colour_test.go)、[image_jpx_eycc_test.go](image_jpx_eycc_test.go)、[image_jpx_lowrate_test.go](image_jpx_lowrate_test.go)。 |
| 注解、链接和大纲 | [annotation_test.go](annotation_test.go)、[link_test.go](link_test.go)、[outline_test.go](outline_test.go)。 |
| 输入/解压/像素/字体/处理量/深度预算与原子输出 | [limits_test.go](limits_test.go)，以及各专项测试的预算、Strict、取消和缓存分支。 |
| 多 FD CFF 的 OFD→PDF 输出 | [pdf_cff_test.go](../../pkg/converter/pdf_cff_test.go) 及 font/canvas fork 中的字体与 PDF 回归。 |
| 公开转换入口和真实文档往返 | [PDF 导入器测试目录](../../pkg/converter/import/pdf/)、[converter 测试目录](../../pkg/converter/)。 |

`TestConvertTestdataPDFs` 使用 `testdata/pdf/` 中的样本。样本、外部字体或独立阅读器
不可用时，应查看测试的 skip/省略日志；一次 `go test` 成功不表示全部外部验证已执行。

### 常用命令

以下命令均在仓库根目录运行：

```sh
go test -mod=readonly ./internal/pdf2ofd -count=1
go test -mod=readonly ./pkg/converter ./pkg/converter/import/pdf ./pkg/creator ./pkg/webreader -count=1
go vet -mod=readonly ./internal/pdf2ofd ./pkg/converter ./pkg/creator ./pkg/webreader
CGO_ENABLED=0 go test -mod=readonly -tags purego ./internal/pdf2ofd -run 'TestSemantic|TestType3|TestTextClipping|TestTransparency' -count=1
go run ./cmd/ofd-validator --format text --mode strict /path/to/output.ofd
```

OFD 严格校验预期为 0 错误、0 警告。它检验包结构和语义，不替代源 PDF 的显示对照。
真实杂志、标准文档等往返测试可能需要较长时间，运行时应保留完整结果。

### 外部字体与证据输出

| 环境变量 | 用途 |
|----------|------|
| OFD_CJK_TEST_FONT | 覆盖中文与竖排标点的 TrueType 字体；TTC 使用首个字体面。未指定时按测试规则尝试系统字体。 |
| OFD_CFF_TEST_FONT | 覆盖中文的多 FD OTF/TTC，用于转换器和 Web 阅读器 PDF 字体回退。 |
| OFD_CFF_CID_TEST_FONT | CID≠GID 的 CFF 子集字体。 |
| OFD_CFF_SINGLE_TEST_FONT | 普通单 FD CFF，验证子集化回归。 |
| OFD_TRANSPARENCY_EVIDENCE_DIR | 导出透明专项的源/往返 PDF、OFD 栅格和独立阅读器栅格；目录需事先创建。 |
| OFD_TEXT_EVIDENCE_DIR | 导出文字保真专项证据；按测试约定准备目录。 |

```sh
OFD_CJK_TEST_FONT=/path/to/cjk-font.ttf go test ./internal/pdf2ofd -run 'TestCJK' -count=1 -v
```

测试字体从外部加载；没有合适字体时明确 skip，不下载或提交字体。基础专项也使用
Go 自带字体和内嵌的小型码流。Adobe CMap 是运行映射数据，来源、许可与重建由
[cmaps/README.md](cmaps/README.md) 和 `SHA256SUMS` 维护。

## 独立验证与阅读器差异

PDF JPX 解码默认使用纯 Go 后端。Linux 可选构建 OpenJPEG 后端：安装 `libopenjp2`
开发文件与 `pkg-config` 后，使用 `CGO_ENABLED=1 go test -tags openjpeg ./internal/pdf2ofd`
验证；调用方可通过 `Options.JPXBackend` 选择 `openjpeg`。该后端应用 PDF `/Decode`
数组，用于需要匹配特定阅读器显示行为的文档。

修改渲染逻辑时，检查源 PDF、直接 OFD 栅格、往返 PDF 栅格和文字提取。
仅比较 OFD 与往返 PDF，或仅通过格式校验，不能证明与源 PDF 一致。

| 验证内容 | 方法 |
|----------|------|
| 普通显示与透明混合 | 使用 MuPDF 的 mutool 和 Poppler 的 pdftoppm 渲染源 PDF，比较平坦区域的全部颜色通道；单独保留抗锯齿和插值边界差异。 |
| 字符语义和位置 | 使用 pdftotext 检查完整文本；使用 mutool 的结构化文字检查逐字符原点。语义字体的定位回归容差为 0.02pt。 |
| 原始压缩样本 | 使用独立 OpenJPEG、ImageMagick 或手工构造的 JPEG/JBIG2 参考，验证原分量、位深、通道和 alpha；不要只依赖同一 Go 编解码器往返。 |
| 数据与字体稳定性 | Adobe 映射按源资源及 SHA-256 核对；CFF 对比原程序、CID/GID、ToUnicode，以及重复导出是否改写字体。 |

以下是回归中已观察到的阅读器差异，版本和参考值应保留在对应测试注释中，
不能据此宣称与所有阅读器一致：

| 场景 | 已观察到的差异与验证选择 |
|------|--------------------------|
| 挖空组、软掩码 TR、AIS | MuPDF 1.21 与 Poppler 22.12 的 K/TR 支持不同；挖空以 MuPDF 对照，TR 以 Poppler 对照，AIS 另验证覆盖率公式并保留近似诊断。 |
| 开放路径 B/B* 的半透明填充与描边 | MuPDF 1.21 将描边形状从填充中挖空；Poppler 22.12 在重叠区保留填充贡献。按 PDF 的单个透明对象语义对照 MuPDF，转换以局部栅格合并并报告诊断。 |
| Type3 d1 的 Tr=1/2/5/6 | 现有 MuPDF 与 Poppler 对描边/非描边颜色的选择不同；回归保留 CharProc 及调用方非描边颜色语义。 |
| sYCC（JP2 colr=18） | 现有 Poppler 对独立样例直接显示 Y/Cb/Cr；颜色转换以 BT.601 原分量参考和 MuPDF 对照。 |
| eYCC（JP2 colr=24） | 现有 MuPDF/Poppler 对独立样例未应用 eYCC 矩阵；使用 OpenJPEG 固定参考和等价 RGB PDF 验证，保留原阅读器结果。 |
| JPX 颜色键 | MuPDF 与 Poppler 对部分源颜色键处理不同，二者也未区分某些相邻 16 位样本；同时验证原整数匹配与显式 RGB 参考 PDF。 |
| 高位深 JPX SMask | 现有 Poppler 对部分样例有布局差异；保留其结果，并与 MuPDF 和原 alpha 参考对照。 |
| CMYK/YCCK | 黑色端点和色彩转换因阅读器而异；不能把 CMYK→RGB 近似等同于原空间保真。 |
| 图片放大与透明边缘 | 最近邻和平滑插值会产生边缘差异；原尺寸原分量测试与放大显示对照分别验收。 |
| 变长 ToUnicode | 源阅读器的提取行为存在差异；代码映射及往返提取需直接断言，不能只以源提取为参考。 |

32 位最近邻坐标计算另有独立回归；整包 386 验证曾被 `go-xml/xdm` 的既有年份
常量溢出阻塞，不能把单个坐标回归通过写成整项目已通过 32 位编译。

## OFD→PDF 字体输出与依赖维护

多 FD CFF 子集化尚未实现。创建器保留完整字体面，转换器和 Web 阅读器的 PDF 输出
按字体使用完整嵌入回退；其他字体继续子集化。TTC 只提取所选字体面。

CID-keyed CFF 写为 `FontFile3 /CIDFontType0C`。PDF Encoding 将紧凑字符代码映射到
原 CFF charset 中的 CID，W 按 CID 写出；CIDFontType0 不使用 TrueType 专属的
CIDToGIDMap。ToUnicode 来自实际源文本，保留共享字形的不同 Unicode、连字和
补充平面字符。完整嵌入会增大 PDF，但应避免不安全地重排 FDSelect 和局部子程序。

| 依赖 | 维护内容 |
|------|----------|
| [zc310/font](https://github.com/zc310/font) | CID/charset 与 Font DICT 元数据、字体面提取和模块路径；工作区源码在 `../third_party/font`。 |
| [zc310/canvas](https://github.com/zc310/canvas) | 多 FD CFF 嵌入、源文本 ToUnicode、逐字体回退、填充/描边共享透明度恢复；工作区源码在 `../third_party/canvas`。 |
| go-jpeg2000 | 工作区 `../third_party/go-jpeg2000` 中基于 v1.5.12 的截断 MQ 补丁；按实际编码轮数停止，并在剩余区间中点还原幅值。维护方式和许可见该仓库的 `OFD-PATCH.md`。 |

本地 replace 会使用工作区依赖补丁。发布时需同步依赖版本，并确认脱离本地 replace
仍可构建；测试源码通过本地补丁不等于补丁已进入发布依赖。

font/canvas fork 单独运行默认 go vet 曾有 CFF2 不可达代码和 Point/Rect 未指定字段名
的既有提示。维护记录应区分项目相关包检查和依赖自身检查，不能合并描述为全部通过。
