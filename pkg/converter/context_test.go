package converter

import "context"

// ctxTODO 是测试里共用的取消信号。
//
// 转换入口第一个参数是 context.Context，绝大多数测试并不关心取消，统一在这里
// 给一个 Background；真正验证取消行为的用例见 cancel_test.go。
var ctxTODO = context.Background()

// testCancelled 返回一个立即取消的 Context。
func testCancelled() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
