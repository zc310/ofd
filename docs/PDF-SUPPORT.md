# PDF 转 OFD 支持清单

本文档描述 `internal/pdf2ofd`（经 `pkg/converter/import/pdf` 暴露为
`converter.Convert("pdf", "ofd", ...)` 与 `ofd-converter` 的 PDF 输入）当前对
PDF 规范（ISO 32000-1/-2）的支持范围。主要实现方式：pdfcpu 解析 PDF 对象结构，
自研内容流解释器还原页面几何、文字、路径与图像，并经 `pkg/creator` 输出 OFD 包。

本文档描述的是项目当前的实现范围，不代表完整实现或通过任何一致性认证。真实 PDF
样本差异极大，使用前仍应以目标业务样本验证。

## 支持级别

| 图标 | 级别     | 含义                                                      |
|:----:|----------|-----------------------------------------------------------|
|  ✅  | 已支持   | 已有对应实现，并有测试或实际样例依据。                    |
|  ⚠️  | 部分支持 | 可以处理主要结构，但部分属性、组合场景或视觉效果有限制。  |
|  ❌  | 暂不支持 | 当前没有实现；相关对象通常被忽略，或该对象/页面转换失败。 |

## 页面与几何

| 标准范围                   | 状态 | 说明                                                                         |
|----------------------------|:----:|------------------------------------------------------------------------------|
| `MediaBox`                 |  ✅  | 缺少 `CropBox` 时回退到 `MediaBox`；页面缺 Box 时转换失败。                  |
| `CropBox` 与继承属性       |  ✅  | 依次取页面 `CropBox`、继承的 `CropBox`、继承的 `MediaBox`、页面 `MediaBox`。 |
| 非 0 原点的 Box            |  ✅  | 按左下角归一后平移页面坐标。                                                 |
| `Rotate`（0/90/180/270）   |  ⚠️  | 页面尺寸按 90/270 交换宽高；内容旋转仅对简单文字/图像组合验证过。            |
| `UserUnit`                 |  ✅  | 乘入页面尺寸、文字宽度和字号。                                               |
| PDF 坐标系（点，左下原点） |  ✅  | 按 1pt = 25.4/72mm 换算；OFD 使用页顶原点的毫米坐标。                        |

## 内容流操作符

| 操作符                      | 状态 | 说明                                                                                                                                                                                                                                                                                                                                                           |
|-----------------------------|:----:|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `q` / `Q`（图形状态栈）     |  ✅  |                                                                                                                                                                                                                                                                                                                                                                |
| `cm`                        |  ✅  | 右乘当前 CTM；对嵌套 Form XObject 的平移缩放做过专项修正。                                                                                                                                                                                                                                                                                                     |
| `w`（线宽）、`d`（虚线）、`J/j/M` | ⚠️ | 线宽按 CTM 缩放；最细线近似为 0.1mm；虚线数组按线宽换算。保留线帽、连接与斜接限制（OFD 以毫米计）；ExtGState 的 `LW/LC/LJ/ML` 共用相同处理。`i` 平滑度未实现。 |
| `rg/RG/g/G/k/K`             |  ✅  | DeviceRGB / DeviceGray / DeviceCMYK 转换为 RGB。                                                                                                                                                                                                                                                                                                               |
| `cs/CS` + `sc/scn/SC/SCN`   |  ⚠️  | 按当前颜色空间解释分量：DeviceGray/RGB/CMYK、`ICCBased`、`Indexed`、`Separation`、`DeviceN`（tint 变换求值）；`Pattern` 颜色空间按下面的图案规则填充。                                                                                                                                                                                                         |
| `W` / `W*`（裁剪）          |  ✅  | 暂存到下一次路径绘制（`n/S/f/B...`）时生效；`q/Q` 恢复状态不会泄漏裁剪。                                                                                                                                                                                                                                                                                       |
| `m/l/c/v/y/h/re`            |  ✅  | `re` 拆为四段并闭合；贝塞尔曲线按三次曲线输出。                                                                                                                                                                                                                                                                                                                |
| `S/s/f/F/f\*/B/B\*/b/b\*/n` |  ✅  | 支持描边、填充、描边加填充；填充规则 NonZero / Even-Odd。                                                                                                                                                                                                                                                                                                      |
| `Do`（XObject） | ⚠️ | 支持 Image 与 Form（含 `Matrix`、Form 自带 `Resources`），默认嵌套深度上限 64，可配置；其余子类型报告诊断。 |
| 内联图像 `BI/ID/EI`         |  ⚠️  | 缩写键展开；未给出 `ColorSpace` 时按规范默认 DeviceGray。                                                                                                                                                                                                                                                                                                      |
| `sh`（Shading）             |  ⚠️  | ShadingType 2/3 输出为 OFD `AxialShd`/`RadialShd`，函数按 32 段采样；ShadingType 4（自由）/5（规则）/6/7（补丁）网格着色输出为矢量 `GouraudShd`/`LaGouraudShd`（Type 5 保留每行顶点数，Type 7 按 Coons 曲面细分）；控制点超过上限或裁剪区无法还原为路径时回退为带透明度的位图。Type 1 忽略。                                                                   |
| Pattern 填充                |  ⚠️  | PatternType 1（平铺图案）展开为图片对象，图块过多时按最大图块数合成单张密集图；PatternType 2（图案着色）输出为渐变。图案自身的 `Matrix`、BBox 与颜色空间参与计算。                                                                                                                                                                                             |
| 透明组、`gs`（ExtGState）   |  ⚠️  | 读取 ExtGState 的 `ca`/`CA` 作为填充/描边不透明度并输出为 OFD 透明度；Form XObject 作为透明度组，`BM` 为 Normal 时 `Do` 时的外层 `ca`/`CA` 作为组透明度保留（不被 Form 内 `gs` 覆盖）。OFD 无混合模式：`/BM /Multiply` 的填充近似为半透明——纯色按白色背景还原原色并让文字透出，渐变（轴向/径向/网格）按每个色标独立套用同一近似、Alpha 落在色标上；其余 `/BM`（如 `Screen`、`HardLight`）忽略且不套用外层 `ca`/`CA`。`/SMask` 为 `/S /Luminosity` 且其 `/G` 最终解析到图像时，把该灰度图像作为 alpha 烘进随后绘制的图像；`/SMask /None` 清除软掩码。掩码由矢量内容（嵌套 Form）生成时无法还原为 alpha，仍然丢弃。`/Group` 不保留。 |

