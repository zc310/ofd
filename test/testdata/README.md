# OFD 测试文档索引

本目录保存解析、校验、文字提取、渲染和格式转换使用的 OFD fixture。下表登记本目录下全部 39 个 `.ofd` 文件；文档体、页面、对象、文字、图片、字体等数量来自 `cmd/ofd-analyzer` 的当前分析结果。文件大小使用 K/M 表示（1K = 1000 bytes，1M = 1000000 bytes）。数量会随着 fixture 内容变化而变化，特性说明则以便于选取测试样例为目标。

来源分两类：`OFD.xml` 中带 `Creator=ofd-creator` 的文件由本仓库的创建器生成，表中给出对应的生成来源 manifest；其余文件（`999.ofd`、`ano.ofd`、`drawparam.ofd`、`font-styles.ofd`、`huawei.ofd`、`intro.ofd`、`multi_demo.ofd`、`radial_demo.ofd`、`shading.ofd`、`zsbk.ofd`）为手工构造或外部收集的样本，原始发布者、授权许可和来源链接尚未逐一核实；如需再分发或用于生产，请自行确认相应权利。

`markdown/` 和 `pdf/` 子目录保存的是 Markdown、PDF 等非 OFD 格式的导入测试输入，不在本表登记。

## 文件清单

| 文件                                           |  大小 | 结构摘要                                                                                      | 主要特性和适用场景                                                                                                                                                                                                              |
|------------------------------------------------|------:|-----------------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| [`1000-pages.ofd`](1000-pages.ofd)             |  467K | 1 文档，1000 页，1000 对象，19000 字符，2 字体，1 模板                                        | A4 纵向大文档；每页带模板内容和页码，适合测试大页数、首尾页、分页加载、虚拟列表和批量渲染。生成来源：[`1000-pages.json`](../../cmd/ofd-creator/examples/1000-pages.json)。                                                      |
| [`5000-pages.ofd`](5000-pages.ofd)             |  2.3M | 1 文档，5000 页，5000 对象，95000 字符，2 字体，1 模板                                        | A4 纵向大文档；与 `1000-pages.ofd` 同结构、页数更多，适合测试中等页数规模、分页加载、虚拟列表和批量渲染。                                                                                                                       |
| [`10000-pages.ofd`](10000-pages.ofd)           |  4.7M | 1 文档，10000 页，10000 对象，200001 字符，2 字体，1 模板                                     | A4 纵向超大大文档；与 `1000-pages.ofd` 同结构，适合测试极限页数、虚拟列表、内存占用和批量渲染。                                                                                                                                 |
| [`999.ofd`](999.ofd)                           | 29.8K | 1 文档，5 页，561 对象，2752 字符，1 图片，4 字体，1 颜色空间，5 模板，1 附件，1 注解，1 签名 | 真实发票类文档；包含混合页面方向、二维码、模板、附件、Stamp 注解和 1 个签名，适合测试复杂包结构、资源引用、附件及签名读取。                                                                                                     |
| [`actions.ofd`](actions.ofd)                   |  2.5K | 1 文档，2 页，4 对象，61 字符，2 多媒体，1 字体                                               | 动作示例；点击打开 URI、按书签跳到第二页，并引用音频和视频资源。生成来源：[`actions.yaml`](../../cmd/ofd-creator/examples/actions.yaml)。                                                                                       |
| [`annotations.ofd`](annotations.ofd)           |  2.7K | 1 文档，1 页，32 对象，165 字符，1 字体，2 注解                                               | 页面注解示例；整页 Watermark 水印（多处旋转的「保密资料」文字）加一枚半透明红色「已阅」Stamp 印章，适合测试注解类型解析、外观渲染和注解叠加。生成来源：[`annotations.yaml`](../../cmd/ofd-creator/examples/annotations.yaml)。  |
| [`ano.ofd`](ano.ofd)                           |  718K | OFD 1.1，3 页，533 对象，7250 字符，8 图片，9 字体，55 注解                                   | 注释与外部资源样例；包含 Stamp 和 Link 页面注释、透明/变换文字、嵌入字体和 JPG/PNG 图片，适合渲染、注释、字体和图像回归测试。                                                                                                   |
| [`axial-extend.ofd`](axial-extend.ofd)         |  1.7K | 1 文档，1 页，9 对象，85 字符，1 字体                                                         | 轴向渐变 Extend 示例；轴线短于矩形，分别展示 Extend 0、1、2、3。生成来源：[`axial-extend.yaml`](../../cmd/ofd-creator/examples/axial-extend.yaml)。                                                                             |
| [`clips.ofd`](clips.ofd)                       |  1.7K | 1 文档，1 页，6 对象，16 字符，1 字体                                                         | 裁剪区示例；矩形分别被椭圆路径和文字裁剪，并演示同一裁剪区中两个路径取并集。生成来源：[`clips.yaml`](../../cmd/ofd-creator/examples/clips.yaml)。                                                                               |
| [`color-spaces.ofd`](color-spaces.ofd)         |  2.4K | 1 文档，1 页，15 对象，72 字符，1 字体，4 颜色空间                                            | 颜色空间示例；覆盖 GRAY、CMYK、RGB 调色板和带 sRGB ICC Profile 的 RGB。生成来源：[`color-spaces.yaml`](../../cmd/ofd-creator/examples/color-spaces.yaml)。                                                                      |
| [`draw-params.ofd`](draw-params.ofd)           |  1.8K | 1 文档，1 页，10 对象，92 字符，1 字体，3 DrawParam                                           | 绘制参数继承示例；`thick` 继承 `base` 并改线宽，`dashed` 再继承 `thick` 并改为虚线和蓝色描边。生成来源：[`draw-params.yaml`](../../cmd/ofd-creator/examples/draw-params.yaml)。                                                 |
| [`drawparam.ofd`](drawparam.ofd)               |  6.8K | 1 文档，4 页，94 对象，1204 字符，1 字体，12 DrawParam，1 颜色空间                            | 绘制参数示例；覆盖 DrawParam 引用与对象级属性、线宽、Cap、Join、MiterLimit、虚线及颜色，适合验证绘制参数继承和路径描边。                                                                                                        |
| [`font-styles.ofd`](font-styles.ofd)           |  3.6K | 1 文档，2 页，48 对象，1004 字符，4 字体，1 颜色空间                                          | 字体样式矩阵；覆盖 100--900 字重、常规/斜体、字号、HScale、填充/描边文字，以及轴向和径向渐变文字。                                                                                                                              |
| [`hello.ofd`](hello.ofd)                       |  1.4K | 1 文档，1 页，2 对象，21 字符，1 字体                                                         | 最小 Hello World 文档；适合快速验证 OFD 打开、基础文字读取和最小渲染链路。生成来源：[`hello.yaml`](../../cmd/ofd-creator/examples/hello.yaml)。                                                                                 |
| [`helloworld.ofd`](helloworld.ofd)             |  2.3K | 1 文档，2 页，10 对象，42 字符，1 字体                                                        | 基础创建器输出；包含英文和中文文字，以及轴向/径向渐变矩形，适合 converter API、PDF 输出和渐变渲染测试。生成来源：[`helloworld.yaml`](../../cmd/ofd-creator/examples/helloworld.yaml)。                                          |
| [`huawei.ofd`](huawei.ofd)                     |  3.1K | 1 文档，1 页，8 对象，8 路径，0 字符                                                          | Huawei 图标提取结果；由渐变填充的路径和 CTM 组成，包含轴向/径向 Shading，适合测试路径变换、渐变渲染和无文字文档。                                                                                                               |
| [`image-effects.ofd`](image-effects.ofd)       |  2.8K | 1 文档，1 页，6 对象，25 字符，4 图片，1 字体                                                 | 图片效果示例；蓝色小图分别演示圆角边框、ImageMask 半透明蒙版和损坏资源的 Substitution 替代图。生成来源：[`image-effects.yaml`](../../cmd/ofd-creator/examples/image-effects.yaml)。                                             |
| [`intro.ofd`](intro.ofd)                       |  7.5M | 1 文档，42 页，1806 对象，5120 字符，66 图片，12 字体，9 复合图元                             | 较大的真实转换文档；包含大量嵌入字体、图片、路径和复合对象，页面为横向，适合字体修复、图片/矢量渲染、SVG/PDF 转换、性能和文字提取测试。                                                                                         |
| [`kaiti-styles.ofd`](kaiti-styles.ofd)         |  3.0K | 1 文档，2 页，28 对象，451 字符，1 字体                                                       | 楷体排版样例；覆盖字号、粗体、特粗体、斜体、粗斜体、空心描边字和压缩字宽。生成来源：[`kaiti-styles.yaml`](../../cmd/ofd-creator/examples/kaiti-styles.yaml)。                                                                   |
| [`line-styles.ofd`](line-styles.ofd)           |  1.9K | 1 文档，1 页，16 对象，147 字符，1 字体                                                       | 线条样式样例；覆盖 Butt/Round/Square 端点、Miter/Round/Bevel 连接、线宽、虚线、偏移、折线和透明度。生成来源：[`line-styles.json`](../../cmd/ofd-creator/examples/line-styles.json)。                                            |
| [`links.ofd`](links.ofd)                       |  3.4K | 1 文档，2 页，8 对象，51 字符，1 字体，3 注解                                                 | 链接注解示例；第 1 页两个 Link 注解分别跳转到第 2 页和打开 GitHub URI，第 2 页一个，适合测试页面链接提取、Goto/URI 动作和注解跳转。生成来源：[`links.yaml`](../../cmd/ofd-creator/examples/links.yaml)。                        |
| [`media-actions.ofd`](media-actions.ofd)       | 17.4K | 1 文档，1 页，6 对象，56 字符，2 多媒体，1 字体                                               | 声音与影片动作示例；打开文档和进入本页时播放 WAV 提示音，点击蓝色区域重播、点击红色区域播放 MP4 影片。                                                                                                                          |
| [`multi-doc.ofd`](multi-doc.ofd)               |  2.4K | 2 文档，2 页，2 对象，10 字符，2 字体                                                         | 多文档体示例；由 [`multi-doc-a.yaml`](../../cmd/ofd-creator/examples/multi-doc-a.yaml) 和 [`multi-doc-b.yaml`](../../cmd/ofd-creator/examples/multi-doc-b.yaml) 生成后 ZIP 级合并。                                             |
| [`multi_demo.ofd`](multi_demo.ofd)             |  9.1K | 3 文档，4 页，55 对象，530 字符，6 字体，3 DrawParam，3 颜色空间，1 附件                      | 多文档包样例；一个 OFD 包中包含 3 个独立文档，覆盖文档级资源、绘制参数、颜色空间和附件，适合测试文档作用域及跨文档 ID 隔离。                                                                                                    |
| [`package-extras.ofd`](package-extras.ofd)     |  3.3K | 1 文档，1 页，4 对象，60 字符，1 字体，1 附件                                                 | 包结构示例；包含文本附件、自定义标签架构与数据、扩展属性和自定义数据。生成来源：[`package-extras.yaml`](../../cmd/ofd-creator/examples/package-extras.yaml)。                                                                   |
| [`page-area.ofd`](page-area.ofd)               |  1.6K | 1 文档，1 页，6 对象，38 字符，1 字体                                                         | 页面区域示例；同时标出物理框、应用框、内容框和出血框。生成来源：[`page-area.yaml`](../../cmd/ofd-creator/examples/page-area.yaml)。                                                                                             |
| [`path-fill-rules.ofd`](path-fill-rules.ofd)   |  4.6K | 1 文档，3 页，33 对象，317 字符，1 字体                                                       | 路径填充规则对比；覆盖默认规则、`NonZero`、`Even-Odd`、嵌套轮廓、星形、多孔图形、重叠路径，以及渐变填充和对象级 Alpha。生成来源：[`path-fill-rules.json`](../../cmd/ofd-creator/examples/path-fill-rules.json)。                |
| [`pattern-fill.ofd`](pattern-fill.ofd)         | 23.1K | 1 文档，24 页，208 对象，3377 字符，1 字体，24 图案                                           | 图案填充矩阵；棋盘格、圆点、斜纹、镜像斜纹、砖墙和十字编织 6 种单元各配 Normal、Row、Column、RowAndColumn 四种 ReflectMethod，每页对比原始单元与 Pattern 平铺效果。                                                             |
| [`pattern-reflect.ofd`](pattern-reflect.ofd)   |  1.7K | 1 文档，1 页，9 对象，49 字符，1 字体，4 图案                                                 | 底纹翻转示例；同一三角单元按 Normal、Row、Column、RowAndColumn 平铺。生成来源：[`pattern-reflect.yaml`](../../cmd/ofd-creator/examples/pattern-reflect.yaml)。                                                                  |
| [`permissions.ofd`](permissions.ofd)           |  1.7K | 1 文档，1 页，3 对象，32 字符，1 字体                                                         | 权限示例；禁止编辑、批注、水印和截屏，允许导出、签名，打印 2 份，并带有效期。生成来源：[`permissions.yaml`](../../cmd/ofd-creator/examples/permissions.yaml)。                                                                  |
| [`preferences.ofd`](preferences.ofd)           |  2.0K | 1 文档，2 页，6 对象，66 字符，1 字体                                                         | 阅读偏好示例；`PageMode=UseOutlines`、`PageLayout=TwoPageL`、`ZoomMode=FitWidth`。生成来源：[`preferences.yaml`](../../cmd/ofd-creator/examples/preferences.yaml)。                                                             |
| [`project-showcase.ofd`](project-showcase.ofd) | 26.2K | 1 文档，2 页，50 对象，388 字符，1 字体，1 嵌入字体                                           | OFD 项目介绍页；包含文档元数据、背景/正文图层、图形装饰、内嵌字体和可点击 GitHub 跳转，适合测试创建器元数据、图层、嵌入字体及跳转动作。                                                                                         |
| [`radial-ellipse.ofd`](radial-ellipse.ofd)     |  1.6K | 1 文档，1 页，8 对象，29 字符，1 字体                                                         | 椭圆径向渐变示例；对比离心率 0 与 0.75，以及倾斜 0、45、90 度。生成来源：[`radial-ellipse.yaml`](../../cmd/ofd-creator/examples/radial-ellipse.yaml)。                                                                          |
| [`radial-gradient.ofd`](radial-gradient.ofd)   |  4.3K | 1 文档，5 页，30 对象，179 字符，1 字体                                                       | 径向渐变示例；第 1 页覆盖同心、偏心和三色色标，第 2 页为图 36 的 Extend，后 3 页按 GB/T 33190 图 37 分别绘制 Direct、Repeat、Reflect。生成来源：[`radial-gradient.yaml`](../../cmd/ofd-creator/examples/radial-gradient.yaml)。 |
| [`radial_demo.ofd`](radial_demo.ofd)           |  6.8K | 1 文档，5 页，51 对象，537 字符，1 字体，1 颜色空间                                           | 渐变综合样例；前两页是径向渐变的 Eccentricity 与 Angle，第 3 页是轴向渐变的 Extend/MapType，后两页是径向渐变的 Extend 与 MapType。                                                                                              |
| [`shading.ofd`](shading.ofd)                   |  6.1K | 1 文档，6 页，52 对象，332 字符，4 图案                                                       | Shading 综合样例；覆盖 Axial、Radial、Gouraud、LaGouraud 和 Pattern 填充，并包含 Direct/Repeat/Reflect 映射，适合渐变和网格着色回归测试。                                                                                       |
| [`text-directions.ofd`](text-directions.ofd)   |  6.6K | 1 文档，5 页，85 对象，731 字符，1 字体                                                       | 文字方向样例；覆盖 `ReadDirection` 的 0/90/180/270 度和 `CharDirection` 的 0/90 度组合，适合横排、竖排、旋转字形和 `TextCode` 测试。生成来源：[`text-directions.json`](../../cmd/ofd-creator/examples/text-directions.json)。   |
| [`versions.ofd`](versions.ofd)                 |  3.3K | 1 文档，2 页，4 对象，24 字符，1 字体                                                         | 文档版本示例；v1 只包含第一页，当前版本 v2 增加第二页。生成来源：[`versions.yaml`](../../cmd/ofd-creator/examples/versions.yaml)。                                                                                              |
| [`zsbk.ofd`](zsbk.ofd)                         |  1.6M | OFD 1.1，2 页，38 对象，296 字符，3 图片，12 字体，2 模板，2 签名                             | 真实业务文档样例；页面方向混合，包含图片、嵌入字体、模板页和双签名/印章文件，适合测试真实文档解析、模板应用和签名数据读取。                                                                                                     |

