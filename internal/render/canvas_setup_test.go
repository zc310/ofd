package render_test

// 该外部测试文件空白导入 canvas 后端，使 package render 的测试二进制在启动时
// 完成 canvas 的字体引擎/矢量/PDF/SVG/离屏/栅格后端注册；同时避免 package
// render 内部测试直接导入 backends/canvas 造成 import cycle。
import (
	_ "github.com/zc310/ofd/internal/render/backends/canvas"
)