`Multiply` 透明组中只含一次轴向/径向 `sh` 绘制时，保留 Form 调用时的混合与不透明度，
以 300dpi 在已转换的背景上局部合成为带 alpha 的 PNG；组内 `/BM /Normal` 或 `ca=1`
不会清除组外混合状态。该路径修复了 `VectorApple.pdf` 标签的渐变遮住底色和字母的问题。
合成基于转换后的 sRGB 背景，仍属于近似，严格模式仍报告 `graphics.blend`；多次绘制、
其他混合模式和一般透明组尚未完整支持。背景重绘、栅格缓冲和合成计入资源预算。

## 文本

| 标准范围                                     | 状态 | 说明                                                                                              |
|----------------------------------------------|:----:|---------------------------------------------------------------------------------------------------|
| `BT/ET`、`Tf`、`Tm`、`Td/TD/TL/T\*` | ✅ | 保留文字矩阵与图形矩阵的完整仿射变换（旋转、错切、镜像、非均匀缩放），重算 `Boundary` 并补偿平移；`q/Q` 恢复文字参数，保留文本矩阵的推进。 |
| `Tc`（字距）、`Tw`（词距）、`Tz`（水平缩放） |  ✅  | `Tz` 同时输出为 OFD `HScale`（`hScale/100`）并计入推进量（`DeltaX`）；`Tc`/`Tw` 计入宽度与 `DeltaX`。 |
| `Tj/TJ/'/"`                                  |  ✅  | `TJ` 的数值偏移折算为字间推进；纯空白输出只推进文本矩阵，避免后续文字左移。                       |
| `Tr`（渲染模式）                             |  ⚠️  | 0/2/4/6 按填充、1/2/5/6 按描边；填充加描边同时输出颜色。                                          |
| `Ts`（文字升降） | ✅ | 沿文字空间 y 轴改变基线，不改变后续推进。 |
| Type3 `d0/d1` 与 CharProcs | ❌ | 字形绘制尚未实现；报告诊断，并通过 `/Encoding` 尽量保留文字层。 |
| 相邻单字文字对象                             |  ✅  | 转换后合并相邻同样式单字对象（同字体/字号/基线/颜色/`HScale`、水平、无 CTM/Clips/Actions），逐字定位不变。 |
| 字体样式（`Weight`/`Italic`）                |  ✅  | 依据字体描述符与族名判定粗体/斜体并写入文字对象，避免渲染器对内嵌字体再次做伪斜体处理。           |
| 渐变填充的文字（`sh` 或图案着色填充）         |  ⚠️  | 文字使用渐变/图案着色填充时，按文字位置取样为 RGB 并写入 `FillColor`（保留近似纯色），不输出 OFD 渐变。 |

### 文字编码与字宽

| 标准范围                                          | 状态 | 说明                                                                                                                                                                                  |
|---------------------------------------------------|:----:|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `ToUnicode` CMap（`bfchar`/`bfrange`）            |  ✅  | 支持条目计数前缀、数组目标与增量 range。                                                                                                                                              |
| 简单字体 `/Encoding`（`/Differences` + 基础编码） |  ✅  | 字形名经 Unicode 映射还原；基础编码覆盖 WinAnsi、MacRoman、MacExpert、Symbol、ZapfDingbats、AdobeStandard。                                                                           |
| 预定义 CJK CMap                                   |  ⚠️  | GBK-EUC（含 V/2K/p）、ETen-B5、CNS-EUC、KSC-EUC/UHC、`Uni*` UCS2/UTF16；变长编码按首字节切分。全角 CMap（`Uni*` UCS2/UTF16）非 ASCII 字形回退为 1000/1000 字宽，避免缺 `DW`/`W` 时推进偏窄。 |
| `Widths` + `FirstChar`、CID `/W`、`DW` | ✅ | CID 字宽用于内嵌字体 hmtx 缺失或不一致时的 `DeltaX`（阈值 0.5/1000）；字号与本地增量按组合矩阵 y 轴长度换算，余下变换保留在文字 CTM 中。 |
| 内嵌字体（TrueType/CFF 包装）                     |  ⚠️  | 保留嵌入数据（`PreserveEmbeddedFonts`）；字形映射走 `CIDToGIDMap` 或字体 cmap，`CGTransform` 输出实际字形。CFF 包装后按 `FontMatrix` 修正 `head.unitsPerEm`，避免 1/2048 字体被放大。 |
| 不连续 CID / 子集字体                             |  ✅  | 经 fontfix 私有区映射（F0000+CID）判定字形存在，避免误丢整段文字。                                                                                                                    |
| 字体族名（含 `#XX` 十六进制转义）                 |  ✅  | 按原始字节再解释为 UTF-8，非法转义时原样保留。                                                                                                                                        |
| 未嵌入字体                                        |  ⚠️  | 输出为 OFD 逻辑字体，由阅读器按族名回退本机字体。                                                                                                                                     |

## 图像

