# PDF 转 OFD 支持清单

本文档描述 `internal/pdf2ofd`（经 `pkg/converter/pdfimport` 暴露为
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
| `w`（线宽）、`d`（虚线）    |  ⚠️  | 线宽按 CTM 缩放；`0 w`（PDF 最细线）近似为 0.1mm（OFD 无线宽像素概念）；`d` 的虚线数组按线宽换算为 OFD `DashPattern`/`DashOffset`（OFD 以线宽为单位），空数组恢复实线。`j`/`J`/`M`/`i` 端点与连接样式未实现，保持 OFD 默认。                                                                                                                                   |
| `rg/RG/g/G/k/K`             |  ✅  | DeviceRGB / DeviceGray / DeviceCMYK 转换为 RGB。                                                                                                                                                                                                                                                                                                               |
| `cs/CS` + `sc/scn/SC/SCN`   |  ⚠️  | 按当前颜色空间解释分量：DeviceGray/RGB/CMYK、`ICCBased`、`Indexed`、`Separation`、`DeviceN`（tint 变换求值）；`Pattern` 颜色空间按下面的图案规则填充。                                                                                                                                                                                                         |
| `W` / `W*`（裁剪）          |  ✅  | 暂存到下一次路径绘制（`n/S/f/B...`）时生效；`q/Q` 恢复状态不会泄漏裁剪。                                                                                                                                                                                                                                                                                       |
| `m/l/c/v/y/h/re`            |  ✅  | `re` 拆为四段并闭合；贝塞尔曲线按三次曲线输出。                                                                                                                                                                                                                                                                                                                |
| `S/s/f/F/f\*/B/B\*/b/b\*/n` |  ✅  | 支持描边、填充、描边加填充；填充规则 NonZero / Even-Odd。                                                                                                                                                                                                                                                                                                      |
| `Do`（XObject）             |  ⚠️  | 支持 Image 与 Form（含 `Matrix`、Form 自带 `Resources`、嵌套最多 16 层）；其余子类型忽略。                                                                                                                                                                                                                                                                     |
| 内联图像 `BI/ID/EI`         |  ⚠️  | 缩写键展开；未给出 `ColorSpace` 时按规范默认 DeviceGray。                                                                                                                                                                                                                                                                                                      |
| `sh`（Shading）             |  ⚠️  | ShadingType 2/3 输出为 OFD `AxialShd`/`RadialShd`，函数按 32 段采样；ShadingType 4（自由）/5（规则）/6/7（补丁）网格着色输出为矢量 `GouraudShd`/`LaGouraudShd`（Type 5 保留每行顶点数，Type 7 按 Coons 曲面细分）；控制点超过上限或裁剪区无法还原为路径时回退为带透明度的位图。Type 1 忽略。                                                                   |
| Pattern 填充                |  ⚠️  | PatternType 1（平铺图案）展开为图片对象，图块过多时按最大图块数合成单张密集图；PatternType 2（图案着色）输出为渐变。图案自身的 `Matrix`、BBox 与颜色空间参与计算。                                                                                                                                                                                             |
| 透明组、`gs`（ExtGState）   |  ⚠️  | 读取 ExtGState 的 `ca`/`CA` 作为填充/描边不透明度并输出为 OFD 透明度；Form XObject 作为透明度组，`BM` 为 Normal 时 `Do` 时的外层 `ca`/`CA` 作为组透明度保留（不被 Form 内 `gs` 覆盖）。OFD 无混合模式：`/BM /Multiply` 的纯色填充近似为半透明（按白色背景还原原色并让文字透出，见高亮注释），其余 `/BM` 忽略且不套用外层 `ca`/`CA`；软掩码与 `/Group` 不保留。 |

## 文本

| 标准范围                                     | 状态 | 说明                                                                                              |
|----------------------------------------------|:----:|---------------------------------------------------------------------------------------------------|
| `BT/ET`、`Tf`、`Tm`、`Td/TD/TL/T\*`          |  ✅  | `Tm` 的水平/垂直缩放参与宽度与字号计算；文本矩阵 x 轴方向决定排版角度，纯旋转（无斜切）输出为 OFD 文字对象的旋转 `CTM`，并反推 `Boundary` 使码位原点落在笔位置。 |
| `Tc`（字距）、`Tw`（词距）、`Tz`（水平缩放） |  ✅  | `Tz` 同时输出为 OFD `HScale`（`hScale/100`）并计入推进量（`DeltaX`）；`Tc`/`Tw` 计入宽度与 `DeltaX`。 |
| `Tj/TJ/'/"`                                  |  ✅  | `TJ` 的数值偏移折算为字间推进；纯空白输出只推进文本矩阵，避免后续文字左移。                       |
| `Tr`（渲染模式）                             |  ⚠️  | 0/2/4/6 按填充、1/2/5/6 按描边；填充加描边同时输出颜色。                                          |
| `Ts`（文字上浮）、Type3 `d0/d1`              |  ❌  | 忽略；Type3 字体仍可通过 `/Encoding` 还原文字。                                                   |
| 相邻单字文字对象                             |  ✅  | 转换后合并相邻同样式单字对象（同字体/字号/基线/颜色/`HScale`、水平、无 CTM/Clips/Actions），逐字定位不变。 |
| 字体样式（`Weight`/`Italic`）                |  ✅  | 依据字体描述符与族名判定粗体/斜体并写入文字对象，避免渲染器对内嵌字体再次做伪斜体处理。           |
| 渐变填充的文字（`sh` 或图案着色填充）         |  ⚠️  | 文字使用渐变/图案着色填充时，按文字位置取样为 RGB 并写入 `FillColor`（保留近似纯色），不输出 OFD 渐变。 |

