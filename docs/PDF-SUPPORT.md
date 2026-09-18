# PDF 转 OFD 支持清单

本文档描述 `internal/pdf2ofd`（经 `pkg/converter/pdfimport` 暴露为
`converter.Convert("pdf", "ofd", ...)` 与 `ofd-converter` 的 PDF 输入）当前对
PDF 规范（ISO 32000-1/-2）的支持范围。主要实现方式：pdfcpu 解析 PDF 对象结构，
自研内容流解释器还原页面几何、文字、路径与图像，并经 `pkg/creator` 输出 OFD 包。

本文档描述的是项目当前的实现范围，不代表完整实现或通过任何一致性认证。真实 PDF
样本差异极大，使用前仍应以目标业务样本验证。

## 支持级别

| 级别     | 含义                                                      |
|----------|-----------------------------------------------------------|
| 已支持   | 已有对应实现，并有测试或实际样例依据。                    |
| 部分支持 | 可以处理主要结构，但部分属性、组合场景或视觉效果有限制。  |
| 暂不支持 | 当前没有实现；相关对象通常被忽略，或该对象/页面转换失败。 |

## 页面与几何

| 标准范围                       | 当前状态 | 说明                                                                                     |
|--------------------------------|----------|------------------------------------------------------------------------------------------|
| `MediaBox`                     | 已支持   | 缺少 `CropBox` 时回退到 `MediaBox`；页面缺 Box 时转换失败。                              |
| `CropBox` 与继承属性           | 已支持   | 依次取页面 `CropBox`、继承的 `CropBox`、继承的 `MediaBox`、页面 `MediaBox`。             |
| 非 0 原点的 Box                | 已支持   | 按左下角归一后平移页面坐标。                                                             |
| `Rotate`（0/90/180/270）       | 部分支持 | 页面尺寸按 90/270 交换宽高；内容旋转仅对简单文字/图像组合验证过。                        |
| `UserUnit`                     | 已支持   | 乘入页面尺寸、文字宽度和字号。                                                           |
| PDF 坐标系（点，左下原点）     | 已支持   | 按 1pt = 25.4/72mm 换算；OFD 使用页顶原点的毫米坐标。                                    |

## 内容流操作符

| 操作符                                                   | 当前状态 | 说明                                                                                       |
|----------------------------------------------------------|----------|--------------------------------------------------------------------------------------------|
| `q` / `Q`（图形状态栈）                                  | 已支持   |                                                                                            |
| `cm`                                                     | 已支持   | 右乘当前 CTM；对嵌套 Form XObject 的平移缩放做过专项修正。                                 |
| `w`（线宽）                                              | 部分支持 | 线宽按 CTM 缩放；`d`/`j`/`J`/`M`/`i` 线型与端点样式未实现，保持 OFD 默认。                 |
| `rg/RG/g/G/k/K`                                          | 已支持   | DeviceRGB / DeviceGray / DeviceCMYK 转换为 RGB。                                           |
| `cs/CS` + `sc/scn/SC/SCN`                                | 部分支持 | 支持颜色空间名称与分量数自适应的纯色；Pattern、Shading 输出被忽略。                        |
| `W` / `W*`（裁剪）                                       | 已支持   | 暂存到下一次路径绘制（`n/S/f/B...`）时生效；`q/Q` 恢复状态不会泄漏裁剪。                   |
| `m/l/c/v/y/h/re`                                         | 已支持   | `re` 拆为四段并闭合；贝塞尔曲线按三次曲线输出。                                            |
| `S/s/f/F/f\*/B/B\*/b/b\*/n`                              | 已支持   | 支持描边、填充、描边加填充；填充规则 NonZero / Even-Odd。                                  |
| `Do`（XObject）                                          | 部分支持 | 支持 Image 与 Form（含 `Matrix`、Form 自带 `Resources`、嵌套最多 16 层）；其余子类型忽略。 |
| 内联图像 `BI/ID/EI`                                      | 部分支持 | 缩写键展开；未给出 `ColorSpace` 时按规范默认 DeviceGray。                                  |
| `sh`（Shading）、Pattern 绘制、透明组、`gs`（ExtGState） | 暂不支持 | 相关对象被忽略，透明度与混合模式不保留。                                                   |

## 文本

| 标准范围                                     | 当前状态 | 说明                                                                                              |
|----------------------------------------------|----------|---------------------------------------------------------------------------------------------------|
| `BT/ET`、`Tf`、`Tm`、`Td/TD/TL/T\*`          | 已支持   | `Tm` 的水平/垂直缩放参与宽度与字号计算；旋转与斜切只保留尺度，方向信息不输出。                    |
| `Tc`（字距）、`Tw`（词距）、`Tz`（水平缩放） | 已支持   | 参与宽度计算并输出为 OFD `DeltaX`。                                                               |
| `Tj/TJ/'/"`                                  | 已支持   | `TJ` 的数值偏移折算为字间推进；纯空白输出只推进文本矩阵，避免后续文字左移。                       |
| `Tr`（渲染模式）                             | 部分支持 | 0/2/4/6 按填充、1/2/5/6 按描边；填充加描边同时输出颜色。                                          |
| `Ts`（文字上浮）、Type3 `d0/d1`              | 暂不支持 | 忽略；Type3 字体仍可通过 `/Encoding` 还原文字。                                                   |
| 相邻单字文字对象                             | 已支持   | 转换后合并相邻同样式单字对象（同字体/字号/基线/颜色、水平、无 CTM/Clips/Actions），逐字定位不变。 |

### 文字编码与字宽

