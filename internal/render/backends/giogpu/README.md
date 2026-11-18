# 实验性 Gio GPU 栅格后端

本包复用 canvas 页面记录，通过 Gio 无头窗口完成 GPU 栅格化，再回读为标准 Go
预乘 RGBA。它是仓库内部原型，不是独立的对外 Go 模块；现有 CLI 不因本包存在
而自动启用 GPU。

## 两种调用方式

- 空白导入本包后，通过 `converter.RasterBackend("giogpu")` 或
  `Document.RasterizePage` 选择注册后端。GPU 失败时记录警告并自动回退 canvas CPU。
- 直接调用 `giogpu.Rasterize(scene, resolution)`：只尝试 GPU，失败返回错误，
  **不会自动回退**。性能基准和必须确认 GPU 执行成功的场景应使用这一入口。

返回图像可以直接交给 `png.Encode` 或 `image/draw`；透明像素已从 GPU 的线性光
预乘表示转换为 Go 的 sRGB 通道预乘表示。完全不透明的像素不做转换。

Gio 与 canvas 的抗锯齿和透明合成模型不同，白底细文字可能显得较浅；本包不提供
文字加深或 Gamma 调节，不能要求两个后端逐像素一致，也不能假定 GPU 一定更快。

路径极细正线宽在栅格化时使用与 canvas CPU 相同的设备下限和低 DPI 预览策略；
换算考虑路径矩阵，并保持原虚线长度与相位。此策略依据 GB/T 33190—2016 表21
的设备线宽要求，低 DPI 按 600 DPI 参考设备换算属于项目预览策略。调整只发生
在输出记录阶段，不改变页面原始路径或 PDF、文字提取结果。

### 半透明遮罩的亮度差异

`intro.ofd` 第 14、18 页等带半透明遮罩的页面，Gio 输出会比默认 CPU 输出更亮。
这是混色空间差异，不是 Alpha 丢失或图片重复预乘：Gio 将 sRGB 解码到线性光空间
再做 Over 合成，而 canvas 默认 `LinearColorSpace` 不转换通道，直接按原通道值混色。
例如白底叠加 `Alpha=102` 的黑色，Gio 灰度约为 203，默认 CPU 为 153。
图片上传转换用于让 RGBA/NRGBA 与 Gio 原生路径遵循同一模型；回读转换只恢复透明
像素的 Go 预乘表示，无法撤销已经完成的不透明合成，也不应修改所有像素来调暗页面。

`TestTranslucentImagesAndPathsUseSameLinearLightCompositing` 用独立公式检查多种背景、
颜色和透明度下的路径及图片，保护这种一致性。需要与默认 CPU/PDF 输出观感一致的
调用方应继续使用 `canvas` 后端；完全匹配需要逐层采用相同混色模型，不是一个全局
Gamma 参数，也不能通过改动源图片的 Alpha 在任意背景上同时做到。

非原生画笔的局部栅格回退现在按实际输出 DPI 采样，不再固定为 150 DPI；每张回退图
仍限制在 2048×2048 像素（RGBA 约 16 MiB），大面积高 DPI 画笔可能受预算限制而降采样。
这只是实现的质量/内存策略，不是 OFD 标准要求；GB/T 33190—2016 §8.3.4.1
规定渐变语义与具体输出设备、处理方式无关。未显式指定密度的其他 Gio 渲染调用仍保留
150 DPI 默认值；可通过渲染器的 `SetRasterResolution` 指定页面像素密度。

图片准备对 RGBA、NRGBA 使用直接行缓冲快路径，对已知不透明格式跳过透明度扫描。
转换不修改共享源图，不按图片或场景指针跨帧缓存，因此后续调用仍能观察源内容变化。

## 回退诊断

`giogpu.Stats()` 返回并发安全的进程级累计快照：

| 字段 | 含义 |
|---|---|
| `GPUFrames` | 成功回读的 GPU 帧数，包括直接调用 `Rasterize` |
| `CPUFallbacks` | 注册后端回退 canvas CPU 的次数 |
| `LastFallback` | 最近回退原因，没有回退时为 `nil` |

例如，仓库内串行转换任务可在前后检查快照：

```go
before := giogpu.Stats()
// 在此调用使用 "giogpu" 的转换或页面渲染，并正常处理其返回错误。
after := giogpu.Stats()
if after.CPUFallbacks > before.CPUFallbacks {
    slog.Warn("本次期间发生 CPU 回退", "error", after.LastFallback)
}
```

并发任务共享这些计数，差值不能归属单个任务。自动回退警告最多每五秒输出一次，
但每次回退都计数并更新原因；警告通过 `slog` 输出，不写入 PNG/JSON 产物。

