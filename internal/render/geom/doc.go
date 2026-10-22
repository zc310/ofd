// Package geom 提供渲染核心与各后端共用的几何类型及OFD绘制规则。
// Path、Matrix、Point、Rect及描边类型是github.com/zc310/canvas的类型别名，
// 路径变换、裁剪、描边展开均复用该依赖，不在本仓库维护第二套路径引擎。
//
// 颜色、画笔和分辨率规则由本包维护：渐变保留OFD的非预乘插值及透明色标，
// Paint区分未指定颜色与显式透明，MinimumRasterStrokeWidth处理设备线宽下限。
// 各后端共享几何类型，绘制与字体引擎仍通过drawing接口注册。
package geom