## 选取建议

- 最小打开或基础渲染：`hello.ofd`、`helloworld.ofd`
- 字体和文字布局：`font-styles.ofd`、`kaiti-styles.ofd`、`text-directions.ofd`
- 路径、描边和渐变：`drawparam.ofd`、`line-styles.ofd`、`path-fill-rules.ofd`、`radial_demo.ofd`、`radial-gradient.ofd`、`shading.ofd`、`huawei.ofd`
- 颜色空间：`color-spaces.ofd`、`multi_demo.ofd`
- 绘制参数：`drawparam.ofd`、`draw-params.ofd`
- 图片、复合对象和性能：`ano.ofd`、`intro.ofd`
- 图片边框、蒙版和替代图：`image-effects.ofd`
- 模板、附件、注释和签名：`999.ofd`、`zsbk.ofd`
- 页面注解和链接注解：`annotations.ofd`、`links.ofd`
- 文档动作：`actions.ofd`、`media-actions.ofd`
- 文档版本：`versions.ofd`
- 权限和阅读偏好：`permissions.ofd`、`preferences.ofd`
- 附件、自定义标签和扩展：`package-extras.ofd`
- 椭圆径向渐变：`radial-ellipse.ofd`
- 轴向渐变 Extend：`axial-extend.ofd`
- 裁剪区：`clips.ofd`
- 图案填充与底纹翻转：`pattern-fill.ofd`、`pattern-reflect.ofd`
- 页面区域：`page-area.ofd`
- 多文档体：`multi-doc.ofd`、`multi_demo.ofd`
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

刷新本表中的结构摘要可对全部 fixture 重新执行分析：

```text
for f in test/testdata/*.ofd; do go run ./cmd/ofd-analyzer --format json "$f"; done
```

创建器示例的 manifest 位于 [`cmd/ofd-creator/examples`](../../cmd/ofd-creator/examples)。修改这些 manifest 或对应 fixture 时，应执行 `--check`、`--validate` 和必要的严格校验。
