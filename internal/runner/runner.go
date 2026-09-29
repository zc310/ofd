// Package runner 是 ofd-server 的常驻编排层：取任务、跑转换、记结果、发通知。
//
// 三个组件各管一段，runner 只负责把它们接起来并控制并发：
//
//	jobstore   任务与通知的持久化队列
//	convertersvc  一次转换的执行
//	notify     状态变化的投递
//
// 并发分两条通道：fast 与 heavy。两者共享一个任务池，但各有独立的 worker 数与
// 令牌桶。原因是 heavy 任务会拉起 LibreOffice 或 Chrome，单个就能吃掉几百 MB
// 内存并占住一个进程；和 fast 任务抢同一组令牌时，几个 Office 文档就能让整个
// 服务对轻量请求失去响应。
package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	json "github.com/goccy/go-json"

	"github.com/zc310/ofd/internal/convertersvc"
	"github.com/zc310/ofd/internal/jobstore"
	"github.com/zc310/ofd/internal/notify"
	"github.com/zc310/ofd/internal/transfer"
)

// DefaultBackoff 是任务失败后的重试退避基数，按 Attempt 指数放大。
const DefaultBackoff = 5 * time.Second

// Converter 是 runner 依赖的转换能力。*convertersvc.Service 直接满足它。
//
// 提成接口是为了能注入受控实现做并发压测：通道隔离、退避时序、关闭时的
// 在途任务处置，这些都要有"能拖多久就拖多久"的慢任务才测得出来，而真实的
// OFD 转换快到没法构造这种场景。
type Converter interface {
	Run(ctx context.Context, spec convertersvc.Spec) (convertersvc.Result, error)
}

// Config 是 runner 的配置。
type Config struct {
	// FastWorkers 是快速通道的并发数。
	FastWorkers int
	// HeavyWorkers 是重通道的并发数。
	HeavyWorkers int
	// PollInterval 是队列为空时的轮询间隔。
	PollInterval time.Duration
	// NotifyWorkers 是通知投递的并发数。
	NotifyWorkers int
	// JobTimeout 是单次转换的整体超时，0 表示不限。
	JobTimeout time.Duration
	// MaxJobAttempts 是任务自动重试次数，0 表示只试一次。
	MaxJobAttempts int
	// Backoff 是重试退避基数，0 时用 DefaultBackoff。
	Backoff time.Duration
	// PruneInterval 是清理终态任务的间隔，小于等于 0 表示不清理。
	PruneInterval time.Duration
	// Retention 是终态任务的保留时长。
	Retention time.Duration
	// NotifyBatch 是一次最多取多少条到期通知，0 时用 32。
	NotifyBatch int
}

// withDefaults 补齐零值。
func (c *Config) withDefaults() {
	if c.FastWorkers <= 0 {
		c.FastWorkers = 4
	}
	if c.HeavyWorkers <= 0 {
		c.HeavyWorkers = 1
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 500 * time.Millisecond
	}
	if c.NotifyWorkers <= 0 {
		c.NotifyWorkers = 4
	}
	if c.Backoff <= 0 {
		c.Backoff = DefaultBackoff
	}
	if c.NotifyBatch <= 0 {
		c.NotifyBatch = 32
	}
}

// Runner 驱动任务与通知。
type Runner struct {
	store     *jobstore.Store
	convert   Converter
	deliverer *notify.Deliverer
	cfg       Config
	log       *slog.Logger

	// sem 是每条通道的令牌。heavy 单独一组，互不挤占。
	fastSem  chan struct{}
	heavySem chan struct{}

	wg     sync.WaitGroup
	cancel context.CancelFunc
	closed sync.Once
}

// New 构造 runner。
func New(store *jobstore.Store, convert Converter, deliverer *notify.Deliverer, cfg Config, log *slog.Logger) *Runner {
	cfg.withDefaults()
	if log == nil {
		log = slog.Default()
	}
	return &Runner{
		store:     store,
		convert:   convert,
		deliverer: deliverer,
		cfg:       cfg,
		log:       log,
		fastSem:   make(chan struct{}, cfg.FastWorkers),
		heavySem:  make(chan struct{}, cfg.HeavyWorkers),
	}
}

