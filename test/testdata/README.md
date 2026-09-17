# OFD 测试文档索引

本目录保存解析、校验、文字提取、渲染和格式转换使用的 OFD fixture。下表中的对象、文本和资源数量来自 `cmd/ofd-analyzer` 的当前分析结果；文件大小使用 K/M 表示（1K = 1000 bytes）。数量会随着 fixture 内容变化而变化，特性说明则以便于选取测试样例为目标。

除明确标注生成来源的文件外，本目录中的 OFD 文件均为从互联网搜集的公开样本，仅用于测试和兼容性验证。样本的原始发布者、授权许可和来源链接尚未逐一核实；如需再分发或用于生产，请自行确认相应权利。

## 文件清单

| 文件                                           |  大小 | 结构摘要                                                                   | 主要特性和适用场景                                                                                                                                                                                                            |
|------------------------------------------------|------:|----------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| [`1000-pages.ofd`](1000-pages.ofd)             |  466K | 1 文档，1000 页，1000 对象，19000 字符，1 字体                             | A4 纵向大文档；每页带模板内容和页码，适合测试大页数、首尾页、分页加载、虚拟列表和批量渲染。生成来源：[`1000-pages.json`](../../cmd/ofd-creator/examples/1000-pages.json)。                                                    |
| [`5000-pages.ofd`](5000-pages.ofd)             |   2.3M | 1 文档，5000 页，5000 对象，95000 字符，2 字体，1 模板                                                  | A4 纵向大文档；每页带模板内容和页码，适合测试中等页数规模、首尾页、分页加载、虚拟列表和批量渲染。生成来源：[`5000-pages.json`](../../cmd/ofd-creator/examples/5000-pages.json)。                                                                                                                       |
| [`10000-pages.ofd`](10000-pages.ofd)           |   4.7M | 1 文档，10000 页，10000 对象，200001 字符，2 字体，1 模板                                               | A4 纵向超大大文档；每页带模板内容和页码，适合测试极限页数、虚拟列表、内存占用、分页加载和批量渲染。生成来源：[`10000-pages.json`](../../cmd/ofd-creator/examples/10000-pages.json)。                                                                                                                 |
| [`999.ofd`](999.ofd)                           | 29.8K | 1 文档，5 页，561 对象，2752 字符，1 图片，4 字体，5 模板                  | 真实发票类文档；包含混合页面方向、二维码、模板、附件、注释/标签包和 1 个签名，适合测试复杂包结构、资源引用、附件及签名读取。                                                                                                  |
| [`ano.ofd`](ano.ofd)                           |  718K | OFD 1.1，3 页，318 对象，6602 字符，8 图片，9 字体                         | 注释与外部资源样例；包含 Stamp 和 Link 页面注释、透明/变换文字、嵌入字体和 JPG/PNG 图片，适合渲染、注释、字体和图像回归测试。                                                                                                 |
| [`drawparam.ofd`](drawparam.ofd)               |  7.0K | 4 页，94 对象，1204 字符，12 DrawParam，1 颜色空间                         | 绘制参数示例；覆盖 DrawParam 引用与对象级属性、线宽、Cap、Join、MiterLimit、虚线及颜色，适合验证绘制参数继承和路径描边。                                                                                                      |
| [`font-styles.ofd`](font-styles.ofd)           |  3.6K | 2 页，48 对象，1004 字符，4 字体，1 颜色空间                               | 字体样式矩阵；覆盖 100--900 字重、常规/斜体、字号、HScale、填充/描边文字，以及轴向和径向渐变文字。                                                                                                                            |
| [`hello.ofd`](hello.ofd)                       |   1.4K | 1 页，2 对象，21 字符，1 字体                                                                     | 最小 Hello World 文档；适合快速验证 OFD 打开、基础文字读取和最小渲染链路。生成来源：[`hello.yaml`](../../cmd/ofd-creator/examples/hello.yaml)。                                                                                                                                                             |
| [`helloworld.ofd`](helloworld.ofd)             |   2.2K | 1 文档，2 页，10 对象，42 字符，1 字体                                                               | 基础创建器输出；包含英文和中文文字，以及轴向/径向渐变矩形，适合 converter API、PDF 输出和渐变渲染测试。生成来源：[`helloworld.yaml`](../../cmd/ofd-creator/examples/helloworld.yaml)。                                                                                                                          |
| [`huawei.ofd`](huawei.ofd)                     |  3.1K | 1 页，8 对象，8 个路径，横向，无文字                                       | Huawei 图标提取结果；由渐变填充的路径和 CTM 组成，包含轴向/径向 Shading，适合测试路径变换、渐变渲染和无文字文档。                                                                                                             |
| [`intro.ofd`](intro.ofd)                       |  7.5M | 1 文档，42 页，1806 对象，5120 字符，66 图片，12 字体，9 复合对象          | 较大的真实转换文档；包含大量嵌入字体、图片、路径和复合对象，页面为横向，适合字体修复、图片/矢量渲染、SVG/PDF 转换、性能和文字提取测试。                                                                                       |
| [`kaiti-styles.ofd`](kaiti-styles.ofd)         |  3.0K | 2 页，28 对象，451 字符，1 字体                                            | 楷体排版样例；覆盖字号、粗体、特粗体、斜体、粗斜体、空心描边字和压缩字宽。生成来源：[`kaiti-styles.yaml`](../../cmd/ofd-creator/examples/kaiti-styles.yaml)。                                                                 |
| [`line-styles.ofd`](line-styles.ofd)           |  1.9K | 1 页，16 对象，147 字符，8 路径，1 字体                                    | 线条样式样例；覆盖 Butt/Round/Square 端点、Miter/Round/Bevel 连接、线宽、虚线、偏移、折线和透明度。生成来源：[`line-styles.json`](../../cmd/ofd-creator/examples/line-styles.json)。                                          |
| [`multi_demo.ofd`](multi_demo.ofd)             |  9.1K | 3 文档体，4 页，55 对象，530 字符，6 字体，3 DrawParam，3 颜色空间，1 附件 | 多文档包样例；一个 OFD 包中包含 3 个独立文档，覆盖文档级资源、绘制参数、颜色空间和附件，适合测试文档作用域及跨文档 ID 隔离。                                                                                                  |
| [`path-fill-rules.ofd`](path-fill-rules.ofd)   |  4.1K | 3 页，33 对象，317 字符，路径填充规则                                      | 路径填充规则对比；覆盖默认规则、`NonZero`、`Even-Odd`、嵌套轮廓、星形、多孔图形、重叠路径，以及渐变填充和对象级 Alpha。生成来源：[`path-fill-rules.json`](../../cmd/ofd-creator/examples/path-fill-rules.json)。              |
| [`project-showcase.ofd`](project-showcase.ofd) |  4.5K | 2 页，50 对象，388 字符，1 字体                                            | OFD 项目介绍页；包含文档元数据、背景/正文图层、图形装饰和可点击 GitHub 跳转，适合测试创建器元数据、图层及跳转动作。生成来源：[`project-showcase.yaml`](../../cmd/ofd-creator/examples/project-showcase.yaml)。                |
| [`radial_demo.ofd`](radial_demo.ofd)           |  6.8K | 5 页，51 对象，537 字符，1 字体，1 颜色空间                                | 径向渐变参数样例；分别展示 `Eccentricity` 和 `Angle` 的变化，适合验证非圆形径向渐变、渐变参数序列化和渲染。                                                                                                                   |
| [`shading.ofd`](shading.ofd)                   |  6.0K | 6 页，52 对象，332 字符，4 Pattern                                         | Shading 综合样例；覆盖 Axial、Radial、Gouraud、LaGouraud 和 Pattern 填充，并包含 Direct/Repeat/Reflect 映射，适合渐变和网格着色回归测试。                                                                                     |
| [`text-directions.ofd`](text-directions.ofd)   |  6.6K | 5 页，84 对象，709 字符，1 字体                                            | 文字方向样例；覆盖 `ReadDirection` 的 0/90/180/270 度和 `CharDirection` 的 0/90 度组合，适合横排、竖排、旋转字形和 `TextCode` 测试。生成来源：[`text-directions.json`](../../cmd/ofd-creator/examples/text-directions.json)。 |
| [`zsbk.ofd`](zsbk.ofd)                         |  1.6M | OFD 1.1，2 页，38 对象，296 字符，3 图片，12 字体，2 模板，2 签名          | 真实业务文档样例；页面方向混合，包含图片、嵌入字体、模板页和双签名/印章文件，适合测试真实文档解析、模板应用和签名数据读取。                                                                                                   |

