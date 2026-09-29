package main

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/valyala/fasthttp"

	"github.com/zc310/ofd/internal/convertersvc"
	"github.com/zc310/ofd/internal/jobstore"
)

// inlineWaitLimit 是等待同步转换槽位的上限。
//
// 满了就立刻返回 503 而不是继续排：调用方是来拿结果的，让它挂在 HTTP 连接上
// 等不如让它重试——积压时排队的请求最终会一起超时，而 503 加 Retry-After 至少
// 让客户端能做出正确反应。
const inlineWaitLimit = 5 * time.Second

// 同步转换的响应头。名字会被 fasthttp 规范化，见 apiKeyHeader 处的说明。
const (
	jobIDHeader = "X-OFD-Job-Id"
	tookHeader  = "X-OFD-Took-Ms"
)

// handleConvertInline 处理 stream 输出：转换在本次请求内完成，产物直接作为
// 响应体返回。
//
// 为什么 stream 必须同步而不是走队列：它的产物只在内存里，不落盘。异步模型的
// 前提是结果有个地方等着被取，而内存里的结果出了这次请求就没了——任务照样记为
// succeeded、size 照样报得出来，但谁也拿不到内容。转换被完整执行一遍，产出为零。
//
// 转换不在 worker 池里跑，由本请求直接执行，用信号量限制并发。换来的是客户端
// 断连能直接取消转换（用的就是请求的 context），不必再起一个监视协程；代价是
// 不与异步任务共用 worker 池，因此信号量按各通道的 worker 数配置——两种请求
// 互相限制不了对方的 CPU 占用。
//
// 任务记录照常写：/v1/stats 的转换量统计与 GET /v1/jobs/{id} 都不会漏掉同步
// 转换，否则走 stream 的调用在统计里是隐形的。
func (s *Server) handleConvertInline(ctx *fasthttp.RequestCtx, request *submitRequest) {
	lane := jobstore.LaneFast
	if convertersvc.IsHeavy(request.Input, request.Output.Format) {
		lane = jobstore.LaneHeavy
	}

	job, err := s.buildJob(request)
	if err != nil {
		writeError(ctx, statusForRequestError(err), codeForRequestError(err), err.Error())
		return
	}

	sem := s.inlineSemaphore(lane)
	select {
	case sem <- struct{}{}:
		defer func() { <-sem }()
	case <-ctx.Done():
		// 等槽位时客户端就走了，没必要再记一条任务。
		return
	case <-time.After(inlineWaitLimit):
		ctx.Response.Header.Set("Retry-After", "5")
		writeError(ctx, fasthttp.StatusServiceUnavailable, "server_busy",
			"同步转换并发已满，请稍后重试；或去掉 output.kind 的 stream 走异步提交")
		return
	}

	if err := s.store.RecordDirect(job); err != nil {
		s.log.Error("写入同步任务记录失败", "job", job.ID, "err", err)
		writeError(ctx, fasthttp.StatusInternalServerError, "internal_error", "写入任务记录失败")
		return
	}
	s.log.Info("同步转换开始", "job", job.ID, "lane", lane, "to", job.To)

	// 请求的 context 直接作为转换的 context：客户端一断开，转换立刻停。
	//
	// job_timeout 为 0 表示不限，此时不套 WithTimeout——WithTimeout(ctx, 0)
	// 会立刻超时，把所有转换都变成失败。
	var runCtx context.Context = ctx
	cancel := func() {}
	if limit := s.cfg.JobTimeout.Duration(); limit > 0 {
		runCtx, cancel = context.WithTimeout(ctx, limit)
	}
	defer cancel()

	result, runErr := s.convert.Run(runCtx, convertersvc.Spec{
		Input:  request.Input,
		Output: request.Output,
	})
	if runErr != nil {
		s.finishInline(ctx, job, runErr)
		if ctx.Err() != nil {
			// 客户端已经不在了，再写响应没有意义。
			s.log.Info("同步转换因客户端断开而中止", "job", job.ID, "err", runErr)
			return
		}
		writeError(ctx, statusForRequestError(runErr), codeForRequestError(runErr), runErr.Error())
		return
	}

	if err := s.store.RecordUsage(job.ID, result.InputFormat, result.InputBytes); err != nil {
		s.log.Error("记录输入用量失败", "job", job.ID, "err", err)
	}
	if err := s.store.Finish(job.ID, jobstore.StateSucceeded, jobstore.Output{
		Kind: result.Kind, Path: result.FileName, Size: int64(len(result.Bytes)),
		Files: []string{result.FileName},
	}, ""); err != nil {
		s.log.Error("写终态失败", "job", job.ID, "err", err)
	}
	s.log.Info("同步转换完成", "job", job.ID, "format", result.Format,
		"took_ms", result.TookMs, "bytes", len(result.Bytes))

	// 任务 ID 放进响应头：调用方已经拿到内容，但之后仍可凭它查记录或对账。
	// 头名在链路上是 X-Ofd-Job-Id / X-Ofd-Took-Ms：fasthttp 会把自定义头规范化，
	// 不保留缩写全大写。见 apiKeyHeader 处的说明。
	ctx.Response.Header.Set(jobIDHeader, job.ID)
	ctx.Response.Header.Set(tookHeader, strconv.FormatInt(result.TookMs, 10))
	ctx.SetStatusCode(fasthttp.StatusOK)
	if result.MIME != "" {
		ctx.SetContentType(result.MIME)
	} else {
		ctx.SetContentType("application/octet-stream")
	}
	if name := headerSafe(result.FileName); name != "" {
		ctx.Response.Header.Set("Content-Disposition", `attachment; filename="`+name+`"`)
	}
	ctx.SetBody(result.Bytes)
}

// finishInline 把结果写进任务记录，让统计里的成功数与失败数包含同步转换。
//
// 客户端主动断开导致的取消记为 cancelled 而不是 failed：那不是转换失败，
// 混在一起会让失败率虚高——尤其在有人写脚本轮询大文件转换时。
func (s *Server) finishInline(ctx context.Context, job *jobstore.Job, cause error) {
	state := jobstore.StateFailed
	if ctx.Err() != nil || errors.Is(cause, context.Canceled) {
		state = jobstore.StateCancelled
	}
	if err := s.store.Finish(job.ID, state, jobstore.Output{}, cause.Error()); err != nil {
		s.log.Error("写终态失败", "job", job.ID, "err", err)
	}
}

// inlineSemaphore 返回某通道的同步转换信号量。
//
// 容量取该通道的 worker 数：同步转换不共用 worker 池，这个信号量就是它自己的
// 并发上限。跟着配置走，配置改了上限也跟着变。
func (s *Server) inlineSemaphore(lane jobstore.Lane) chan struct{} {
	s.inlineOnce.Do(func() {
		size := func(n int) int {
			if n < 1 {
				return 1
			}
			return n
		}
		s.inlineFast = make(chan struct{}, size(s.cfg.FastWorkers))
		s.inlineHeavy = make(chan struct{}, size(s.cfg.HeavyWorkers))
	})
	if lane == jobstore.LaneHeavy {
		return s.inlineHeavy
	}
	return s.inlineFast
}

// headerSafe 清掉不能进 HTTP 头的字符。
//
// 文件名由服务从格式注册表拼出（"output" + 扩展名），当前不来自调用方；但它
// 要进 Content-Disposition，而头里的 CR/LF 会被对端当作响应头分隔符
// （HTTP 响应拆分）。所以即便来源可控也过滤掉，成本是三行。
func headerSafe(name string) string {
	return strings.NewReplacer("\r", "", "\n", "", `"`, "").Replace(name)
}
