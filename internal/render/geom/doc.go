// Package geom 是与具体绘制库无关的二维几何与渐变引擎，供 internal/render
// 及其各绘制后端（canvas/gg/ftgg/tinyskia）共用。
//
// 说明：本包的路径子系统（path.go、path_intersection.go、path_stroke.go、
// path_util.go、path_simplify.go、path_tiling.go、path_scanner.go、shapes.go、
// polyline.go、util.go）是从 MIT 许可的 github.com/tdewolff/canvas 路径引擎
// 移植而来，以保证布尔运算（And/Or）、描边展开（Stroke）、Settle 等结果与
// canvas 逐点一致。为便于与上游对照，这些文件保留了上游英文注释；本仓库
// 新增或修改的代码（colors.go、paint.go 等）使用中文注释。
//
// 移植时只删除了依赖渲染分辨率（Resolution）的 ToVectorRasterizer /
// ToScanxScanner，以及两处紧跟 panic 的不可达 continue 和已废弃
// FastClip 的旧实现，其余逻辑未改动。
package geom
