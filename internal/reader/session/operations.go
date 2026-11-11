package session

import "context"

func scan[T any](ctx context.Context, s *Session, run func(context.Context) (T, error)) (T, error) { //nolint:ireturn // 保留调用方的具体统计结果类型。
	return limited(ctx, s, s.searches, run)
}

// 所有有界操作共用排队、关闭等待和迟到结果丢弃规则。
func limited[T any](ctx context.Context, s *Session, slots chan struct{}, run func(context.Context) (T, error)) (T, error) { //nolint:ireturn // 保留调用方的具体结果类型。
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if err := s.begin(); err != nil {
		return zero, err
	}
	defer s.active.Done()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel) //nolint:contextcheck // 同时监听会话关闭和调用方取消。
	defer func() { stop(); cancel() }()
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	case <-ctx.Done():
		return zero, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	// AfterFunc 的回调异步执行；不能只依赖派生 ctx，避免关闭后排队操作抢到名额继续读取。
	if err := s.ctx.Err(); err != nil {
		return zero, err
	}
	result, err := run(ctx)
	if cancelErr := ctx.Err(); cancelErr != nil {
		return zero, cancelErr
	}
	if cancelErr := s.ctx.Err(); cancelErr != nil {
		return zero, cancelErr
	}
	return result, err
}
