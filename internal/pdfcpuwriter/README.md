# pdfcpu 实验写入包

这是 OFD 内部实验包，**不注册后端、不修改默认转换路径，也不是完整 PDF 编码器**。
实现位于本目录的 `writer.go`、`font.go`、`cff.go` 和 `gradient.go`；
OFD/canvas 集成对照测试仍位于 `internal/render/backends/canvas/pdfcpu_prototype*_test.go`。

## 接口与生命周期

- `New(ctx, width, height)` 创建含第一页的 `*Document`，尺寸单位毫米，坐标 y 轴向上。
- `Document` 实现内置 `canvas.Renderer`，支持已有原生路径/文字/图片裁剪及共享渐变画笔扩展。
- `NextPage(width, height)` 封存当前页，开始不同尺寸的新页；不得沿用上一页的 canvas Context 状态。
- `Err()` 检查绘制回调中累计的错误；`Stats()` 仅返回资源计数，不暴露可变 PDF 对象。
- `Write(w)` 完成全部字体及页面嵌入，只能调用一次。绘制失败不写出；底层写入失败
  可能留下部分输出，调用方须丢弃，不得重试同一实例。
- 使用 `errors.Is(err, ErrUnsupported)` 识别尚未支持的能力，使用 `ErrClosed` 识别结束后的调用。
  取消返回原始 `ctx.Err()`。共享渐变文字扩展遇到不支持的画笔返回 `false`，供现有适配层回退。
- 不支持并发调用。字体及相关资源必须保持有效，直到 `Write` 返回；输出 writer 归调用方所有。
  不安装全局字体、不启动后台任务，无需额外 `Close`；结束后丢弃实例释放内存。

最小用法（限 OFD 模块内部）：

```go
doc, err := pdfcpuwriter.New(ctx, 210, 297)
if err != nil {
    return err
}
page := canvas.NewContext(doc)
page.SetFillColor(canvas.Black)
page.DrawPath(10, 20, canvas.Rectangle(30, 15))
if err := doc.NextPage(297, 210); err != nil {
    return err
}
page = canvas.NewContext(doc)
page.DrawPath(20, 30, canvas.Rectangle(40, 10))
return doc.Write(output)
```

## 边界

- OFD 渲染核心通过现有 canvas 状态适配器直接向原型发送绘制操作；
  不先生成 PDF，也不调用 canvas 的 PDF 写入器。
- 仍使用 canvas 的几何、样式和已整形文字，以及现有 font 包的 TrueType 子集能力。
  CFF 保留完整原程序，不重排字形、FDSelect 或局部子程序。
- pdfcpu 负责 PDF 对象、流编码、交叉引用表及最终写出；图片资源也由 pdfcpu 创建。
- 裁剪、描边、文字定位、字体字典、Encoding CMap、CIDToGIDMap 和 ToUnicode 由原型构造。
  保留源字形及位置；同一字形对应不同文本时分配不同字符编码。
- 支持不透明轴向/径向渐变、多色标拼接函数、Extend 和独立文字画笔坐标。
  Pattern 与颜色函数分别复用；缓存按浮点位模式区分坐标及色标位置，不近似合并。
- 支持横向 TrueType、名称键控 CFF（Type1C）和 CID 键控 CFF（CIDFontType0C）。
  名称键控 CFF 通过字形名编码，当前每个字体资源最多 255 个非零字符编码；
  CID 键控 CFF 通过自定义 CMap 映回原 CID，不把 CID 误当 GID。
- 支持多页及不同页面尺寸；字体汇总全部页面字形后嵌入一次，图片按原始编码或无损
  PNG 编码的 SHA-256 精确复用，渐变及透明度资源在文档内共享。
  颜色图片与 SMask 均显式开启插值，避免 pdfcpu 默认插值策略带来的透明边缘差异。
  图片绘制前与 canvas/pdf 一样设置外接矩形和图片四边形裁剪，保持边缘抗锯齿一致。
- 字节及流类型完全相同的字体程序共享嵌入流，各字体字典的编码和 ToUnicode 独立；
  不为减小体积改写完整 CFF 程序。页面共享一个间接资源字典，不逐页内联整份资源表。
- 当前支持标准端点/连接点及图片。透明渐变、图案、CFF2、竖排、合成字重、
  名称键控 CFF 超出编码上限等明确报错，不静默丢弃内容。
- 不安装全局字体，不修改 pdfcpu 或 canvas 的模块缓存，不新增模块依赖。

## 验证

从仓库根目录运行：

```sh
go test ./internal/pdfcpuwriter -count=1
go test ./internal/render/backends/canvas -run '^TestPDFCPUPrototype' -v -count=1
go test -race ./internal/pdfcpuwriter ./internal/render/backends/canvas -count=1
```

测试默认只使用内存和临时目录。安装 `pdftotext`、`pdftoppm` 后，会检查文字提取及 PDF
栅格化结果；缺少这些工具只跳过对应子测试。源 fixture 为
`testdata/ofdrw/intro.ofd`，缺失时跳过 fixture 测试。

需要保存第 7、26 页原型与 canvas 对照 PDF 时，显式指定临时输出目录：

```sh
OFD_PDFCPU_PROTOTYPE_DIR=/tmp/pdfcpu-prototype \
  go test ./internal/render/backends/canvas -run '^TestPDFCPUPrototypeIntroPage' -v -count=1
```

