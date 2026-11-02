# 渲染内部接口

`Document` 持有文档绘制资源；调用者结束使用后必须 `Close`。页面绘制和文字查询均持有
页面租约，模板专属资源与印章使用独立资源作用域，不能按相同 Font ID 借用正文字体。

构造统一使用 `NewDocument(background, doc, dpi)`，`dpi <= 0` 使用默认 96 DPI。
指定分辨率使用 `geom.DPI(...)`；它影响离屏栅格化和替代图像选择，不改变页面物理尺寸。

## 文字查询

新调用统一使用返回 error 的 `QueryTextLayouts(page, TextQueryOptions)` 或
`QueryTextValues(page, TextQueryOptions)`：

- `Visibility` 控制页面图层与模板的可见性。
- `Sources` 控制注解、水印和 OFD 印章等附加来源。
- `RunIndices == nil` 表示全部；非 nil 空切片表示零个 run。
- 选择索引以相同可见性、来源策略下的完整文字流为准；重复和负索引不产生额外结果。
- 即使选择为空也检查文档、页面和加载错误，不再提供丢弃错误的静默查询方法。

## 契约与诊断

后端契约及工厂注册以 `drawing` 为准。颜色和 SVG 场景契约归该层所有，`paint` 和
`imageasset` 依赖契约，而非契约反向依赖资源实现。SVG 场景的串行化保护在资源层完成。
核心旋转使用 `Transform`；路径文字使用 `DrawPath`，不要求后端提供专门的文字路径接口。

异常跳过和可恢复降级进入单次报告，正常隐藏、透明或裁剪为空不作为故障。
颜色缓存命中仍重放本次问题，不把其他页面的报告存入共享解析缓存。数值资源 ID 不存在时
使用 0，而非编造 ID。库内可恢复绘制问题只输出 Debug 日志，不按图元刷 Warn/Error。

`PDFDocument.Close` 幂等，重复调用返回首次关闭结果；关闭后禁止继续加入页面或大纲。
绘制表面放置使用 `layout/image` 的 `CanvasBox` 与 `BoundaryMatrix`，不重复施加源内容 CTM。
坐标与内部/外部空间依据 GB/T 33190—2016 §8.1.3—8.1.5、§13 表50；本次整理不改变放置语义。