// Start 启动 worker 并做崩溃恢复。
//
// Recover 与 RecoverInflight 必须先跑：进程上次退出时留在 running / inflight 的
// 记录，对外部而言都是"卡住了"，不重新排队就永远不会有结果。
func (r *Runner) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	if n, err := r.store.Recover(); err != nil {
		return fmt.Errorf("恢复任务失败: %w", err)
	} else if n > 0 {
		r.log.Info("已把中断的任务重新排队", "count", n)
	}
	if n, err := r.store.RecoverInflight(); err != nil {
		return fmt.Errorf("恢复通知失败: %w", err)
	} else if n > 0 {
		r.log.Info("已把中断的通知重新排队", "count", n)
	}

	for i := 0; i < r.cfg.FastWorkers; i++ {
		r.wg.Add(1)
		go r.jobLoop(ctx, jobstore.LaneFast, r.fastSem)
	}
	for i := 0; i < r.cfg.HeavyWorkers; i++ {
		r.wg.Add(1)
		go r.jobLoop(ctx, jobstore.LaneHeavy, r.heavySem)
	}
	for i := 0; i < r.cfg.NotifyWorkers; i++ {
		r.wg.Add(1)
		go r.notifyLoop(ctx)
	}
	if r.cfg.PruneInterval > 0 {
		r.wg.Add(1)
		go r.pruneLoop(ctx)
	}
	return nil
}

// Stop 停止所有 worker 并等待它们退出。已取到的任务会被写回失败态，
// 因为 ctx 已经取消，转换不可能再完成。
func (r *Runner) Stop() {
	r.closed.Do(func() {
		if r.cancel != nil {
			r.cancel()
		}
		r.wg.Wait()
	})
}

// jobLoop 持续从一条通道取任务。
func (r *Runner) jobLoop(ctx context.Context, lane jobstore.Lane, sem chan struct{}) {
	defer r.wg.Done()
	for {
		if ctx.Err() != nil {
			return
		}
		// 先占令牌再取任务。反过来会在队列积压时把任务全取走压在内存里，
		// 令牌也就失去限流意义了。
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		job, err := r.store.Claim(lane)
		if err != nil {
			<-sem
			if ctx.Err() != nil {
				return
			}
			r.log.Error("取任务失败", "lane", lane, "err", err)
			sleep(ctx, r.cfg.PollInterval)
			continue
		}
		if job == nil {
			<-sem
			sleep(ctx, r.cfg.PollInterval)
			continue
		}
		r.execute(ctx, job)
		<-sem
	}
}

// execute 跑一个任务并记录终态。
func (r *Runner) execute(ctx context.Context, job *jobstore.Job) {
	log := r.log.With("job", job.ID, "lane", job.Lane)
	runCtx := ctx
	if r.cfg.JobTimeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, r.cfg.JobTimeout)
		defer cancel()
	}
	spec, err := specFromJob(job)
	if err != nil {
		// 请求本身不合法，重试多少次都一样。
		log.Warn("任务参数不合法", "err", err)
		r.finish(job, jobstore.StateFailed, jobstore.Output{}, err.Error())
		return
	}
	result, err := r.convert.Run(runCtx, spec)
	if err != nil {
		if errors.Is(err, context.Canceled) && ctx.Err() != nil {
			// 服务正在关闭，不算这个任务失败。
			//
			// 写成 failed 会同时做两件错事：任务状态变成终态、给调用方发一条
			// "context canceled" 的失败通知——而重启后这个任务本来会正常跑完。
			// 正确做法是把任务留在 running，由下次启动的 Recover 重新排队。
			log.Info("服务关闭，任务留待重启后重试", "job", job.ID, "attempt", job.Attempt)
			return
		}
		log.Warn("转换失败", "attempt", job.Attempt, "err", err)
		r.retryOrFail(job, err)
		return
	}
	if ctx.Err() != nil {
		// 转换刚成功但服务同时在关闭：不写终态，避免和关停流程抢。
		// 结果已经产出了，重启后重试会再跑一遍。
		log.Info("服务关闭，已完成的结果不落终态", "job", job.ID)
		return
	}
	log.Info("转换完成", "format", result.Format, "took_ms", result.TookMs)
	// 用量先记账再落终态：记账失败只记错误日志，不能因此把一个已经
	// 成功、产物已经在磁盘上的任务判成 failed。
	if err := r.store.RecordUsage(job.ID, result.InputFormat, result.InputBytes); err != nil {
		log.Error("记录输入用量失败", "job", job.ID, "err", err)
	}
	r.finish(job, jobstore.StateSucceeded, jobstore.Output{
		Kind: result.Kind,
		Path: outputPath(result),
		Size: totalSize(result),
		// 文件名清单要让调用方知道产物叫什么。逐页输出会有多项，只报目录
		// 的话调用方得自己猜名字或去列目录。
		Files: fileNames(result),
	}, "")
}