第 26 页检查完整页面、中文搜索、两张图片的 SMask，以及紫色水印的原生裁剪和 224 段
三次曲线。另有字形别名/非 BMP Unicode、裁剪孔洞、透明度恢复和取消检查。
第 7 页检查渐变资源维持 16 个及文字/像素一致性。该页里的几个 `.cff` 资源实际是
TrueType，不能用扩展名判定格式，也不能把该页通过当成 CFF 已验证。

CFF 另外使用样本内真实 CFF 字体验证，并用合成的非恒等 CID、多 Font DICT 字体检查
CID 580/22031、Unicode 别名和原程序不变。可见字形独立对照源轮廓，避免错误的字体映射
在两个嵌入实现中相互抵消；原生字体 hinting 与轮廓填充的边缘差异另做墨迹位移检查。
轴向/径向渐变另有非等比缩放、倾斜、逐 run 定位、色标端点补齐和单 ULP 缓存回归。

完整文档测试默认检查全部页面的写出、结构及文字提取；逐页 144 DPI 像素对照可显式启用，
每次只保留一对页面图像，不把 84 张图同时加载到内存：

```sh
OFD_PDFCPU_FULL_PIXELS=1 OFD_PDFCPU_PROTOTYPE_DIR=/tmp/pdfcpu-full \
  GOWORK=off go test ./internal/render/backends/canvas \
  -run '^TestPDFCPUPrototypeIntroFullDocument$' -v -count=1
```

输出目录中的 `multipage-pdfcpu.pdf`、`multipage-canvas.pdf` 为本地对照产物，不提交。
这里对照的是内置 canvas，不是官方 PDF，故不跳过第 14–19 页。

速度与分配量基准（不运行外部工具）：

```sh
GOWORK=off go test ./internal/render/backends/canvas -run '^$' \
  -bench '^BenchmarkPDFCPUPrototypeIntroFullDocument$' -benchtime=3x -count=5 -benchmem
```

每次迭代独立加载同一样本并新建渲染文档；计时覆盖42页绘制到内存中完整PDF写出，
包含首次字体加载，不含输入解析及文档关闭。canvas 使用现有场景记录/回放，pdfcpu 使用直接绘制；
均开启压缩及支持范围内的字体子集，CFF 保留策略仍有上述差异。
`B/op` 是累计分配字节，不是峰值内存；此基准不包含进程冷启动和磁盘输出，
也不代表所有文档的性能。不能复用同一个渲染文档预热后直接外推首次转换速度。

本机 Linux/amd64、Ryzen 9 5950X、Go 1.26.9，5组各3次的中位数：

| 后端 | 42页绘制及写出 | 累计分配（十进制MB） | 分配次数 |
|---|---:|---:|---:|
| canvas | 0.955秒 | 649 MB | 约414万 |
| pdfcpu原型 | 1.399秒 | 872 MB | 约922万 |

当前原型耗时约增加46.5%，累计分配约增加34.4%；尚无速度优势。
这包含原型自身的图片编码、内容格式化和资源构建成本，不能归因于pdfcpu库本身。

## 初步结果与后续

在包含本轮 canvas 优化的内置版本中，单页输出约为：

| 页码 | pdfcpu 原型 | canvas 对照（压缩、字体子集） |
|---|---:|---:|
| 7 | 31.4 KB | 33.6 KB |
| 26 | 147.6 KB | 153.3 KB |

两页文字提取一致。第 26 页在 144 DPI 约 247 万像素中，仅 16 个像素存在差异；
第 7 页按通道容差 20 比较，匹配率约 99.9976%。这只是单页结果，不能外推整份文档体积
或性能。canvas 现已迁入 `internal/thirdparty/canvas`，运行原型不需要外部 workspace。

多页回归覆盖不同页面尺寸、跨页新增字形、同像素不同图片实例及透明度，以及样本的
第 7/26/7 页组合。该组合 pdfcpu 为 199,503 字节，canvas 为 210,840 字节；
文字提取一致，通道容差 20 下像素匹配率约 99.9984%。
完整 42 页已验证结构、文字及逐页像素（通道容差 20），最低匹配率约 99.9976%（第7页）。
字体程序精确复用、资源表改为间接对象并补齐图片边缘裁剪后，pdfcpu 为 7,725,622 字节，
canvas 为 7,675,323 字节，原型仍大约 0.66%；不能把单页体积优势外推到整份文档。
上述字节数为测量快照，pdfcpu 输出大小在不同运行间存在少量波动。拆包后重新验证
42页的解压内容流与拆包前逐页一致，文字提取哈希一致；最低像素匹配率仍约99.9976%。
复用前原型约 7,904,677 字节，字体程序压缩流合计 580,563 字节，对照为 339,426 字节；
主要差异包含原型保留的完整 CFF 程序，而对照会对子集能力支持的字体进行子集化。
第 32–35 页包含不可见搜索文字，原型用 `Tr 3` 保留文字语义，不为其设置空画笔。
这些页面此前的局部差异位于照片竖直边缘，与不可见文字无关：原型缺少 canvas/pdf
在图片外额外设置的外接矩形和图片四边形裁剪。补齐后，144 DPI 下第32、34、35页
逐像素完全一致，第33页仅1个像素不同（最大通道差14）；通道容差20下四页均完全匹配。
针对这四页新增每页匹配率至少99.9999%的回归检查，防止全页宽松阈值掩盖边缘退化。
此结果仅覆盖页面绘制，不代表已支持交互链接、大纲或模板 Form 复用。
尚未测量峰值内存；资源表仍由全部页面共享，未按页裁剪。

可继续验证模板 Form、链接和大纲，以及透明渐变与名称键控 CFF
超过 255 个编码时的资源拆分。
完成这些验证前，不替换生产后端，也不把原型包装成可用的对外转换选项。
