package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/natefinch/lumberjack/v3"
)

// newLogger 按配置构造日志器。
//
// 落盘用 lumberjack 做轮转：服务是要长跑的，日志文件不切的话迟早把磁盘写满，
// 而磁盘写满的表现是任务静默失败，比服务直接挂掉更难查。
//
// 是否写标准输出由 log_to_stdout 决定，它的默认值跟着 log_dir 走：配了目录
// 就只写文件，不配就只写 stdout。两者同时开的唯一理由是"容器里要收 stdout、
// 同时本地还要留档"。
//
// 同时开时用 io.MultiWriter 扇出：每行日志只序列化一次，再把同样的字节写到
// 两处。用多个 slog.Handler 各写一遍的话，JSON 编码、属性格式化都要做两次。
func newLogger(cfg *Config) (*slog.Logger, func() error, error) {
	level, err := parseLevel(cfg.LogLevel)
	if err != nil {
		return nil, nil, err
	}
	var writers []io.Writer
	// 轮转器持有文件句柄，退出前必须 Close。
	var closers []io.Closer

	if cfg.LogToStdout == nil || *cfg.LogToStdout {
		writers = append(writers, os.Stdout)
	}
	if cfg.LogDir != "" {
		if err := os.MkdirAll(cfg.LogDir, 0o750); err != nil {
			return nil, nil, fmt.Errorf("创建日志目录 %s 失败: %w", cfg.LogDir, err)
		}
		roller, err := newLogFile(cfg)
		if err != nil {
			return nil, nil, err
		}
		writers = append(writers, roller)
		closers = append(closers, roller)
	}
	if len(writers) == 0 {
		// 一个出口都不留的话日志会被静默丢弃，这比报错更难排查。
		return nil, nil, fmt.Errorf("日志没有任何输出目标：请设置 log_dir 或 log_to_stdout")
	}

	var output io.Writer = writers[0]
	if len(writers) > 1 {
		// 顺序写，任一处失败即返回错误；已经写出的内容不受影响。
		output = io.MultiWriter(writers...)
	}
	logger := slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{Level: level}))
	closer := &closeOnce{closers: closers}
	return logger, closer.Close, nil
}

// closeOnce 让退出时的关闭可以重复调用而不报错（defer 与显式调用都会触发）。
//
// 必须是指针接收者：值接收者会拷贝 sync.Once，"只执行一次"的保证就没了。
type closeOnce struct {
	closers []io.Closer
	once    sync.Once
	err     error
}

func (c *closeOnce) Close() error {
	c.once.Do(func() {
		for _, closer := range c.closers {
			if err := closer.Close(); err != nil && c.err == nil {
				c.err = err
			}
		}
	})
	return c.err
}

// newLogFile 构造轮转文件写入器。
//
// v3 换成了 NewRoller + Options 的形式，尺寸上限由构造参数单独传入。注意它的
// 单位是**字节**——v2 的 Logger.MaxSize 字段是 MiB，由 lumberjack 内部换算；
// v3 改成构造参数后不再换算。少乘这一下，maxSize 就等于 MB 数值本身：设 1 是
// 1 字节上限，任何一行日志都超过它，Write 直接返回 ErrWriteTooLong，结果是
// 所有日志被静默丢弃、文件却存在。
func newLogFile(cfg *Config) (io.WriteCloser, error) {
	roller, err := lumberjack.NewRoller(
		filepath.Join(cfg.LogDir, cfg.LogFile),
		int64(cfg.LogMaxSizeMB)<<20, // MiB → 字节
		&lumberjack.Options{
			MaxAge:     time.Duration(cfg.LogMaxAgeDays) * 24 * time.Hour,
			MaxBackups: cfg.LogMaxBackups,
			Compress:   cfg.LogCompress,
			// 归档文件名用本地时间，与日志内容里的时区对得上；默认是 UTC，
			// 排障时对着文件名和内容的时间会错开。
			LocalTime: true,
		})
	if err != nil {
		return nil, fmt.Errorf("初始化日志轮转失败: %w", err)
	}
	return roller, nil
}

func parseLevel(name string) (slog.Level, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return slog.LevelInfo, nil
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(trimmed)); err != nil {
		return 0, fmt.Errorf("日志级别 %q 不合法（可用 debug/info/warn/error）", name)
	}
	return level, nil
}