| 标准范围                                          | 当前状态 | 说明                                                                                                        |
|---------------------------------------------------|----------|-------------------------------------------------------------------------------------------------------------|
| `ToUnicode` CMap（`bfchar`/`bfrange`）            | 已支持   | 支持条目计数前缀、数组目标与增量 range。                                                                    |
| 简单字体 `/Encoding`（`/Differences` + 基础编码） | 已支持   | 字形名经 Unicode 映射还原；基础编码覆盖 WinAnsi、MacRoman、MacExpert、Symbol、ZapfDingbats、AdobeStandard。 |
| 预定义 CJK CMap                                   | 部分支持 | GBK-EUC（含 V/2K/p）、ETen-B5、CNS-EUC、KSC-EUC/UHC、`Uni*` UCS2/UTF16；变长编码按首字节切分。              |
| `Widths` + `FirstChar`、CID `/W`、`DW`            | 已支持   | CID 字宽用于内嵌字体 hmtx 缺失或不一致时的 `DeltaX`（阈值 0.5/1000）。                                      |
| 内嵌字体（TrueType/CFF 包装）                     | 部分支持 | 保留嵌入数据（`PreserveEmbeddedFonts`）；字形映射走 `CIDToGIDMap` 或字体 cmap，`CGTransform` 输出实际字形。 |
| 不连续 CID / 子集字体                             | 已支持   | 经 fontfix 私有区映射（F0000+CID）判定字形存在，避免误丢整段文字。                                          |
| 字体族名（含 `#XX` 十六进制转义）                 | 已支持   | 按原始字节再解释为 UTF-8，非法转义时原样保留。                                                              |
| 未嵌入字体                                        | 部分支持 | 输出为 OFD 逻辑字体，由阅读器按族名回退本机字体。                                                           |

## 图像

| 标准范围                                     | 当前状态 | 说明                                                                                                |
|----------------------------------------------|----------|-----------------------------------------------------------------------------------------------------|
| `DCTDecode`（JPEG）                          | 部分支持 | JPEG 数据直接嵌入 OFD；DeviceCMYK JPEG（含 Adobe APP14 反相约定）转为 RGB PNG。                     |
| `FlateDecode` 解码后的栅格数据               | 部分支持 | 8 位 DeviceGray/DeviceRGB、DeviceCMYK（油墨值）与 Indexed 调色板图像重编码为 PNG。                  |
| `ImageMask`（1 位蒙版）                      | 部分支持 | 0 采样按当前填充色着色；仅支持 1 位蒙版。                                                           |
| `Decode` 数组（反相）                        | 已支持   |                                                                                                     |
| 颜色空间                                     | 部分支持 | DeviceGray/CalGray、DeviceRGB/CalRGB、DeviceCMYK、`Indexed`（基础空间受限）、`ICCBased`（按 `N`）。 |
| `JPXDecode`、`CCITTFaxDecode`、`JBIG2Decode` | 暂不支持 | 该图像转换失败（不影响其他内容）。                                                                  |
| SMask（软蒙版）、 transparency               | 暂不支持 | 忽略。                                                                                              |

## 包结构与容错

| 标准范围                   | 当前状态 | 说明                                                                                   |
|----------------------------|----------|----------------------------------------------------------------------------------------|
| 输入形式                   | 已支持   | 文件路径、`[]byte`、`io.Reader`（经 `converter.Convert` 或 `pdf2ofd.Convert`）。       |
| 经典 xref / 对象流         | 已支持   | pdfcpu 解析；对象流（compressed object streams）由 pdfcpu 展开。                       |
| xref 损坏或 free-list 异常 | 已支持   | 自动重建经典 xref（`repairPDFXRef`）后重试。                                           |
| 解析异常（panic）          | 已支持   | recover 并在修复 xref 后重试一次。                                                     |
| 损坏的 `Info` 日期         | 已支持   | 忽略 Info 引用重新校验，保留已提取元数据。                                             |
| 文档元数据                 | 已支持   | `Title/Author/Subject` 映射到 `DocInfo`；`Creator=zc310/ofd`、`CreatorVersion=0.0.1`。 |
| 溯源信息                   | 已支持   | `CustomData SourceFormat=PDF`；PDF `Producer` 保留在 `CustomData SourceProducer`。     |

## 暂不支持的能力

| 标准范围                                            | 行为                       |
|-----------------------------------------------------|----------------------------|
| 注释与链接注解（`Annots`）、AcroForm 表单域         | 忽略                       |
| Shading（`sh`）、Pattern 填充、ExtGState（`gs`）    | 忽略（透明度/混合不保留）  |
| SMask、透明组（`/Group`、blend mode）               | 忽略                       |
| JPX / CCITT / JBIG2 图像                            | 转换失败（返回错误）       |
| 数字签名、附件、嵌入式 JavaScript/PostScript        | 忽略                       |
| 书签（Outlines）结构                                | 忽略                       |
| `Ts` 文字上浮、Type3 `d0/d1` 度量                   | 忽略                       |
| 线型（`d`）、端点/连接样式（`j/J/M`）               | 保持 OFD 默认值            |

## 验证方式

```bash
go test ./internal/pdf2ofd -count=1
go test ./pkg/converter -run '^Example' -count=1
go run ./cmd/ofd-validator --format text --mode strict output.ofd
```

`internal/pdf2ofd/pdf_to_ofd_test.go` 覆盖页面几何、矩阵级联、文本推进与
`DeltaX`、CJK 解码、裁剪、内联图像、ImageMask、CMYK JPEG、文字对象合并与
元数据；`TestConvertTestdataPDFs` 会对 `test/testdata/pdf/` 下的固定样本做
端到端转换校验。