// retryOrFail 判断该重试还是直接判死。
//
// 区分的依据是"同样的输入再来一次会不会有不同结果"：参数错误、超限、格式不支持
// 都不会；进程崩溃、超时、网络抖动、磁盘瞬时不足会。所以只看错误类型，
// 不看次数之外的东西。
func (r *Runner) retryOrFail(job *jobstore.Job, cause error) {
	if retryableJob(cause) && job.Attempt < r.cfg.MaxJobAttempts {
		// 退避靠 DueAt，而不是 sleep：sleep 会占住一个 worker，
		// 一批任务同时失败时等于把整个通道停掉。
		next := *job
		next.State = jobstore.StateQueued
		next.Error = ""
		next.Output = jobstore.Output{}
		next.StartedAt = time.Time{}
		next.DueAt = time.Now().Add(r.backoff(job.Attempt))
		if err := r.store.Enqueue(&next); err != nil {
			r.log.Error("重试入队失败", "job", job.ID, "err", err)
			r.finish(job, jobstore.StateFailed, jobstore.Output{}, cause.Error())
			return
		}
		r.log.Info("任务已按退避重新排队", "job", job.ID, "attempt", next.Attempt, "retry_at", next.DueAt)
		return
	}
	r.finish(job, jobstore.StateFailed, jobstore.Output{}, cause.Error())
}

// retryableJob 报告这个失败是否值得重试。
//
// 判据是"同样的输入再来一次会不会有不同结果"。参数错误、格式不支持、超限都
// 属于请求本身的问题，重试多少次结果都一样，立刻判死；其余（超时、崩溃、
// 网络抖动、磁盘瞬时不足）都值得再试一次。
func retryableJob(cause error) bool {
	switch {
	case errors.Is(cause, convertersvc.ErrBadRequest),
		errors.Is(cause, convertersvc.ErrUnsupported),
		errors.Is(cause, convertersvc.ErrTooLarge),
		// 目标已存在：重试多少次都还是已存在。而 output.dir 允许指向共享
		// 目录（调用方主动放弃任务隔离），同名冲突因此是常见失败，不该
		// 白白耗掉 3 次转换与退避。
		errors.Is(cause, transfer.ErrExists):
		return false
	default:
		return true
	}
}

func (r *Runner) backoff(attempt int) time.Duration {
	delay := r.cfg.Backoff << (attempt - 1)
	if delay > 10*time.Minute {
		delay = 10 * time.Minute
	}
	return delay
}

// finish 写入终态并排队通知。
func (r *Runner) finish(job *jobstore.Job, state jobstore.State, output jobstore.Output, failure string) {
	if err := r.store.Finish(job.ID, state, output, failure); err != nil {
		r.log.Error("写终态失败", "job", job.ID, "err", err)
		return
	}
	r.scheduleNotify(job, state, output, failure)
}

// scheduleNotify 排一条状态通知。没配通知目标就什么都不做。
func (r *Runner) scheduleNotify(job *jobstore.Job, state jobstore.State, output jobstore.Output, failure string) {
	if job.NotifyTarget == "" || r.deliverer == nil {
		return
	}
	event := notify.EventSucceeded
	if state != jobstore.StateSucceeded {
		event = notify.EventFailed
	}
	payload := map[string]any{
		"job_id":  job.ID,
		"state":   string(state),
		"event":   event,
		"output":  output,
		"attempt": job.Attempt,
	}
	if failure != "" {
		payload["error"] = failure
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		r.log.Error("序列化通知失败", "job", job.ID, "err", err)
		return
	}
	// 通知 ID 用 job ID + 事件，重试任务时不会因为重新排队而重复通知。
	delivery := jobstore.Delivery{
		ID:          job.ID + ":" + event,
		JobID:       job.ID,
		Target:      job.NotifyTarget,
		Event:       event,
		Payload:     raw,
		DueAt:       time.Now(),
		MaxAttempts: 5,
	}
	if err := r.store.ScheduleDeliveries([]jobstore.Delivery{delivery}); err != nil {
		r.log.Error("通知入队失败", "job", job.ID, "err", err)
	}
}

