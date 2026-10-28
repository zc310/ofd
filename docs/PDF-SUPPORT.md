# PDF 转 OFD 支持清单

本文档说明 PDF（ISO 32000-1/-2）转 OFD 的能力范围、限制和失败处理，适用于
`converter.Convert(ctx, "pdf", "ofd", ...)`、`ofd-converter` 的 PDF 输入，以及
Office/HTML/EML/SVG 转 OFD 时的 PDF 中间阶段。

EML 导入器在页面写出时另行提供原附件，由创建器写入 OFD 附件区；这是上游 EML 数据，
并非迁移 PDF 自身的附件。详见 [EML 支持清单](EML-SUPPORT.md)。

| 级别 | 含义 |
|------|------|
| 已支持 | 表中列出的范围已有实现；不代表支持该特性的所有组合。 |
| 部分支持 | 可以转换列出的内容，但存在近似、依赖条件或未覆盖的结构。 |
| 暂不支持 | 相关语义未输出，或内容被忽略、跳过、近似处理或报告诊断。 |

实现与回归验证说明见 [PDF 转换维护说明](../internal/pdf2ofd/README.md)。
命令行用法见 [ofd-converter](../cmd/ofd-converter/README.md#pdf-转-ofd)。

## 已支持

### 页面、路径与图形状态

| 能力 | 支持范围 |
|------|----------|
| 页面尺寸 | `MediaBox`、`CropBox` 及继承属性；缺少 `CropBox` 时回退到 `MediaBox`，没有有效页面尺寸则转换失败。 |
| 页面坐标 | 非零 Box 原点、`UserUnit`；PDF 点坐标换算为 OFD 左上原点的毫米坐标。 |
| 状态与矩阵 | `q/Q` 保存和恢复图形状态，`cm` 累积变换；Form 自带 `Matrix` 与资源作用域。 |
| 路径构造 | `m/l/c/v/y/h/re`，包括矩形、直线和三次贝塞尔曲线。 |
| 路径绘制 | `S/s/f/F/f*/B/B*/b/b*/n`；描边、填充、描边加填充，以及 NonZero/Even-Odd 填充规则。`B/B*` 的填充隐式闭合，描边保留开放子路径；`b/b*` 显式闭合最后一个子路径。 |
| 路径裁剪 | `W/W*` 在当前路径绘制完成后生效；支持与已有裁剪求交及 `q/Q` 恢复。 |
| 设备颜色 | 路径和文字的 DeviceGray、DeviceRGB、DeviceCMYK；输出颜色转换为 RGB。CMYK 外观受颜色转换方式影响。 |
| 颜色空间选择 | `cs/CS` 重置对应颜色空间的初始颜色，并清除旧图案选择；描边与非描边状态独立。 |
| 常量透明度 | `ca/CA` 分别保留填充与描边不透明度，应用于路径、文字和图片。 |
| 图片几何 | 保留旋转、错切、镜像等仿射变换，并处理 CropBox 原点、UserUnit 和页面旋转。 |

### 文字、编码与字宽

| 能力 | 支持范围 |
|------|----------|
| 文本状态与定位 | `BT/ET/Tf/Tm/Td/TD/TL/T*`；保留旋转、错切、镜像、非均匀缩放等文字变换。 |
| 文本显示 | `Tj/TJ/'/"`；保留数值偏移，纯空白文本仍推进后续文字的位置。 |
| 字符串语法 | 字面字符串支持转义、八进制和反斜杠续行（LF、CR、CRLF）；未转义行结束符归一化为 LF。十六进制字符串接受 PDF 定义的全部空白，奇数个半字节按 PDF 规则末位补 0。 |
| 字距、缩放与升降 | `Tc/Tw/Tz/Ts`；保留原始字符推进、水平缩放和基线升降。Tw 按源单字节空格代码判断。 |
| 普通字体渲染模式 | `Tr=0–7` 的填充、描边、不可见文字和字形裁剪；裁剪字形在 `ET` 时求并集，再与已有裁剪求交。 |
| 不可见文字层 | `Tr=3/7`、空裁剪隐藏的文字及透明合成中的文字保留原文和位置；空轮廓字体保留源字宽，不产生可见笔画。 |
| ToUnicode | `bfchar/bfrange`，包括数组目标、连续区间和多 Unicode 码位映射。 |
| 简单字体编码 | `/Encoding`、`/Differences`；WinAnsi、MacRoman、MacExpert、Symbol、ZapfDingbats、AdobeStandard。 |
| 预定义 CJK CMap | 内置 Adobe-GB1/CNS1/Japan1/Korea1 的 150 个横排/竖排 Encoding CMap 和四个字符集的 CID→Unicode 映射；GB1 文字映射通过仓库内 `internal/cidmap` 共用，保留异体选择符及与字体修复不同的标点映射，其余三份表保存在导入器中。不依赖系统 CMap 或网络。 |
| 嵌入 Encoding CMap | 固定或混合 1–4 字节代码、`cidchar/cidrange`、`notdefchar/notdefrange`、WMode；名称或流形式的 UseCMap 继承及本地覆盖。保留代码长度，区分 `<01>` 与 `<0001>`。 |
| 横排字宽 | `Widths/FirstChar`、CID 的 `W/DW`；按源字宽保存字符推进，避免子集字体的占位 hmtx 改变字距。 |
| 竖排度量 | `W2/DW2` 的数组和区间形式，包含字形原点与垂直推进；W/W2 按 CID 查询，ToUnicode 按原始代码查询。 |
| 字体与字形映射 | `CIDToGIDMap`、字体 cmap、`CGTransform` 指定字形，以及不连续 CID 和子集字体映射。 |
| 字体名称与样式 | 字体名的 `#XX` 转义、常见粗体/斜体/衬线/等宽声明。 |

### 文档结构与容错

| 能力 | 支持范围 |
|------|----------|
| 输入形式 | 文件路径、`[]byte`、`io.Reader`；拒绝 typed-nil `io.Reader`。`[]byte` 在调用期间只读借用，不额外复制整个输入；调用方须保持数据不变直到转换返回。 |
| PDF 对象结构 | 经典 xref、压缩对象流。 |
| 内容流分词 | 按 PDF 空白和分隔符划分指令、数值及名称，支持紧邻 token 的 `%` 注释、NUL 和换页符。 |
| 常见损坏结构 | 尝试修复 xref/free-list 异常、解析异常和损坏的 Info 日期；修复失败仍返回错误。 |
| 元数据 | Title、Author、Subject；记录源格式和 Producer（缺失时退回 Creator），写入当前转换器版本。 |
| 链接 | `/Subtype /Link` 以及带可转换动作的 `/Subtype /Widget`；支持 Dest、GoTo、URI，保留点击热区和页面跳转位置。单页注解最多 65,536 条，注解和链接都计入处理量预算，链接计入输出对象预算。超限时返回资源限制错误。 |
| 大纲与书签 | Outlines 层级、展开状态、命名目标、GoTo/URI、PageMode UseOutlines，以及 XYZ/Fit/FitH/FitV/FitR 目标；FitB 系列按对应 Fit 类型近似。 |

## 部分支持

### 字体与绘制效果

| 能力 | 支持范围 | 限制和处理方式 |
|------|----------|----------------|
| 页面 Rotate | 0/90/180/270 度，90/270 度交换页面宽高。 | 常见文字、图片、裁剪和软掩码组合已有处理；复杂组合需按源文档验证。 |
| XObject `Do` | Image 和 Form，包含 Form 的 Matrix、Resources 和隐式 BBox 裁剪。 | 其他子类型报告诊断；Form 嵌套受 MaxDepth 限制。 |
| 线条样式 | `w/d/J/j/M` 及 ExtGState 的 `LW/LC/LJ/ML`。 | 按 CTM 换算线宽和虚线；0 线宽近似为 0.1mm，不能保持所有输出分辨率下的单像素语义。 |
| 开放路径的透明填充和描边 | 保留 `B/B*` 的开放描边；半透明描边、非 Normal 混合或软掩码下，先合并填充与描边再应用外层效果。 | 以 300dpi 局部合成，报告 `graphics.path_fill_stroke`，Strict 失败；普通不透明路径继续使用矢量输出。 |
| 内嵌字体 | TrueType、CFF 包装及实际字形映射；转换时将修复后的字体数据写入 OFD 包。CFF/OTF 中可安全清理的旧式 `ForceBoldThreshold`、`lenIV=-1` 私有字段会在写包前处理，避免导出 SVG 时被浏览器拒绝。 | 字体修复和特殊结构可能失败；创建器支持多 FD CFF 子集化，子集化错误直接返回；转回 PDF 时保留 OFD 中实际嵌入的 CFF 程序。 |
| 未嵌入字体 | 输出 OFD 逻辑字体，由阅读器替代。 | 字形和度量取决于可用字体；浏览器/Android 环境依赖嵌入或已注册的回退字体。 |
| 竖排替代字形 | 内嵌字体保留指定字形；替代字体尝试 `vrt2/vert` 和 Unicode 竖排形式，文字层保留原始标点。 | 支持 SingleSubst 格式 1/2 与 Extension；缺字或缺少竖排轮廓会诊断，Strict 失败。 |
| Type3 字体 | 执行 `d0/d1`、CharProcs 中的路径、图片及 Form；保留 FontMatrix、资源作用域、颜色和文字层。 | 复杂内容沿用内容流解释器的限制；Tr=3/7 不绘制，Tr=4–7 不增加字形裁剪。 |
| 渐变/图案填充文字 | 按文字位置取样并保留文字。 | PDF 导入时近似为纯色，不保留该文字的原始渐变填充。 |
| 轴向/径向着色 | ShadingType 2/3 转为 OFD 渐变。 | 函数按 32 段采样，不连续点和非线性变化可能被平滑。 |
| 网格着色 | ShadingType 4/5/6/7 转为 OFD 矢量网格。 | 复杂度超限或无法还原裁剪时回退为带透明度的位图。 |
| Pattern | PatternType 1 平铺图案、PatternType 2 着色图案；保留 Matrix、BBox 和颜色空间，BBox 作为图块裁剪区并保留填充透明度。 | 平铺图案展开为图片，图块过多时合成为密集图；不是原 PDF 图案对象的完整语义保留。 |
| 注解外观 | `/AP /N`，包括外观状态选择、BBox/Matrix 和 Rect 映射。 | 无独立外观的注解可能跳过；表单域和弹窗只保留可见外观。缺少有效 `/AS` 时，兼容选择依次为 On、Yes、Off，其余按状态名称字节序取首项，确保重复转换显示一致。 |

### 颜色空间与函数

| 能力 | 支持范围 | 限制和处理方式 |
|------|----------|----------------|
| ICCBased | 部分路径、文字及图片颜色转换。 | 配置文件和分量结构有限制；可能报告近似诊断。 |
| CalGray/CalRGB | 按对应设备颜色空间转换。 | 校准参数未完整应用，报告近似诊断。 |
| Indexed | 普通栅格图像调色板。 | JPX 的调色板和通道映射另有限制，见不支持表。 |
| Separation/DeviceN | 路径和文字的 tint 函数求值。 | 图像中的这两类颜色空间暂不支持；输出转换为 RGB。 |
| Type 0 函数 | 1/2/4/8/12/16 位采样，多线性插值。 | 特殊参数和组合仍受实现能力及资源预算限制。 |
| Type 2/3 函数 | 指数插值、拼接函数，Domain/Range 裁剪。 | 作为渐变使用时受分段采样精度限制。 |
| Type 4 函数 | PDF 规定的 42 个计算操作符、32 位整数、双精度实数、嵌套 if/ifelse 和 Domain/Range。 | 最多 64 层过程、1024 个栈操作数，单程序解析/单次求值各 100,000 步；实现上限、无效类型或算术等错误报告 `function.type4`。 |

### 图像解码

| 格式或能力 | 支持范围 | 限制和处理方式 |
|------------|----------|----------------|
| 普通栅格/Flate | DeviceGray/RGB/CMYK 和 Indexed；支持常见 1/2/4/8/16 位样本及 Decode。 | 转为 PNG；16 位颜色尽量保留，CMYK 转为 RGB。 |
| Separation 专色图片 | 普通栅格/Flate 的 1/2/4/8/16 位单通道采样，先应用 Decode，再经 tint 函数和替代颜色空间转换；包括亮度软掩码 Form 中的图片。 | 输出 8 位 RGB PNG；支持原始采样颜色键和图像 Mask/SMask。JPEG/JPX/JBIG2 专色图片暂不支持。 |
| DCTDecode/JPEG | Gray/RGB、四分量 CMYK/YCCK（包括 PDF 声明 CMYK 但 JPEG 无 Adobe 标记）；8 位原分量 Decode、颜色键和 Matte，支持外层 Flate 包装。 | 默认兼容模式下 CMYK 转 RGB PNG；`compact` 模式仅将无 Mask/SMask 的 CMYK JPEG 转为指定质量的有损 RGB JPEG。与码流冲突的 ColorTransform 覆盖暂不支持。 |
| CCITTFaxDecode | 解码后重编码为 PNG，也可用于 ImageMask。 | 沿用 PDF 解析器的解码能力。 |
| JBIG2Decode | 灰度图、JBIG2Globals、Decode 反相、ImageMask、灰度软掩码；颜色键和 Matte 使用二值原采样。 | 普通图像的任意 Decode 区间未完整支持；复合过滤器有限制。 |
| JPXDecode | JP2/裸码流；DeviceGray/RGB/CMYK、1/3/4 分量 PDF ICCBased 配置文件、同位深 1–16 位无符号样本、cdef 颜色重排和单个整体 opacity。 | 默认 Go 后端沿用原分量解码；8 位 RGB ICCBased JP2 使用完整质量解码路径。可选 OpenJPEG 后端应用 PDF Decode 数组，以兼容特定阅读器行为。高位深输出 16 位 PNG；复合过滤器、复杂通道、调色板及 JP2 码流内嵌 ICC 暂不支持。 |
| JP2 特殊颜色 | 三分量 sYCC/eYCC、YCbCr2/3、PhotoYCC、CMY 转为 RGB。 | 暂不能与 Matte、灰度掩码或内嵌 opacity 组合；部分源阅读器的转换行为不同。 |
| 内联图像 | `BI/ID/EI`，展开缩写键；支持当前资源中的颜色空间名称，未给 ColorSpace 时按 DeviceGray。无过滤器图像按每行补齐字节后的总长度读取，保留像素中的 ` EI ` 和末尾空白。 | 沿用普通图片的颜色空间、过滤器和预算限制；过滤器编码数据仍通过分隔符扫描 EI，存在二进制标记歧义。 |
| ImageMask | 1 位模板图像，按当前填充色绘制，支持 Decode 反相。 | 多位 ImageMask 不支持。 |

### 图像遮罩与透明合成

| 能力 | 支持范围 | 限制和处理方式 |
|------|----------|----------------|
| Mask 颜色键 | 在 Decode/颜色转换前匹配原始整数采样；普通栅格、Indexed 索引、JPX 原分量、8 位 JPEG 和 1 位 JBIG2。 | 无效或不支持的键报告诊断；普通模式保留基础图，Strict 失败。 |
| 显式 Mask | 独立的 1 位 ImageMask，含 Decode 反相。 | 尺寸不同按最近邻对齐；合成取双方各轴最大分辨率。 |
| 图像 SMask | 普通 1/2/4/8/16 位灰度掩码、JPX 1–16 位灰度掩码、JPEG/JBIG2 灰度掩码。 | 高位深颜色或 alpha 输出 16 位 PNG；不同尺寸按各轴最大分辨率合成。损坏掩码普通模式保留基础图，Strict 失败。 |
| Matte 去底色 | 普通栅格、JPEG、JBIG2、JPX 的受支持 DeviceGray/RGB/CMYK 底图和灰度掩码。 | 在原颜色空间还原颜色；要求底图与掩码同尺寸。校准/ICC Matte 和 JPX 内嵌透明度的 Matte 暂不支持。 |
| JPX 内嵌透明度 | SMaskInData=1 的非预乘及 =2 的预乘 alpha；=2 在原颜色分量中反预乘。 | 缺省/=0 时忽略额外 alpha。与外置 SMask 冲突时普通模式优先外置掩码并诊断，Strict 失败。 |
| 图像掩码优先级 | SMask 覆盖 Mask；选中的 JPX 内嵌 alpha 覆盖 Mask；图像关联软掩码覆盖 ExtGState 软掩码。 | 显式二值 Mask 不会关闭图形状态软掩码；整体 ca 和组透明度仍生效。 |
| 混合模式 | Normal/Compatible，以及 15 种标准非 Normal 模式；BM 数组选取首个受支持模式。 | 非 Normal 模式在实际背景上以 300dpi RGB 局部合成，报告 `graphics.blend`，Strict 失败。 |
| 透明组 | 隔离、非隔离、嵌套及路径/图片挖空组；保留组外透明度和文字层。 | 需要合成时使用局部 PNG，报告 `graphics.group`，Strict 失败；不完整保留原颜色空间、叠印、TK/AIS 语义。 |
| ExtGState 软掩码 | Alpha/Luminosity、矢量/图片/嵌套 Form、设置时的 CTM、BC、TR，以及 SMask None。 | 通常以 300dpi 局部合成，报告 `graphics.mask`，Strict 失败；部分恒黑单图片包装有兼容快速路径。 |

局部合成只覆盖图元、组边界与裁剪区的交集，并保留文字提取层。
RGB 栅格合成、CMYK 颜色转换、8 位量化、抗锯齿和图片放大插值可能与源 PDF
阅读器产生差异；高倍放大和印刷用途尤其需要检查源文档与转换结果。

## 暂不支持的能力

| 能力 | 当前处理 |
|------|----------|
| ShadingType 1、PatternType 1/2 之外的图案 | 未实现对应绘制，可能跳过或保留近似纯色，并报告已识别的缺口。 |
| 图像的 Lab、DeviceN，以及 JPEG/JPX/JBIG2 编码的 Separation | 跳过图像并报告诊断；Strict 失败。 |
| 完整 CalGray/CalRGB/Lab 校准语义 | 不完整应用校准参数；已有转换使用设备颜色近似或跳过。 |
| JPX 调色板、cmap、码流内嵌 ICC、有符号分量或混合位深 | 不支持相应结构，通常报告图像解码诊断并跳过图片；Strict 失败。PDF 图像字典引用的 ICCBased 配置文件不属于此限制。 |
| JPX 多 opacity、逐颜色 opacity | 不支持；通道关联不完整、重复或与 SMaskInData 冲突会报告诊断。 |
| 精确的原颜色空间混合及印刷叠印 | RGB 合成不能完整保留 CMYK/ICC 混合和叠印结果。 |
| 挖空文字 TK 的整体形状、完整 AIS 语义 | 当前采用近似，分别报告 `graphics.knockout_text`、`graphics.alpha_shape`。 |
| 图片 Interpolate 的独立语义 | OFD 无对应标志；PDF 显式设置 Interpolate 且源图不超过 64 像素时，导入阶段先做双线性预放大，避免极小图像在输出阅读器中呈现为明显色块。 |
| 竖排替代字体的上下文/多字形 GSUB | 未实现这类替换；简单 vert/vrt2 和竖排形式替代仍可用。 |
| 线条平滑度 `i` | 忽略，不保留该设备相关参数。 |
| 表单域、弹窗等交互 | 不保留交互语义，仅可能绘制已有外观。 |
| GoToR、JavaScript、Launch 等动作 | 不转换动作；仅支持 GoTo 和 URI。 |
| PDF 数字签名、附件、嵌入式 JavaScript/PostScript | 不迁移相关对象或语义。 |

## 资源预算与诊断

JPX 后端通过 `Options.JPXBackend` 选择，默认 `go`。`openjpeg` 后端仅在 Linux 上以
`CGO_ENABLED=1` 和 `-tags openjpeg` 构建时可用，并依赖系统 `libopenjp2` 开发文件和
`pkg-config`。CLI 对应参数为 `--pdf-jpx-backend openjpeg`。

### 默认预算

默认预算应用于所有 PDF 导入入口，也应用于 Office/HTML 转 OFD 的 PDF 阶段。
`Limits` 字段为 `int64`；`0` 使用默认值，负值和 `MaxInt64` 无效。

| 字段 | 默认值 | 限制范围 |
|------|--------|----------|
| MaxInputBytes | 512MiB | 原始输入。 |
| MaxPages | 10,000 | PDF 页数。 |
| MaxObjects | 1,000,000 | 对象数及 xref 可引用编号范围。 |
| MaxStreamBytes | 256MiB | 单流编码数据。 |
| MaxDecodedStreamBytes | 256MiB | 单流解压及页面内容流拼接。 |
| MaxTotalDecodedBytes | 1GiB | 转换中读取的解压流；同一流重复访问不重复计数。 |
| MaxImagePixels | 100,000,000 | 单图实际尺寸，包括掩码与合成栅格。 |
| MaxTotalImagePixels | 2,000,000,000 | 图片解码、掩码和合成的累计处理像素。 |
| MaxFontBytes | 64MiB | 单个内嵌字体程序。 |
| MaxOutputObjects | 1,000,000 | 合并前生成的文字、路径和图片图元。 |
| MaxOperations | 1,000,000,000 | 内容解析、文字/映射、遮罩、网格、函数等处理量。 |
| MaxDepth | 64 | 解析、状态栈、Form、软掩码和函数等递归深度。 |

CLI 可通过 `--pdf-max-total-decoded-bytes` 调整累计解码字节预算。字节参数支持 `B`、
十进制 `KB`/`MB`/`GB`/`TB` 和二进制 `KiB`/`MiB`/`GiB`/`TiB` 后缀；不带后缀时按字节数
解析，`0` 使用默认值。例如，4GiB 预算可写为 `--pdf-max-total-decoded-bytes 4GiB`。

这些预算限制对应的处理量，并不等同于进程峰值内存上限。

### 转换模式与失败处理

| 情况 | 普通模式 | Strict 模式 |
|------|----------|-------------|
| 已报告的内容缺失或近似 | 调用 OnDiagnostic 后，按该能力的规则继续、近似或跳过。 | 首条诊断后失败，错误满足 `converter.ErrPDFImportUnsupported`。 |
| 资源超限 | 终止转换，错误满足 `converter.ErrPDFImportLimitExceeded`。 | 同左。 |
| context 取消或超时 | 原样返回 `ctx.Err()`。 | 同左。 |
| 未检测或不迁移的 PDF 语义 | 按本清单的限制处理。 | 不保证触发诊断；Strict 不等同于完整的 PDF 一致性检查。 |

`OnDiagnostic` 在转换协程中同步调用，字段包括 Code、从 1 开始的 Page、Object、
Generation、Resource 和 Message；未知对象编号为 0。回调可以取消当前 context。

资源超限和 Strict 失败发生在写出之前，不写出部分 OFD。文件入口成功后才替换
目标文件，失败会清理临时文件并保留已有目标。

### 库配置示例

```go
var output bytes.Buffer
err := converter.Convert(ctx, "pdf", "ofd", input, &output,
    converter.WithPDFImportOptions(converter.PDFImportOptions{
        Limits: converter.PDFImportLimits{MaxInputBytes: 32 << 20},
        Strict: true,
        OnDiagnostic: func(d converter.PDFImportDiagnostic) {
            log.Printf("%s: page=%d object=%d/%d resource=%s: %s",
                d.Code, d.Page, d.Object, d.Generation, d.Resource, d.Message)
        },
    }))
```

`errors.As` 到 `*converter.PDFImportLimitError` 可取得 Resource、Limit 和 Actual。
Actual 是已观察到的用量；提前终止解码时可能只表示超过上限的最小已知值。

直接 PDF 导入入口为 `ConvertWithOptions(ctx, input, writer, password, options)` 和
`ConvertFileWithOptions(ctx, pdfPath, ofdPath, password, options)`。CLI 的预算选项和
`--pdf-strict` 见 [转换工具说明](../cmd/ofd-converter/README.md#pdf-转-ofd)。