| 标准范围                       | 状态 | 说明                                                                                                                                                                            |
|--------------------------------|:----:|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `DCTDecode`（JPEG） | ⚠️ | 缺省/恒等 Decode 的 Gray/RGB JPEG 保留原字节；8 位 Gray/RGB/CMYK 的非恒等 Decode 在原分量中映射后输出 PNG。DeviceCMYK JPEG（含 Adobe APP14 反相约定）转为 RGB PNG；支持 Adobe CMYK/YCCK 与外层 Flate 包装。 |
| `FlateDecode` 解码后的栅格数据 |  ⚠️  | 8 位 DeviceGray/DeviceRGB、DeviceCMYK（油墨值）与 Indexed 调色板图像重编码为 PNG。                                                                                              |
| `ImageMask`（1 位蒙版）        |  ⚠️  | 0 采样按当前填充色着色；仅支持 1 位蒙版。                                                                                                                                       |
| `Decode` 数组（反相） | ⚠️ | 普通栅格、8 位 JPEG、Matte 与颜色键路径保留 Decode 区间及反相；普通 JPX 按规范忽略 Decode。普通 JBIG2 图像目前只保留 Decode 反相，任意区间由 Matte/颜色键路径支持。 |
| 图像仿射变换（`cm`） | ✅ | 从页面四角恢复完整图片 `CTM`，保留旋转、错切、镜像，并结合页面 Rotate、CropBox 原点和 UserUnit。 |
| `/Mask`（颜色键或显式遮罩） | ⚠️ | 颜色键按 `/Decode` 与颜色转换之前的原始整数采样值匹配闭区间，支持普通栅格数据的 1/2/4/8/16 位 DeviceGray/RGB/CMYK 与 Indexed（按索引匹配）、JPX 的 1–16 位 DeviceGray/RGB/CMYK 原分量、8 位 JPEG Gray/RGB/CMYK 及 1 位 JBIG2 Gray。显式遮罩支持独立的 1 位 `ImageMask`，默认 0 绘制、1 透明，`Decode [1 0]` 反相；不同尺寸按最近邻对齐，以两者各轴最大分辨率合成 PNG，16 位基础图保留颜色精度。`SMask` 优先于 `Mask`；损坏或不支持的遮罩报告诊断，普通模式保留基础图、严格模式失败。 |
| 颜色空间 | ⚠️ | 图像支持 DeviceGray/CalGray、DeviceRGB/CalRGB、DeviceCMYK、Indexed 与部分 ICCBased；CalGray/CalRGB 的校准参数报告近似诊断。Lab、Separation/DeviceN 图像目前跳过并报告；路径/文字的 Separation/DeviceN 支持 Type 0/2/3/4 tint 函数。 |
| tint 与着色函数 | ⚠️ | Type 0 采样（8/16/1/2/4/12 位，多线性插值）、Type 2 指数插值、Type 3 拼接；Type 4 支持 PDF 规定的 42 个计算操作符（包括 `true/false`）、嵌套 `if/ifelse`、整数/实数类型及 `Domain/Range` 裁剪，用于 Separation/DeviceN 与渐变/网格着色函数。轴向/径向渐变仍按 32 段采样，函数不连续点可能被分段插值平滑。 |
| `CCITTFaxDecode`               |  ✅  | 由 pdfcpu 解码后重编码为 PNG（扫描件 ImageMask 常见）。                                                                                                                         |
| `JBIG2Decode`                  |  ✅  | 由 `github.com/dkrisman/gobig2` 解码为灰度位图；支持 `/JBIG2Globals`、`/Decode` 反相，`ImageMask` 按填充色着色，也可作为 `/SMask`。                                             |
| `JPXDecode` | ⚠️ | 由 `github.com/mrjoshuak/go-jpeg2000` 的原整数分量接口解码；支持 JP2/裸码流的 DeviceGray/RGB/CMYK、同位深 1–16 位无符号样本、灰度软掩码，以及 `SMaskInData=1/2` 的内嵌透明度。普通图像忽略 PDF `Decode` 和字典位深；ImageMask 保留二值 Decode 反相。超过 8 位输出 16 位 PNG；CMYK 沿用现有颜色转换近似。复杂颜色空间与通道结构尚未支持。 |
| `SMask`（软蒙版） | ⚠️ | 图像自身 `/SMask`：1/2/4/8/16 位普通灰度样本、1–16 位 JPX 原灰度分量及 JPEG/JBIG2 灰度掩码作为 PNG alpha 应用到图像。底图或掩码高于 8 位时使用 16 位非预乘 PNG。普通栅格的 `Decode` 区间、反相及间接数值保留；JPX 的 Decode 被忽略。ExtGState 图形状态 `/SMask`：仅 `/S /Luminosity` 支持，掩码的 `/G` 需能解析到图像流（直接是图像，或仅调用一次 `Do` 绘制图像的 Form XObject）；矢量掩码与一般透明组尚未支持。 |
| 透明度                         |  ⚠️  | 图像、路径、文字的 `ca`/`CA` 转换为 OFD 透明度（0-255）；不实现混合模式与透明组。                                                                                               |

图片的 `/Interpolate` 尚未单独保留；遮罩合成阶段使用最近邻，生成 PNG 在阅读器放大
显示时仍按阅读器的图片插值规则处理，极低分辨率样本的二值透明边缘可能显示为平滑过渡。

JPX 颜色键支持 JP2 与裸码流，按实际码流位深及尺寸读取原分量，忽略 PDF
`Decode` 和字典位深。先按 `cdef` 确定逻辑颜色顺序，再匹配全部颜色分量的闭区间；
比较发生在 CMYK/sYCC 等颜色转换前，不比较 PNG 的 RGB，也不先降到 8 位。
有损逆变换的分量先舍入并截断到有效整数范围；额外原分量解码及 PNG 合成均计入
图片与处理量预算，逐行检查取消，重复绘制缓存结果。外置 SMask 或非零
`SMaskInData` 覆盖颜色键，忽略的键不校验、不解码。无效区间报告
`image.mask_explicit`，普通模式保留基础图，严格模式失败。

JPEG/JBIG2 颜色键共用 Matte 的原采样读取。JPEG 在 JPEG 颜色变换之后、PDF
`Decode` 之前匹配全部分量的闭区间；Adobe CMYK/YCCK 先撤销 Go 解码器的反相，
不能按油墨值或已转换的 RGB 匹配。支持 8 位 Gray/RGB/CMYK、外层 Flate 包装，
实际尺寸和分量数须与声明一致；与码流变换冲突的 `ColorTransform` 覆盖报告缺口。
JBIG2 匹配二值采样 0/1，支持现有全局段读取和受控解压，实际尺寸须与声明一致。
匹配后在原颜色空间应用 `Decode`，再输出带二值 alpha 的 PNG；间接键数组及端点
保留。额外原采样解码和 PNG 合成分别计入图片/处理量预算，逐行检查取消，重复
绘制缓存结果。外置 SMask 覆盖颜色键；无效键或不支持结构报告
`image.mask_explicit`，普通模式保留基础图，严格模式失败。