// notifyLoop 持续投递到期通知。
func (r *Runner) notifyLoop(ctx context.Context) {
	defer r.wg.Done()
	for {
		if ctx.Err() != nil {
			return
		}
		now := time.Now()
		claimed, err := r.store.ClaimDueDeliveries(now, r.cfg.NotifyBatch)
		if err != nil {
			if ctx.Err() == nil {
				r.log.Error("取通知失败", "err", err)
			}
			sleep(ctx, r.cfg.PollInterval)
			continue
		}
		if len(claimed) == 0 {
			sleep(ctx, r.cfg.PollInterval)
			continue
		}
		for _, item := range claimed {
			r.deliverOne(ctx, item)
		}
	}
}

func (r *Runner) deliverOne(ctx context.Context, item jobstore.Delivery) {
	if r.deliverer == nil {
		return
	}
	err := r.deliverer.Deliver(ctx, notify.Request{
		Target:  item.Target,
		ID:      item.ID,
		JobID:   item.JobID,
		Event:   item.Event,
		Payload: item.Payload,
		Attempt: item.Attempts,
	})
	switch {
	case err == nil:
		if finishErr := r.store.CompleteDelivery(item.ID); finishErr != nil {
			r.log.Error("标记通知完成失败", "delivery", item.ID, "err", finishErr)
		}
	case notify.Retryable(err):
		// 指数退避， Attempts 越大等得越久。
		delay := r.backoff(item.Attempts)
		if failErr := r.store.FailDelivery(item.ID, err.Error(), delay); failErr != nil {
			r.log.Error("标记通知失败状态出错", "delivery", item.ID, "err", failErr)
		}
	default:
		// 不可重试：立刻判死，别再占用重试配额。
		if failErr := r.store.FailDelivery(item.ID, err.Error(), 0); failErr != nil {
			r.log.Error("放弃通知出错", "delivery", item.ID, "err", failErr)
		}
	}
}

// pruneLoop 定期清理终态任务。
func (r *Runner) pruneLoop(ctx context.Context) {
	defer r.wg.Done()
	ticker := time.NewTicker(r.cfg.PruneInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-r.cfg.Retention)
			if r.cfg.Retention <= 0 {
				cutoff = time.Now()
			}
			if n, err := r.store.Prune(cutoff); err != nil {
				r.log.Error("清理任务失败", "err", err)
			} else if n > 0 {
				r.log.Info("已清理终态任务", "count", n)
			}
			if n, err := r.store.PruneDeliveries(cutoff); err != nil {
				r.log.Error("清理通知失败", "err", err)
			} else if n > 0 {
				r.log.Info("已清理终态通知", "count", n)
			}
		}
	}
}

// specFromJob 从任务记录还原转换请求。
func specFromJob(job *jobstore.Job) (convertersvc.Spec, error) {
	if len(job.Request) == 0 {
		return convertersvc.Spec{}, fmt.Errorf("%w: 任务缺少请求体", convertersvc.ErrBadRequest)
	}
	var request struct {
		Input  convertersvc.Input  `json:"input"`
		Output convertersvc.Output `json:"output"`
	}
	if err := json.Unmarshal(job.Request, &request); err != nil {
		return convertersvc.Spec{}, fmt.Errorf("%w: 请求体解析失败: %v", convertersvc.ErrBadRequest, err)
	}
	if request.Output.Format == "" {
		request.Output.Format = job.To
	}
	return convertersvc.Spec{Input: request.Input, Output: request.Output}, nil
}

func outputPath(result convertersvc.Result) string {
	if result.Kind == convertersvc.OutputStream {
		return result.FileName
	}
	return result.Dir
}

// fileNames 提取产物的文件名清单。
//
// stream 输出的名字在 FileName 里，dir 与远端输出在 Files 里；两者都取，
// 免得调用方要按 kind 分支。
func fileNames(result convertersvc.Result) []string {
	if len(result.Files) > 0 {
		names := make([]string, 0, len(result.Files))
		for _, file := range result.Files {
			names = append(names, file.Name)
		}
		return names
	}
	if result.FileName != "" {
		return []string{result.FileName}
	}
	return nil
}

func totalSize(result convertersvc.Result) int64 {
	if result.Kind == convertersvc.OutputStream {
		return int64(len(result.Bytes))
	}
	var sum int64
	for _, file := range result.Files {
		sum += file.Size
	}
	return sum
}

func sleep(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