窗口创建失败的错误可用 `errors.Is(err, giogpu.ErrGPUUnavailable)` 判断，底层错误
仍保留。同一像素尺寸的创建失败缓存五秒，之后重试；尺寸变化可立即重试，避免
超大页面的资源限制阻止普通页面。帧执行和像素回读失败会丢弃窗口，不缓存失败。

## 资源与环境

进程至多缓存一个离屏窗口，同尺寸页面复用，尺寸变化时释放后重建。GPU 操作串行化；
同一后端实例的绘制状态仍不能并发修改。`giogpu.Close()` 可主动归还窗口资源、清除创建失败缓存；
之后仍可重建窗口。诊断累计计数不会清零，页面及文档资源仍由调用方正常关闭。

编译需满足 Gio 在目标平台的构建依赖，运行需可用的无头 GPU 后端。CPU 自动回退
只处理运行时错误，不能弥补缺少编译依赖；软件 GPU 驱动也可能成功创建窗口，
但不代表具备硬件加速或性能优势。

## 验证

```sh
go test ./internal/render/backends/giogpu -count=1
go test -race ./internal/render/backends/giogpu -count=1
go test ./internal/render/backends/giogpu -run '^$' -bench 'BenchmarkRasterizeScene' -benchmem
go test ./internal/render/backends/giogpu -run '^$' -bench '^BenchmarkRasterizeSceneGPUStages$/150dpi/' -benchtime=20x -benchmem
go test ./internal/thirdparty/canvas/renderers/gio -run '^$' -bench '^BenchmarkImagePreparationFormats$' -benchmem
go test ./internal/thirdparty/canvas/renderers/gio -run '^$' -bench '^BenchmarkVectorPathRecording$' -benchmem
go test ./internal/render/backends/giogpu -run '^TestOFDFixturesGPURendering$' -count=1 -v
go test ./internal/render/backends/giogpu -run '^$' -bench '^BenchmarkOFDFixtures$' -benchmem
go test ./internal/thirdparty/canvas -run '^$' -bench '^BenchmarkStrokeAllocation$' -benchmem
```

分阶段基准分别测量 `record`（操作记录）、`submit`（帧提交）、`readback`（同步回读）
和 `normalize`（标准 RGBA 转换）。提交包含驱动工作，不是纯 GPU 内核计时；回读复用
目标缓冲且排除帧提交，归一化排除原始数据恢复。阶段结果不能简单相加来代替完整
`Rasterize` 的端到端耗时，也不包含 PNG 编码。GPU 基准使用严格入口，不会悄悄测到 CPU 回退。

矢量路径记录基准单独覆盖字形轮廓、贝塞尔填充和圆弧。无圆弧的路径只读扫描，
在提交控制点时应用变换，避免复制整条路径及颜色接口装箱；圆弧仍沿用先整体变换、
再转贝塞尔的顺序。这个优化只减少 CPU 操作记录开销，不代表 GPU 帧提交或整页导出
获得同等加速；描边展开和奇偶填充规整仍沿用原有几何算法。

描边的公共几何准备减少了三类分配：`Paths.Merge` 按原始坐标数一次分配、不再
丢弃预分配；扫描线端点队列按每段两个事件预留并几何增长；容差方格插入改为原地
右移，避免嵌套 append 复制尾部。它们同时用于 CPU/PDF 等输出，不改变交点判定、
规整、端点、连接、虚线或曲线近似规则，也没有启用 `FastStroke`。
描边基准覆盖字形轮廓、已分段虚线和曲线；真实页面的收益取决于描边及规整占比，
不能将临时分配下降等同于整页渲染同等提速。

真实文档测试覆盖 `testdata/ofdrw/999.ofd` 的 5 页及 `intro.ofd` 的 42 页，使用
150 DPI、白色背景，记录 GPU/CPU 像素哈希和平均通道差异；尺寸和不透明性必须一致，
但不将两个渲染模型的像素差异作为通过阈值。需要保留两种输出的 PNG 时，显式设置
`OFD_GIO_TEST_OUTPUT=/tmp/ofd/gio-fixtures`；默认不保留预览文件。
基准选择两份文档首页和 intro 第 42 页，分别测量已构建场景的 `record`、包含同步
回读及 RGBA 归一化的 `gpu` 和 `cpu` 光栅化，不包含解析、字体加载、页面构建或 PNG
编码。测硬件加速前须确认驱动设备，不要将 llvmpipe 等软件实现的结果当作显卡性能。

GPU 测试只在最小窗口创建失败时跳过；探测通过后，实际场景、帧执行和回读失败
均使测试失败。窗口创建冷却、诊断计数、接口转发和回读转换另有不依赖 GPU 的测试。