软掩码合成取基础图与掩码各轴的最大分辨率，分别按最近邻对齐，避免高分辨率
掩码被压到底图尺寸而丢失细线、局部透明及渐变。普通、JPEG/JBIG2 和 JPX
掩码共用这一合成尺寸规则，图像自身与受支持的图形状态软掩码均适用。JPEG/JBIG2
掩码沿用现有的 8 位灰度解码路径。JPX 按码流实际尺寸和位深读取原灰度分量；超过 8 位时
保留完整 16 位 alpha，相邻样本不会先被合并成同一个 8 位值。图像自身 `/SMask` 的 `/Matte` 支持普通栅格及 8 位 JPEG 的
DeviceGray/RGB/CMYK：先应用基础图和掩码的 `Decode`，在原颜色空间还原预混合
分量，再转换为 RGB 并写入 alpha；保留 8/16 位输出精度，alpha 为 0 时颜色置零。
JPEG 底图与 JPEG 灰度掩码均支持 Matte；Adobe CMYK/YCCK 先撤销 Go 解码反相，
再应用 PDF `Decode`，在 CMYK 分量中去底色。JPEG 实际尺寸须与声明一致；支持
外层 Flate 包装，额外 JPEG 解码计入预算。与码流变换冲突的 `ColorTransform` 覆盖
暂未支持。Matte 也支持 1 位 DeviceGray JBIG2 底图和 JBIG2 灰度掩码；掩码可搭配
普通栅格或 JPEG 的 DeviceGray/RGB/CMYK 底图。先在原灰度样本上应用双方 `Decode`，
掩码的部分透明度端点保留为 16 位直到去底色完成，再按底图精度写入 8/16 位 PNG。
JBIG2 实际尺寸须与声明一致；支持已有的 `JBIG2Globals` 全局段读取与受控解压，
额外解码计入像素和处理量预算，解码器在段间检查取消并原样返回 context 错误。
JBIG2 暂只支持单一过滤器。Matte 也支持 JP2 与裸 JPEG 2000 码流的 DeviceGray/RGB/CMYK
底图及 JPX 灰度掩码：从整数样本平面还原颜色，避免先经 JP2 的 CMYK→RGB 转换或
把第四个颜色分量误当作 alpha。支持同位深的 1–16 位无符号分量；按码流位深归一化，
超过 8 位时输出 16 位 PNG，并保留 JPX 掩码的透明度精度。PDF 规范规定 JPX 的
`BitsPerComponent` 和普通图像的 `Decode` 被忽略；普通与 Matte 路径均遵守此规则，
只有 1 位 ImageMask/显式遮罩保留 Decode 反相。未指定 PDF `ColorSpace` 时，可由 JP2 的灰度/RGB/CMYK
枚举颜色信息推导；裸码流仍需显式指定颜色空间。Matte 路径的实际尺寸必须与声明一致，额外解码
计入像素与处理量预算，JP2 盒扫描也受处理量预算约束。JPX 暂只支持单一过滤器；
调色板、`cmap` 通道映射、ICC、额外颜色变换、有符号或混合位深分量尚未支持；
支持 `cdef` 通过 Association 指定逻辑颜色顺序，颜色分量可重排，单个作用于全部
颜色的 opacity 可以位于任意平面。条目的排列次序不影响映射。先按通道定义调整
采样平面引用，再做 Matte、反预乘及颜色转换，不额外复制逐像素数据。
重复、缺失、颜色关联不完整或与 `SMaskInData` 不一致的通道定义报告诊断。
逐颜色 opacity、多 opacity 尚未支持；普通三分量 JP2 支持颜色重排后再做
sYCC、eYCC、YCbCr2/3、PhotoYCC 和 CMY 转换。
Matte 路径仍不支持 `SMaskInData` 内嵌透明度。JPX 解码前后及逐行合成检查取消，
解码库内部没有 context 接口。
带 Matte 的掩码必须与基础图同尺寸；校准或 ICC 颜色空间的 Matte 暂未支持。
无效或不支持的 Matte 报告 `image.mask_matte`，普通模式保留基础图，
严格模式失败。掩码采样、Matte 逐分量还原与逐像素合成
计入 `MaxOperations`，解码、合成及缓存遵循图片预算；逐行检查取消。损坏掩码在
普通模式报告并保留基础图，严格模式失败。

普通 JPX 与 Matte 共用原分量解码和结构校验。普通 JPX 支持 `SMaskInData=1` 的
非预乘透明度及 `SMaskInData=2` 的预乘透明度；后者先在原颜色分量中反预乘，再
转换为 RGB 并写入 PNG。`SMaskInData` 缺省或为 0 时忽略码流额外 alpha，保持
不透明。`SMaskInData=1/2` 且没有外置 `SMask` 时，使用内嵌透明度并忽略 `Mask`。
非零 `SMaskInData` 本不应与 `SMask` 同时出现；对此冲突，普通模式按已验证的
MuPDF/Poppler 行为优先应用外置 `SMask`，并报告 `image.mask_priority`，严格模式
拒绝兼容转换。外置 alpha 替换内嵌 alpha，不相乘；选中的遮罩计入解码/图片/处理量
预算，重复绘制缓存结果并只报告一次。损坏的外置 SMask 报告诊断并保留内嵌透明度，
预算超限始终终止转换。缺省或为 0 时仍按 `SMask` 优先于 `Mask` 的规则选择。
被忽略的显式 `Mask` 不解码，也不扣除其图片预算。普通图像的解码和输出写入计入处理量
预算，基础解码只计一次像素，重复绘制命中缓存。尚未支持的 JPX 调色板、`cmap`、
ICC、其余颜色变换、有符号或混合位深结构在普通模式报告 `image.decode` 并跳过
该图片，其余页面内容保留；严格模式终止。损坏或不支持的 JPX 软掩码报告 `image.mask`
并按既有规则保留底图或严格失败。
普通三分量 JP2 从原样本读取 sYCC/eYCC、YCbCr2/3、PhotoYCC 和 CMY：先应用
`cdef`，再转换为 RGB。sYCC 修正为 BT.601 矩阵，避免解码库原 BT.709 系数造成
非中性色偏差；色度中心按码流位深计算，直到输出 PNG 才量化，高于 8 位时保留
16 位输出。eYCC 按 OpenJPEG `color_esycc_to_rgb` 的无符号样本逆矩阵转换，
修正原先复用 BT.709 简化式造成的非中性色偏差；YCbCr2/3、PhotoYCC、CMY
沿用既有转换语义。
颜色转换逐分量计入处理量预算，像素预算只计一次解码，重复绘制命中缓存。
这些颜色变换尚不能用于 Matte、灰度掩码或内嵌 opacity 组合。

