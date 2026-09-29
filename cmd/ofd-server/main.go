// 命令 ofd-server 提供异步 OFD 转换的 HTTP 服务。
//
//	POST /v1/convert              提交转换，返回 202 与任务 ID
//	GET  /v1/jobs/{id}            查询任务状态
//	POST /v1/jobs/{id}/cancel     取消仍在排队的任务
//	GET  /healthz /readyz         存活与就绪探针
//
// 转换在后台队列里执行，提交请求不会挂起等待——同步等待会把 HTTP 连接数
// 和队列深度绑在一起，一批慢任务就能耗尽连接数。
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/zc310/ofd/internal/convertersvc"
	"github.com/zc310/ofd/internal/jobstore"
	"github.com/zc310/ofd/internal/notify"
	"github.com/zc310/ofd/internal/runner"
	"github.com/zc310/ofd/pkg/converter"
)

func main() {
	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if opts.help {
		fmt.Println(usage)
		return
	}
	if err := run(opts); err != nil {
		fmt.Fprintln(os.Stderr, "ofd-server:", err)
		os.Exit(1)
	}
}

type options struct {
	configPath  string
	checkConfig bool
	help        bool
}

const usage = `用法: ofd-server [flags]

以 HTTP 服务形式提供异步 OFD 转换。

Flags:
  -c, --config <path>   配置文件路径（JSON）
      --check-config    只校验配置，不启动服务
  -h, --help            显示本帮助

未指定 --config 时全部走默认值与 OFD_SERVER_* 环境变量，
但 db_path 与 output_dir 必须给出。`

func parseArgs(args []string) (*options, error) {
	opts := &options{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-h", "--help":
			opts.help = true
		case "--check-config":
			opts.checkConfig = true
		case "-c", "--config":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("%s 需要一个参数", args[i])
			}
			i++
			opts.configPath = args[i]
		default:
			return nil, fmt.Errorf("未知参数 %q\n\n%s", args[i], usage)
		}
	}
	return opts, nil
}

