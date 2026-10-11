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
| `DCTDecode`（JPEG）            |  ⚠️  | JPEG 数据直接嵌入 OFD；DeviceCMYK JPEG（含 Adobe APP14 反相约定）转为 RGB PNG。                                                                                                 |
| `FlateDecode` 解码后的栅格数据 |  ⚠️  | 8 位 DeviceGray/DeviceRGB、DeviceCMYK（油墨值）与 Indexed 调色板图像重编码为 PNG。                                                                                              |
| `ImageMask`（1 位蒙版）        |  ⚠️  | 0 采样按当前填充色着色；仅支持 1 位蒙版。                                                                                                                                       |
| `Decode` 数组（反相）          |  ✅  |                                                                                                                                                                                 |
| 图像仿射变换（`cm`） | ✅ | 从页面四角恢复完整图片 `CTM`，保留旋转、错切、镜像，并结合页面 Rotate、CropBox 原点和 UserUnit。 |
| `/Mask`（颜色键或显式遮罩） | ⚠️ | 颜色键按 `/Decode` 与颜色转换之前的原始整数采样值匹配闭区间，支持普通栅格数据的 1/2/4/8/16 位 DeviceGray/RGB/CMYK 与 Indexed（按索引匹配）；JPEG/JPX/JBIG2 颜色键尚未支持。显式遮罩支持独立的 1 位 `ImageMask`，默认 0 绘制、1 透明，`Decode [1 0]` 反相；不同尺寸按最近邻对齐，以两者各轴最大分辨率合成 PNG，16 位基础图保留颜色精度。`SMask` 优先于 `Mask`；损坏或不支持的遮罩报告诊断，普通模式保留基础图、严格模式失败。 |
| 颜色空间 | ⚠️ | 图像支持 DeviceGray/CalGray、DeviceRGB/CalRGB、DeviceCMYK、Indexed 与部分 ICCBased；CalGray/CalRGB 的校准参数报告近似诊断。Lab、Separation/DeviceN 图像目前跳过并报告；路径/文字的 Separation/DeviceN 支持 Type 0/2/3/4 tint 函数。 |
| tint 与着色函数 | ⚠️ | Type 0 采样（8/16/1/2/4/12 位，多线性插值）、Type 2 指数插值、Type 3 拼接；Type 4 支持 PDF 规定的 42 个计算操作符（包括 `true/false`）、嵌套 `if/ifelse`、整数/实数类型及 `Domain/Range` 裁剪，用于 Separation/DeviceN 与渐变/网格着色函数。轴向/径向渐变仍按 32 段采样，函数不连续点可能被分段插值平滑。 |
| `CCITTFaxDecode`               |  ✅  | 由 pdfcpu 解码后重编码为 PNG（扫描件 ImageMask 常见）。                                                                                                                         |
| `JBIG2Decode`                  |  ✅  | 由 `github.com/dkrisman/gobig2` 解码为灰度位图；支持 `/JBIG2Globals`、`/Decode` 反相，`ImageMask` 按填充色着色，也可作为 `/SMask`。                                             |
| `JPXDecode`                    |  ✅  | 由 `github.com/mrjoshuak/go-jpeg2000`（纯 Go JPEG 2000）解码，支持 JP2 与裸码流、灰度/彩色/RGBA 与 `/Decode` 反相；也可作为 `/SMask`。CMYK 裸码流按库的 colr 处理，可能不准确。 |
| `SMask`（软蒙版） | ⚠️ | 图像自身 `/SMask`：1/2/4/8/16 位普通灰度样本及 JPEG/JPX/JBIG2 灰度掩码作为 PNG alpha 通道应用到图像。普通 16 位掩码或 16 位底图使用 16 位非预乘 PNG，保留颜色与透明度精度；`Decode` 的区间、反相及间接数值保留。ExtGState 图形状态 `/SMask`：仅 `/S /Luminosity` 支持，掩码的 `/G` 需能解析到图像流（直接是图像，或仅调用一次 `Do` 绘制图像的 Form XObject）；矢量掩码与一般透明组尚未支持。 |
| 透明度                         |  ⚠️  | 图像、路径、文字的 `ca`/`CA` 转换为 OFD 透明度（0-255）；不实现混合模式与透明组。                                                                                               |

图片的 `/Interpolate` 尚未单独保留；遮罩合成阶段使用最近邻，生成 PNG 在阅读器放大
显示时仍按阅读器的图片插值规则处理，极低分辨率样本的二值透明边缘可能显示为平滑过渡。

软掩码合成沿用基础图尺寸，按最近邻对齐不同尺寸的掩码；JPEG/JPX/JBIG2 掩码沿用
现有的 8 位灰度解码路径。图像自身 `/SMask` 的 `/Matte` 支持普通栅格及 8 位 JPEG 的
DeviceGray/RGB/CMYK：先应用基础图和掩码的 `Decode`，在原颜色空间还原预混合
分量，再转换为 RGB 并写入 alpha；保留 8/16 位输出精度，alpha 为 0 时颜色置零。
JPEG 底图与 JPEG 灰度掩码均支持 Matte；Adobe CMYK/YCCK 先撤销 Go 解码反相，
再应用 PDF `Decode`，在 CMYK 分量中去底色。JPEG 实际尺寸须与声明一致；支持
外层 Flate 包装，额外 JPEG 解码计入预算。与码流变换冲突的 `ColorTransform` 覆盖
暂未支持。带 Matte 的掩码必须与基础图同尺寸；JPX/JBIG2、校准或 ICC 颜色空间的
Matte 暂未支持。无效或不支持的 Matte 报告 `image.mask_matte`，普通模式保留基础图，
严格模式失败。掩码采样、Matte 逐分量还原与逐像素合成
计入 `MaxOperations`，解码、合成及缓存遵循图片预算；逐行检查取消。损坏掩码在
普通模式报告并保留基础图，严格模式失败。

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
| JPEG/JPX/JBIG2 图像的颜色键遮罩 | 报告诊断；普通模式保留基础图，严格模式失败 |
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