图像关联的软掩码（外置 `SMask` 或 JPX 内嵌 opacity）覆盖当前 ExtGState 的
图形状态软掩码；不会再用图形状态掩码替换已经合成的 alpha。`SMask /None` 或
只有显式二值 `Mask` 时，图形状态软掩码仍适用；整体 `ca`、组透明度继续保留。
缓存、非缓存与内联图像共用这项绘制选择，选择只作用于当前图像，不修改 q/Q
保存的图形状态或缓存媒体。ExtGState 的 Form 解析仍在设置 gs 时执行并计入流/
处理量预算；被图像覆盖的图形状态掩码不执行额外像素解码及合成。

Type 4 使用有符号 32 位整数和双精度实数；解析 token、执行指令与 `copy/roll` 的
处理量计入 `MaxOperations`，过程嵌套受 `MaxDepth` 约束。当前实现另有 64 层过程、
1024 个栈操作数以及单程序解析/单次求值各 100,000 个处理步骤的上限；超出实现能力、
无效类型、栈不足或算术错误会报告 `function.type4`。普通模式求值失败时保留原颜色
或跳过相应着色，严格模式终止；配置预算超限和取消始终终止转换。

## 注释

| 标准范围                     | 状态 | 说明                                                                                                                                       |
|------------------------------|:----:|--------------------------------------------------------------------------------------------------------------------------------------------|
| 注释外观（`Annots` + `/AP`） |  ⚠️  | 渲染 `/AP /N` 外观流（含外观状态字典，按 `/AS` 选择），应用 BBox/Matrix 与注解 `Rect` 映射；Popup/Link/Widget 无独立外观或无外观流时跳过。 |
| 链接（`/Subtype /Link`）     |  ✅  | 转为 OFD `Link` 注解：`/Dest` 与 `/A` 的 `GoTo`、`URI` 保留，其余动作类型丢弃；矩形按页面几何与旋转换算并裁到版心。 |
| 其余交互（表单域、弹窗）     |  ❌  | 不输出交互动作，仅保留可见外观。                                                                                                           |

## 大纲与书签

| 标准范围               | 状态 | 说明                                                                                                                                                                                                                                                                                                            |
|------------------------|:----:|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| 目录大纲（`Outlines`） |  ✅  | 递归转换 `/Outlines` 的 `First`/`Next` 大纲树为 OFD 大纲，保留标题、层级与 `Count`/展开状态；`/Dest` 或 `/A /S /GoTo` 的页面目标按 `XYZ`/`Fit`/`FitH`/`FitV`/`FitR` 换算为毫米坐标（`FitB*` 近似为 `Fit*`），命名目标（目录 `/Dests` 字典与名称树）同样解析；目录 `/PageMode /UseOutlines` 时同步写出显示偏好。 |
| 大纲动作               |  ⚠️  | `/A /S /URI` 输出为 OFD URI 动作；其他动作（GoToR、JavaScript、Launch 等）忽略。                                                                                                                                                                                                                                |

## 包结构与容错

| 标准范围                   | 状态 | 说明                                                                                   |
|----------------------------|:----:|----------------------------------------------------------------------------------------|
| 输入形式                   |  ✅  | 文件路径、`[]byte`、`io.Reader`（经 `converter.Convert` 或 `pdf2ofd.Convert`）。       |
| 经典 xref / 对象流         |  ✅  | pdfcpu 解析；对象流（compressed object streams）由 pdfcpu 展开。                       |
| xref 损坏或 free-list 异常 |  ✅  | 自动重建经典 xref（`repairPDFXRef`）后重试。                                           |
| 解析异常（panic）          |  ✅  | recover 并在修复 xref 后重试一次。                                                     |
| 损坏的 `Info` 日期         |  ✅  | 忽略 Info 引用重新校验，保留已提取元数据。                                             |
| 文档元数据                 |  ✅  | `Title/Author/Subject` 映射到 `DocInfo`；`Creator=zc310/ofd`、`CreatorVersion` 为当前构建版本（见根目录 `Makefile` 的 `VERSION`）。 |
| 溯源信息                   |  ✅  | `CustomData SourceFormat=PDF`；PDF `Producer` 保留在 `CustomData SourceProducer`。     |

## 暂不支持的能力

| 标准范围                                     | 行为                                                          |
|----------------------------------------------|---------------------------------------------------------------|
| 链接                                 | 转为 OFD `Link` 注解（`/Dest` 与 `/A` 的 `GoTo`/`URI`），热区挂在不可见路径上；页内跳转按目标页几何换算坐标，旋转与越界矩形一并处理 |
| 表单域、弹窗等其余交互注解            | 忽略（仅保留 `/AP` 外观）                                     |
| ShadingType 1、PatternType 之外的着色        | 忽略（回退为纯色或不填充）                                    |
| 混合模式（`Multiply` 之外的 `/BM`）、非 Luminosity 的图形状态 `/SMask`、透明组（`/Group`） | 忽略（`ca`/`CA` 透明度保留；`Multiply` 纯色与渐变填充近似为半透明；图像与 Luminosity 图形状态软蒙版在可图像化时保留） |
| 数字签名、附件、嵌入式 JavaScript/PostScript | 忽略                                                          |
| 大纲与链接的其他动作（GoToR、JavaScript、Launch 等） | 忽略（仅保留 `GoTo` 与 URI）                    |
| `Tr=4–7` 的字形裁剪 | 报告诊断；绘制部分保留，字形裁剪未实现 |
| 竖排 CMap 的 `/W2`、`/DW2` | 报告诊断；编码仍可还原，垂直推进未实现 |
| CalGray/CalRGB/Lab 校准参数 | 报告诊断，按设备颜色空间近似 |
| Type3 CharProcs | 报告诊断，字形绘制未实现 |