## 选取建议

- 最小打开或基础渲染：`hello.ofd`、`helloworld.ofd`
- 字体和文字布局：`font-styles.ofd`、`kaiti-styles.ofd`、`text-directions.ofd`
- 路径、描边和渐变：`drawparam.ofd`、`line-styles.ofd`、`path-fill-rules.ofd`、`radial_demo.ofd`、`shading.ofd`、`huawei.ofd`
- 图片、复合对象和性能：`ano.ofd`、`intro.ofd`
- 模板、附件、注释和签名：`999.ofd`、`zsbk.ofd`
- 多文档和资源作用域：`multi_demo.ofd`
- 元数据、图层和跳转：`project-showcase.ofd`
- 大页数与分页性能：`1000-pages.ofd`、`5000-pages.ofd`、`10000-pages.ofd`

## 常用命令

以下命令从仓库根目录运行：

```text
go run ./cmd/ofd-analyzer --format text test/testdata/helloworld.ofd
go run ./cmd/ofd-converter -format pdf test/testdata/intro.ofd /tmp/intro.pdf
go run ./cmd/ofd-converter -format png -page 1 test/testdata/ano.ofd /tmp/ano-page-1.png
go test ./internal/render ./test -count=1
```

创建器示例的 manifest 位于 [`cmd/ofd-creator/examples`](../../cmd/ofd-creator/examples)。修改这些 manifest 或对应 fixture 时，应执行 `--check`、`--validate` 和必要的严格校验。
