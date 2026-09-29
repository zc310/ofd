package test

import "context"

// ctxTODO 是测试里共用的取消信号。转换入口第一个参数是 context.Context，
// 绝大多数测试并不关心取消，统一在这里给一个 Background。
var ctxTODO = context.Background()