## 资源预算与诊断

默认预算应用于所有 PDF 导入入口，也应用于 Office/HTML 转 OFD 的 PDF 阶段。
`Limits` 字段为 `int64`；`0` 使用默认值，负值和 `MaxInt64` 无效。

| 字段 | 默认值 | 计数范围 |
|------|--------|----------|
| `MaxInputBytes` | 512MiB | 文件、字节切片与 Reader 输入 |
| `MaxPages` | 10,000 | PDF 页数 |
| `MaxObjects` | 1,000,000 | PDF 对象及 xref 条目范围 |
| `MaxStreamBytes` | 256MiB | 单流编码字节数 |
| `MaxDecodedStreamBytes` | 256MiB | 单流解压与页面内容流拼接 |
| `MaxTotalDecodedBytes` | 1GiB | 转换中读取的解压流，重复访问同一流不重复扣除 |
| `MaxImagePixels` | 100,000,000 | 声明尺寸及 JPEG/JPX/JBIG2 实际尺寸；掩码与合成图片同样检查 |
| `MaxTotalImagePixels` | 250,000,000 | 图片解码、掩码与合成；缓存命中不重复计算基础图片 |
| `MaxFontBytes` | 64MiB | 单内嵌字体程序，修复前检查 |
| `MaxOutputObjects` | 1,000,000 | 合并前生成的页面文字、路径与图片图元 |
| `MaxOperations` | 10,000,000 | 内容 token、文字解码、CMap/CID 字宽展开、图片遮罩、网格与 Type 4 解析/求值处理量 |
| `MaxDepth` | 64 | PDF 解析、内容容器、图形状态栈、Form、掩码及 Type 4 过程嵌套 |

预算限制的是相应处理量，并非进程峰值内存。pdfcpu 在读取/验证阶段可能提前解码
xref 或对象流；本项目随后将已有解码流计入累计预算。JBIG2 内部符号字典等分配也
受解码库自带预算约束，转换时不会修改进程级设置。

超限会终止转换，并可用 `errors.Is(err, converter.ErrPDFImportLimitExceeded)` 识别；
`errors.As` 到 `*converter.PDFImportLimitError` 可取得 `Resource`、`Limit` 和 `Actual`。
提前终止解码时 `Actual` 是超过预算的最小已知用量。

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

`OnDiagnostic` 在转换协程中同步调用。诊断包含 `Code`、从 1 开始的 `Page`、
`Object`、`Generation`、`Resource` 与说明；未知对象编号为 0。普通模式报告后继续，
`Strict` 在首条诊断后失败，错误满足 `converter.ErrPDFImportUnsupported`。
回调触发 context 取消时原样返回 `ctx.Err()`。

当前诊断覆盖图像/掩码解码、缺失资源、字体程序、字形裁剪、竖排文字、校准颜色、
ICC 近似、Type3、Type4 程序错误或实现上限、复杂平铺图案、不支持着色与混合模式等已识别缺口。
严格模式不保证所有 PDF 语义均被识别，例如未输出的附件、签名与交互仍属支持清单的限制。

直接 PDF 包入口为 `ConvertWithOptions(ctx, input, writer, password, options)` 与
`ConvertFileWithOptions(ctx, pdfPath, ofdPath, password, options)`；文件入口成功后替换输出。
资源超限与严格模式失败发生在写出之前，文件入口失败会清理临时文件并保留已有目标。

## 验证方式

```bash
go test ./internal/pdf2ofd -count=1
go test ./pkg/converter -run '^Example' -count=1
go run ./cmd/ofd-validator --format text --mode strict output.ofd
```

`internal/pdf2ofd/pdf_to_ofd_test.go` 覆盖页面几何、矩阵级联、文本推进与
`DeltaX`、`HScale`、文字旋转 `CTM`、CTM 缩放下的 CJK 毫米字号与推进、渐变填充文字、
CJK 解码、裁剪、内联图像、ImageMask、CMYK JPEG、文字对象合并与
元数据；`shading_test.go`、`pattern_test.go`、`colorspace_test.go`、`mesh_test.go`
覆盖着色/图案/颜色空间与网格着色；`dash_test.go`、`multiply_test.go` 覆盖线条样式与
混合模式近似；`image_jbig2_test.go`、`image_jpx_test.go` 覆盖 JBIG2/JPEG2000 解码；
`annotation_test.go`、`form_test.go`、`text_test.go` 分别覆盖注释外观、Form 矩阵与
字体样式标记；`outline_test.go` 覆盖大纲层级、`Dest` 坐标换算与命名目标；
`TestConvertTestdataPDFs` 会对 `testdata/pdf/` 下的固定样本做端到端转换校验。