func run(opts *options) error {
	cfg, err := LoadConfig(opts.configPath)
	if err != nil {
		return err
	}
	log, closeLog, err := newLogger(cfg)
	if err != nil {
		return err
	}
	if opts.checkConfig {
		// 日志器也要构造：只校验配置文件会漏掉"日志没有输出目标"这类
		// 只有启动时才会暴露的问题，而这类问题恰恰是 --check-config
		// 存在的意义所在。
		if err := closeLog(); err != nil {
			return err
		}
		fmt.Println("配置有效")
		return nil
	}
	defer func() {
		if err := closeLog(); err != nil {
			log.Error("关闭日志文件失败", "err", err)
		}
	}()

	// 目录先建好再开库：bbolt 会在父目录不存在时直接失败，而这时候报出来的
	// 错误完全看不出是哪个路径。
	dirs := []string{filepath.Dir(cfg.DBPath), cfg.TempDir, cfg.OutputDir}
	if cfg.LogDir != "" {
		dirs = append(dirs, cfg.LogDir)
	}
	for _, dir := range dirs {
		if dir == "" || dir == "." {
			continue
		}
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("创建目录 %s 失败: %w", dir, err)
		}
	}

	store, err := jobstore.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := store.Close(); err != nil {
			log.Error("关闭任务库失败", "err", err)
		}
	}()

	list, err := cfg.Allowlist()
	if err != nil {
		return fmt.Errorf("URL 输入白名单不合法: %w", err)
	}
	if len(cfg.AllowInputURLHosts) == 0 {
		log.Warn("未配置 allow_input_url_hosts，input.kind=url 将被拒绝")
	}

	registry := cfg.NotifyRegistry()
	if len(registry.Names()) == 0 {
		log.Info("未配置通知目标，任务不会发出回调")
	}

	// convertersvc 需要外部程序路径时按配置传入；留空则由转换库自行探测。
	convertOptions := []converter.Option{}
	if cfg.OfficePath != "" {
		convertOptions = append(convertOptions, converter.WithSoffice(cfg.OfficePath))
	}
	if cfg.ChromePath != "" {
		convertOptions = append(convertOptions, converter.WithChrome(cfg.ChromePath))
	}
	if cfg.ChromeNoSandbox {
		convertOptions = append(convertOptions, converter.WithChromeNoSandbox(true))
		log.Warn("Chrome 沙箱已关闭，容器内处理不可信 HTML 时请确保隔离")
	}
	ftpSinks, err := cfg.FTPSinks()
	if err != nil {
		return err
	}
	if len(ftpSinks) == 0 {
		log.Info("未配置 FTP 目标，output.kind=ftp 将被拒绝")
	}
	for name, sink := range ftpSinks {
		// 显式提醒明文 FTP 的风险，而不是让部署方事后才发现。
		if sink.Insecure {
			log.Warn("FTP 目标使用明文传输，账号密码在链路上没有任何保护",
				"target", name, "addr", sink.Addr)
		}
		sink.SetLogger(log)
	}

	s3Sinks := cfg.S3Sinks()
	webdavSinks := cfg.WebDAVSinks()
	if len(s3Sinks) == 0 {
		log.Info("未配置 S3 目标，output.kind=s3 将被拒绝")
	}
	if len(webdavSinks) == 0 {
		log.Info("未配置 WebDAV 目标，output.kind=webdav 将被拒绝")
	}
	for name, sink := range s3Sinks {
		if !sink.Secure {
			log.Warn("S3 目标使用 http，凭据与流量都是明文", "target", name, "endpoint", sink.Endpoint)
		}
		sink.SetLogger(log)
	}
	for _, sink := range webdavSinks {
		sink.SetLogger(log)
	}

	sftpSinks := cfg.SFTPSinks()
	if len(sftpSinks) == 0 {
		log.Info("未配置 SFTP 目标，output.kind=sftp 将被拒绝")
	}
	for name, sink := range sftpSinks {
		// 跳过主机密钥校验意味着连接被劫持时凭据与内容都会泄露，
		// 这种配置值得在启动时喊一声。
		if sink.InsecureIgnoreHostKey {
			log.Warn("SFTP 目标跳过主机密钥校验，连接被劫持时凭据与内容都会泄露",
				"target", name, "addr", sink.Addr)
		}
		sink.SetLogger(log)
	}

	convert := convertersvc.New(cfg.TempDir, list)
	convert.FTPTargets = ftpSinks
	convert.S3Targets = s3Sinks
	convert.WebDAVTargets = webdavSinks
	convert.SFTPTargets = sftpSinks
	convert.URLTimeout = cfg.URLTimeout.Duration()
	convert.DefaultOptions = convertOptions

	// 通知用的出网客户端不跟随重定向：回调地址是我们自己配的，
	// 跟到别处没有好处，还可能把签名头与任务内容带到第三方。
	deliverer := notify.NewDeliverer(registry, &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			MaxIdleConnsPerHost: 4,
			IdleConnTimeout:     90 * time.Second,
		},
	})

	jobRunner := runner.New(store, convert, deliverer, runner.Config{
		FastWorkers:    cfg.FastWorkers,
		HeavyWorkers:   cfg.HeavyWorkers,
		NotifyWorkers:  cfg.NotifyWorkers,
		JobTimeout:     cfg.JobTimeout.Duration(),
		MaxJobAttempts: cfg.MaxJobAttempts,
		Retention:      cfg.Retention.Duration(),
		PruneInterval:  cfg.Retention.Duration() / 24,
	}, log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := jobRunner.Start(ctx); err != nil {
		return err
	}
	defer jobRunner.Stop()

	remote := &RemoteTargets{FTP: ftpSinks, S3: s3Sinks, WebDAV: webdavSinks, SFTP: sftpSinks}
	server := NewServer(cfg, store, registry, list, remote, log)
	// 数据库已经打开、worker 已经起来，就绪探针可以放行。
	// 端口由 fasthttp 内部监听，绑定失败会让 Start 返回错误并终止进程，
	// 所以这里不需要额外的就绪状态机。
	server.SetReadyFunc(func() bool { return true })

	log.Info("ofd-server 启动",
		"listen", cfg.Listen,
		"db", cfg.DBPath,
		"output_dir", cfg.OutputDir,
		"fast_workers", cfg.FastWorkers,
		"heavy_workers", cfg.HeavyWorkers)
	return server.Start(ctx)
}