### 文字编码与字宽

| 标准范围                                          | 状态 | 说明                                                                                                                                                                                  |
|---------------------------------------------------|:----:|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `ToUnicode` CMap（`bfchar`/`bfrange`）            |  ✅  | 支持条目计数前缀、数组目标与增量 range。                                                                                                                                              |
| 简单字体 `/Encoding`（`/Differences` + 基础编码） |  ✅  | 字形名经 Unicode 映射还原；基础编码覆盖 WinAnsi、MacRoman、MacExpert、Symbol、ZapfDingbats、AdobeStandard。                                                                           |
| 预定义 CJK CMap                                   |  ⚠️  | GBK-EUC（含 V/2K/p）、ETen-B5、CNS-EUC、KSC-EUC/UHC、`Uni*` UCS2/UTF16；变长编码按首字节切分。全角 CMap（`Uni*` UCS2/UTF16）非 ASCII 字形回退为 1000/1000 字宽，避免缺 `DW`/`W` 时推进偏窄。 |
| `Widths` + `FirstChar`、CID `/W`、`DW`            |  ✅  | CID 字宽用于内嵌字体 hmtx 缺失或不一致时的 `DeltaX`（阈值 0.5/1000）；字宽、字号与 `DeltaX` 统一乘 CTM 平均缩放，保证“整页按 1mm 排版”文档的物理毫米增量。 |
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
| 图像朝向（负缩放 `cm`）        |  ✅  | 轴对齐的负 X/Y 缩放镜像输出为 OFD 图片对象的负缩放 `CTM`（扫描件常见），由阅读器按 `CTM` 翻转；含旋转/斜切时不处理。                                                            |
| 颜色空间                       |  ⚠️  | DeviceGray/CalGray、DeviceRGB/CalRGB、DeviceCMYK、`Indexed`、`ICCBased`（按 `N`）、`Separation`/`DeviceN`（tint 变换，Type 0/2/3 函数）；`Lab` 按 3 分量近似。                  |
| tint 变换函数                  |  ⚠️  | Type 0 采样（8/16/1/2/4/12 位，多线性插值）、Type 2 指数插值、Type 3 拼接；Type 4 PostScript 计算函数未实现。                                                                   |
| `CCITTFaxDecode`               |  ✅  | 由 pdfcpu 解码后重编码为 PNG（扫描件 ImageMask 常见）。                                                                                                                         |
| `JBIG2Decode`                  |  ✅  | 由 `github.com/dkrisman/gobig2` 解码为灰度位图；支持 `/JBIG2Globals`、`/Decode` 反相，`ImageMask` 按填充色着色，也可作为 `/SMask`。                                             |
| `JPXDecode`                    |  ✅  | 由 `github.com/mrjoshuak/go-jpeg2000`（纯 Go JPEG 2000）解码，支持 JP2 与裸码流、灰度/彩色/RGBA 与 `/Decode` 反相；也可作为 `/SMask`。CMYK 裸码流按库的 colr 处理，可能不准确。 |
| `SMask`（软蒙版）              |  ⚠️  | 8 位软蒙版作为 PNG alpha 通道应用到图像；1 位蒙版与其他子类型忽略。                                                                                                             |
| 透明度                         |  ⚠️  | 图像、路径、文字的 `ca`/`CA` 转换为 OFD 透明度（0-255）；不实现混合模式与透明组。                                                                                               |

## 注释

| 标准范围                     | 状态 | 说明                                                                                                                                       |
|------------------------------|:----:|--------------------------------------------------------------------------------------------------------------------------------------------|
| 注释外观（`Annots` + `/AP`） |  ⚠️  | 渲染 `/AP /N` 外观流（含外观状态字典，按 `/AS` 选择），应用 BBox/Matrix 与注解 `Rect` 映射；Popup/Link/Widget 无独立外观或无外观流时跳过。 |
| 注释交互（链接、表单、弹窗） |  ❌  | 不输出交互动作，仅保留可见外观。                                                                                                           |

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
| 文档元数据                 |  ✅  | `Title/Author/Subject` 映射到 `DocInfo`；`Creator=zc310/ofd`、`CreatorVersion=0.0.1`。 |
| 溯源信息                   |  ✅  | `CustomData SourceFormat=PDF`；PDF `Producer` 保留在 `CustomData SourceProducer`。     |

## 暂不支持的能力

| 标准范围                                     | 行为                                                          |
|----------------------------------------------|---------------------------------------------------------------|
| 链接、表单域、弹窗等交互注解                 | 忽略（仅保留 `/AP` 外观）                                     |
| ShadingType 1、PatternType 之外的着色        | 忽略（回退为纯色或不填充）                                    |
| 混合模式、软掩码 1 位、透明组（`/Group`）    | 忽略（`ca`/`CA` 透明度保留；`Multiply` 纯色填充近似为半透明） |
| 数字签名、附件、嵌入式 JavaScript/PostScript | 忽略                                                          |
| 大纲其他动作（GoToR、JavaScript、Launch 等） | 忽略（仅保留 `GoTo` 与 URI）                                  |
| `Ts` 文字上浮、Type3 `d0/d1` 度量            | 忽略                                                          |
| 端点/连接样式（`j/J/M`）、斜接限制（`M`）    | 保持 OFD 默认值                                               |

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
`TestConvertTestdataPDFs` 会对 `test/testdata/pdf/` 下的固定样本做端到端转换校验。