`limits_test.go` 覆盖输入、解压、图片真实尺寸、掩码、字体、处理量、深度与原子输出；
`graphics_state_test.go`、`image_transform_test.go` 覆盖文字参数、路径样式与图片四角的渲染。
`image_mask_test.go` 覆盖原始采样颜色键、Indexed 索引、遮罩反相与尺寸对齐、
16 位颜色精度、`SMask` 优先级、遮罩缓存与预算、错误诊断及透明背景的实际渲染。
`function_type4_test.go` 覆盖计算操作符、分支、Domain/Range、DeviceN 多输入、
网格函数、Type 3 子函数、预算/取消/诊断及 Separation/轴向渐变的实际渲染。
`blend_group_test.go` 覆盖单次着色组的 Multiply 背景合成、组外不透明度、预算及
`VectorApple.pdf` 标签的颜色/字母对比；该样例文件缺失时只跳过样例视觉回归。
`image_softmask_test.go` 覆盖 16 位颜色/alpha 精度、灰度底图、Decode 区间与间接数值、
尺寸对齐、部分透明的实际渲染、Flate 掩码、预算/缓存、损坏掩码回退及合成中取消。
`image_matte_test.go` 覆盖白/黑 Matte 去底色、灰度/CMYK 原颜色空间还原、8/16 位
精度、Decode 顺序、透明/不透明端点、间接 Matte 数值、实际渲染及诊断/预算/缓存。
`image_matte_jpeg_test.go` 覆盖 JPEG 底图/掩码、灰度/RGB、独立生成的 Adobe CMYK/YCCK
样本、双方 Decode、16 位 alpha、Flate 包装、额外解码预算、缓存及实际尺寸/变换诊断。
`image_matte_jbig2_test.go` 覆盖独立构造的 JBIG2 黑/白采样、底图与掩码 Decode、
部分透明端点、16 位 alpha、JPEG/CMYK 输入组合、全局段解压与间接数值、实际渲染、
额外解码预算、缓存、损坏掩码回退以及段间取消。
`image_matte_jpx_test.go` 覆盖 JP2/裸码流的灰度/RGB/CMYK 原分量、1–16 位采样、
透明度精度、JPX Decode 忽略规则、缺省颜色空间推导、JPEG/JBIG2 输入组合、实际渲染、
额外解码与盒扫描预算、缓存及不支持结构的诊断/回退。
另内嵌 OpenJPEG 2.5.0 独立生成的 RGB 与 16 位灰度码流，逐点验证原分量，
避免仅靠同一 Go 编解码器的往返验证。直接 OFD PNG 与往返 PDF 一致，只证明输出
路径一致，不能证明与源 PDF 的视觉保真。测试 JP2 的颜色和 opacity 必须包含相应
通道定义；Go 编码器不会自动写入 `cdef`，四分量样例在外部阅读器中可能被误解。
源 PDF 应另用独立阅读器渲染对比；对 Decode、预乘/缺省内嵌透明度或高位深掩码的
处理存在阅读器差异时，须记录源渲染差异。DeviceCMYK 仍受各阅读器的颜色转换影响。
`image_jpx_precision_test.go` 覆盖普通 JP2/裸码流的 Decode 忽略规则、码流位深、
灰度/RGB/CMYK 颜色精度、变化的 16 位 RGB、16 位软掩码相邻 alpha 与尺寸对齐、
内嵌透明度及反预乘、缺省透明度忽略、二值遮罩反相、实际渲染、预算/缓存和诊断/取消。
另覆盖 `cdef` 的颜色/透明度声明、完整性、重复通道、关联及 opacity 类型不一致。
`image_jpx_channels_test.go` 覆盖 RGB/CMYK 的 8/16 位颜色重排、opacity 位于首个
平面、反预乘、缺省透明度忽略、Matte 顺序及条目次序无关；使用独立 OpenJPEG
码流验证颜色关联，并以源 PDF 的独立阅读器渲染作为视觉对照。
`image_jpx_colour_test.go` 覆盖重排后的 sYCC/YCbCr2/3/PhotoYCC/CMY/eYCC、
8/12/16 位精度、非中性及边界颜色的独立 BT.601 对照，以及处理量预算与缓存。
另内嵌独立 OpenJPEG 的重排 sYCC 码流；源 PDF 的 MuPDF 渲染与本实现最多相差
一个 8 位等级。在当前环境中，Poppler 对该 JP2 的 colr=18 未执行 sYCC→RGB，
直接显示 Y/Cb/Cr 值，因此源 PDF 的两个阅读器结果不同，不能声称与两者同时一致。
`image_jpx_eycc_test.go` 使用 OpenJPEG 单精度参考的固定数值，覆盖 8/12/16 位
无符号 eYCC、中性及非中性色、边界/超色域截断、重排、高位深低位保留及预算/缓存。
另内嵌独立 OpenJPEG 的 eYCC 码流验证实际解码。在当前环境中，MuPDF 和 Poppler
对 eYCC 的 colr=24 都未执行颜色矩阵转换，源 PDF 显示的是 Y/Cb/Cr 样本，
与本实现存在明显差异；视觉验收使用 OpenJPEG eYCC 固定参考值构造的等价 RGB PDF，
并明确保留两种源阅读器的对比结果，不声称与其未转换的显示结果一致。
`image_mask_priority_test.go` 覆盖无外置 SMask 时内嵌 opacity 对显式 Mask 的忽略、
损坏引用/压缩流及超预算尺寸、SMaskInData=0 的外置掩码保留、图像软掩码覆盖图形状态、
ca 保留、缓存/q/Q 隔离、非缓存路径及实际渲染。
`image_mask_conflict_test.go` 覆盖普通模式对冲突外置 SMask 的兼容选择、替换而非
相乘的 alpha、16 位颜色保留、缓存/诊断次数、选中遮罩预算、严格拒绝及取消。
视觉样例保留原始冲突条目；普通模式的黑色外置 SMask 与源 PDF 的 MuPDF/Poppler
均显示为纯蓝背景。图像 SMask 覆盖图形状态、无图像 SMask 时使用图形状态、Q 后
恢复不透明这三项也与两个源阅读器吻合。
`image_mask_jpx_test.go` 覆盖 JP2/裸码流颜色键的全部分量闭区间、1–16 位码流
位深、Decode 忽略、实际尺寸、相邻 16 位采样、CMYK/sYCC 转换前匹配、cdef 重排、
间接区间、软掩码优先级、预算/缓存、无效区间回退及合成中取消；另使用固定
OpenJPEG 16 位码流验证原采样。独立视觉样例的 8 位 RGB 与重排 RGB 色块内部
颜色与当前 Poppler 源渲染一致；当前 MuPDF 对这些颜色键的源渲染不同，两个阅读器
均未区分 16 位颜色键样例中的相邻采样。对比保留这些差异，并用显式 RGB 色块
参考 PDF 验证应透明区域的底色与应保留区域的颜色，不声称与所有源阅读器一致。
低码率 Part 1 MQ 码流的截断编码轮数已修复：按包头实际轮数停止解码，并在
每个有效系数的剩余区间中点还原幅值，避免把终止填充误解为未编码的低位细节。
项目通过本地 `replace` 使用 `../third_party/go-jpeg2000` 中的 fork 及基于
v1.5.12 的解码补丁；包解析、渐进解码和 TileDecoder 共用修复后的 MQ 路径，
完整编码块保持原结果。普通图片、颜色键、软掩码及 Matte 共用修复后的原分量。
灰度/RGB、8/16 位、1/3 层分辨率与五档码率的 40 个独立 OpenJPEG 组合，整数
输出逐点一致；这项验证不代表所有 JPX 编码结构或所有阅读器显示均已一致。
补丁来源和维护方式见该目录的 `OFD-PATCH.md`，原 MIT 许可保留。

`image_jpx_lowrate_test.go` 固化五组独立 OpenJPEG 码流及逐点参考值，覆盖 JP2/
裸码流、截断 MQ、灰度/RGB、多层小波、8/16 位及全空编码块。另保护低码率颜色键、
软掩码和 Matte，防止不同路径继续使用错误低位。fork 依赖中的回归覆盖完整/
截断 MQ 块的解码、空轮数和实际包头轮数。
低码率页面样例分别用 MuPDF、Poppler 渲染源 PDF，并保留优化前/后 OFD 与往返
PDF。普通灰度、RGB 和软掩码在中心采样处与 MuPDF 最多相差两个 8 位等级，
原 RGB 偏差最高达 110；颜色键与 Poppler 的匹配区域一致，MuPDF 仍未应用该
样例的颜色键。放大时 Poppler 的最近邻和 OFD/MuPDF 的平滑插值不同；原分量
回归逐点比较原尺寸整数值，不用放大插值结果替代码流解码验收。
本样例颜色键的透明边缘在 OFD 与往返 PDF 中心采样处最多相差 8 个等级，属于
已有图片插值差异；遮罩原尺寸的 alpha 回归逐点一致，不把该边缘差异记为通过。

`image_softmask_resolution_test.go` 覆盖单像素底图与高分辨率软掩码、交叉长宽的
各轴保留、4/8/16 位 alpha、相邻 16 位值、非零底图原点、Flate/JPEG/JPX/JBIG2
掩码及图形状态掩码。合成新增像素计入累计预算，交叉长宽生成的更大栅格须通过
单图/平台上限检查；处理量按输出像素预留，逐行检查取消，重复图像绘制缓存合成
结果。Matte 路径仍要求掩码与底图同尺寸。
独立源 PDF 的透明条纹、细线和普通四级 alpha 的内部采样与 MuPDF/Poppler
对照；高分辨率 16 位 JPX SMask 与 MuPDF 对照。当前 Poppler 对该 JPX 掩码
源显示存在布局异常，保留其源图结果，不声称与之相同。图片插值规则仍由阅读器
决定，本项修复保留原始遮罩细节，不完整实现 `/Interpolate`。
本样例内部采样与 MuPDF 最多相差一个 8 位等级，前三项与 Poppler 最多相差
两个等级；直接 OFD 与往返 PDF 的对应采样最多相差一个等级。遮罩坐标乘法使用
64 位整数，避免 32 位/wasm 平台上长条图片在最近邻对齐时溢出。
坐标函数及其回归使用真实源码独立通过 `GOARCH=386 go test`；整包的 386
验证被 `go-xml/xdm` 中既有的年份常量超出 int 范围所阻塞，不代表整项目已通过
32 位编译。

`image_mask_compressed_test.go` 覆盖 JPEG Gray/RGB、Adobe CMYK/YCCK 的原分量
匹配、Decode 顺序、全部分量闭区间、JBIG2 0/1 极性、Flate 包装、间接键及压缩
全局段、预算/缓存、SMask 优先级、实际尺寸/ColorTransform 诊断、回退及取消。
另独立构造 MMR 黑白混合 JBIG2 码流，验证匹配区域而非只验证常量图片。
源 PDF 的五组内部色块采样在 MuPDF、Poppler、OFD 及往返 PDF 中一致，支持
透明背景、JPEG/JBIG2 Decode 反相及 Adobe CMYK 键的匹配/不匹配。
JPEG 色度上采样及图片放大插值仍有阅读器差异；样例 RGB 白/红交界在源阅读器
中带窄色带，而 OFD 的透明过渡不同，不把交界差异记为视觉一致。CMYK 色彩转换
继续沿用已有近似。

普通 8 位 JPEG 的显式非恒等 `Decode` 在原分量中映射，再转换为 RGB PNG，
支持灰度反相/区间、RGB 各分量不同区间、Adobe CMYK/YCCK。端点可在 [0,1]
之外，映射后截断到有效颜色范围；间接 Decode 数组、端点、位深和尺寸可解析。
无效数组/非有限端点、不支持结构、实际尺寸不匹配或冲突 ColorTransform 在该
路径报告 `image.decode`，普通模式跳过图像，严格模式失败。源字典不改写。
缺省/恒等 Gray/RGB Decode 保留原 JPEG 字节；缺省/恒等 CMYK 沿用已有转换。
首次原分量解码已由预检计入像素，PNG 合成另计输出像素及处理量，逐行检查取消，
重复绘制缓存结果。Matte、颜色键和显式/软掩码与此路径组合保持原颜色空间顺序。

`image_jpeg_decode_test.go` 覆盖普通 JPEG 的灰度反相/区间与截断、RGB 分量区间、
独立构造的 Adobe CMYK/YCCK、恒等原字节保留、Flate 与间接数值、预算/缓存、
遮罩组合、有限端点/尺寸/ColorTransform 校验、严格诊断及取消。独立源 PDF 的
灰度/RGB/Flate 色块内部与 MuPDF 最多相差一个 8 位等级，与 Poppler 一致；
独立 ImageMagick YCCK 样本的对应采样与 Poppler 一致。当前 MuPDF 将 K=1
显示为 (0,0,0)，Poppler 和既有 CMYK 近似为 (35,31,32)；保留此色彩转换差异，
不声称与两个阅读器同时一致。放大交界仍受图片插值影响。
