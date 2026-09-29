package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// logConfig 造一份走过 applyDefaults 的配置。
//
// 直接写 Config 字面量会漏掉默认值（log_file 为空会让轮转器拿不到文件名，
// log_max_size_mb 为 0 会直接构造失败），而生产路径必然经过 LoadConfig →
// applyDefaults。测试走同一条路径才测的是真实行为。
func logConfig(t *testing.T, dir string, level string) *Config {
	t.Helper()
	cfg := &Config{
		LogLevel: level, LogDir: dir,
		LogMaxSizeMB: 1, LogMaxBackups: 3, LogMaxAgeDays: 1,
	}
	cfg.applyDefaults()
	// applyDefaults 只补零值，这里把测试想要的非默认值固定住。
	cfg.LogMaxSizeMB = 1
	return cfg
}

// readLogLines 读回日志文件的每一行。
func readLogLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读日志失败: %v", err)
	}
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("日志行不是合法 JSON: %q", line)
		}
		records = append(records, record)
	}
	return records
}

// 配置了 log_dir 就应当真的落盘，并且目录能自动创建。
func TestLoggerWritesToFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs", "nested")
	cfg := logConfig(t, dir, "debug")
	cfg.LogFile = "svc.log"
	log, closeLog, err := newLogger(cfg)
	if err != nil {
		t.Fatal(err)
	}
	log.Info("已受理", "job", "j1", "lane", "fast")
	log.Debug("调试信息", "n", 1)
	if err := closeLog(); err != nil {
		t.Fatalf("关闭日志失败: %v", err)
	}

	records := readLogLines(t, filepath.Join(dir, "svc.log"))
	if len(records) != 2 {
		t.Fatalf("应有 2 行日志，实际 %d", len(records))
	}
	if records[0]["msg"] != "已受理" || records[0]["job"] != "j1" {
		t.Errorf("第一条日志内容不对: %v", records[0])
	}
	if records[0]["level"] != "INFO" {
		t.Errorf("level = %v", records[0]["level"])
	}
	if records[0]["time"] == nil {
		t.Error("缺少时间戳")
	}
	// 级别过滤要生效。
	if records[1]["level"] != "DEBUG" {
		t.Errorf("debug 级别应被记录: %v", records[1])
	}
}

func TestLoggerLevelFiltering(t *testing.T) {
	dir := t.TempDir()
	log, closeLog, err := newLogger(logConfig(t, dir, "warn"))
	if err != nil {
		t.Fatal(err)
	}
	log.Debug("不该出现")
	log.Info("也不该出现")
	log.Warn("应当出现")
	log.Error("也应当出现")
	if err := closeLog(); err != nil {
		t.Fatal(err)
	}
	records := readLogLines(t, filepath.Join(dir, "ofd-server.log"))
	if len(records) != 2 {
		t.Fatalf("应只剩 2 行，实际 %d: %v", len(records), records)
	}
	if records[0]["level"] != "WARN" || records[1]["level"] != "ERROR" {
		t.Errorf("级别不对: %v", records)
	}
}

// 未配 log_dir 时只写 stdout，不该凭空造出文件。
func TestLoggerWithoutDirHasNoFile(t *testing.T) {
	dir := t.TempDir()
	cfg := logConfig(t, "", "info")
	cfg.LogToStdout = boolPtr(true) // 没配 log_dir 时本来就默认 true，这里写明意图
	// 收进管道，别让这行日志混进测试输出。
	captureStdout(t, func() {
		var innerErr error
		log, closeLog, err := newLogger(cfg)
		if err != nil {
			innerErr = err
			return
		}
		log.Info("只走 stdout")
		innerErr = closeLog()
		if err != nil {
			t.Fatalf("关闭日志失败: %v", err)
		}
		if innerErr != nil {
			t.Fatal(innerErr)
		}
	})
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("未配 log_dir 时不该产生文件: %v", entries)
	}
}

// 配了 log_dir 就默认不写 stdout：每行日志序列化一次、写两次既没有额外信息，
// 也白花 I/O。
func TestLogDirSuppressesStdoutByDefault(t *testing.T) {
	dir := t.TempDir()
	cfg := logConfig(t, dir, "info")
	if cfg.LogToStdout == nil || *cfg.LogToStdout {
		t.Fatalf("配了 log_dir 时 LogToStdout 应默认 false，实际 %v", cfg.LogToStdout)
	}
	stdout := captureStdout(t, func() {
		log, closeLog, err := newLogger(cfg)
		if err != nil {
			t.Fatal(err)
		}
		log.Info("只应落文件", "job", "j1")
		if err := closeLog(); err != nil {
			t.Fatal(err)
		}
	})
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("stdout 不该有内容，实际 %q", stdout)
	}
	// 文件里必须真的有。
	records := readLogLines(t, filepath.Join(dir, "ofd-server.log"))
	if len(records) != 1 || records[0]["msg"] != "只应落文件" {
		t.Errorf("文件里应有 1 行，实际 %v", records)
	}
}

// 两处内容必须逐字节一致：MultiWriter 只序列化一次，不该出现格式差异。
func TestFanoutWritesIdenticalBytes(t *testing.T) {
	dir := t.TempDir()
	cfg := logConfig(t, dir, "info")
	cfg.LogToStdout = boolPtr(true)
	stdout := captureStdout(t, func() {
		log, closeLog, err := newLogger(cfg)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			log.Info("同一行", "i", i)
		}
		if err := closeLog(); err != nil {
			t.Fatal(err)
		}
	})
	fromFile, err := os.ReadFile(filepath.Join(dir, "ofd-server.log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(fromFile) != stdout {
		t.Errorf("两处内容应逐字节相同:\n文件: %q\nstdout: %q", fromFile, stdout)
	}
}

// stdout 与文件同时开时，两边都要拿到同样的内容。
func TestLoggerFansOutToStdoutAndFile(t *testing.T) {
	dir := t.TempDir()
	stdout := captureStdout(t, func() {
		cfg := logConfig(t, dir, "info")
		cfg.LogToStdout = boolPtr(true)
		log, closeLog, err := newLogger(cfg)
		if err != nil {
			t.Fatal(err)
		}
		log.Info("两处都应出现", "job", "j1")
		if err := closeLog(); err != nil {
			t.Fatal(err)
		}
	})
	records := readLogLines(t, filepath.Join(dir, "ofd-server.log"))
	if len(records) != 1 {
		t.Fatalf("文件里应有 1 行，实际 %d", len(records))
	}
	var fromStdout map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &fromStdout); err != nil {
		t.Fatalf("stdout 不是合法 JSON: %q", stdout)
	}
	if fromStdout["msg"] != "两处都应出现" || fromStdout["job"] != "j1" {
		t.Errorf("stdout 内容不对: %v", fromStdout)
	}
}

// 一个出口都不留必须报错：日志被静默丢弃比启动失败更难排查。
func TestLoggerRequiresAtLeastOneTarget(t *testing.T) {
	cfg := &Config{LogLevel: "info", LogToStdout: boolPtr(false)}
	cfg.applyDefaults()
	_, _, err := newLogger(cfg)
	if err == nil {
		t.Fatal("没有任何输出目标时应报错")
	}
	if !strings.Contains(err.Error(), "输出目标") {
		t.Errorf("错误信息应说明原因: %v", err)
	}
}

func TestLoggerRejectsBadLevel(t *testing.T) {
	cfg := &Config{LogLevel: "verbose", LogToStdout: boolPtr(false)}
	cfg.applyDefaults()
	if _, _, err := newLogger(cfg); err == nil {
		t.Error("非法级别应报错")
	}
}

func TestLoggerRejectsUnusableLogDir(t *testing.T) {
	// 用一个已存在的文件当目录，日志目录创建会失败。
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{LogLevel: "info", LogDir: file}
	cfg.applyDefaults()
	if _, _, err := newLogger(cfg); err == nil {
		t.Error("日志目录不可创建时应报错")
	}
}

// closeLog 必须幂等：defer 与显式调用都会触发，重复调用不该报错。
func TestCloseLogIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	_, closeLog, err := newLogger(logConfig(t, dir, "info"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := closeLog(); err != nil {
			t.Fatalf("第 %d 次关闭应成功: %v", i+1, err)
		}
	}
}

// 并发写日志：轮转器的文件句柄不是并发安全的，fanoutHandler 必须加锁。
func TestLoggerConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	log, closeLog, err := newLogger(logConfig(t, dir, "info"))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	const writers, perWriter = 8, 20
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				log.Info("并发写", "writer", w, "seq", i)
			}
		}(w)
	}
	wg.Wait()
	if err := closeLog(); err != nil {
		t.Fatal(err)
	}
	records := readLogLines(t, filepath.Join(dir, "ofd-server.log"))
	if len(records) != writers*perWriter {
		t.Errorf("日志行数 = %d，期望 %d", len(records), writers*perWriter)
	}
}

// 轮转：超过单文件上限后应切出新文件，历史文件受 max_backups 限制。
func TestLoggerRotates(t *testing.T) {
	dir := t.TempDir()
	cfg := logConfig(t, dir, "info")
	cfg.LogFile = "rot.log"
	// 保留 2 个历史文件，便于观察轮转结果。
	cfg.LogMaxBackups = 2
	log, closeLog, err := newLogger(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// 每行塞足数据，很快写满一个文件。
	blob := strings.Repeat("x", 64<<10)
	for i := 0; i < 100; i++ { // 约 6.4 MiB
		log.Info("轮转", "i", i, "blob", blob)
	}
	if err := closeLog(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var backups int
	for _, entry := range entries {
		if entry.IsDir() {
			t.Errorf("不该产生子目录: %s", entry.Name())
		}
		if entry.Name() != "rot.log" {
			backups++
		}
	}
	if backups == 0 {
		t.Fatalf("应产生过轮转文件，目录内容: %v", names(entries))
	}
	// max_backups=2 生效，但只在"最终"生效：lumberjack 的清理是异步的
	// （mill 在 goroutine 里跑，且在创建新备份之前执行），所以轮转密集时
	// 备份数会暂时超过上限。断言的是收敛，不是瞬时值。
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		backups = 0
		for _, entry := range entries {
			if entry.Name() != "rot.log" {
				backups++
			}
		}
		if backups <= 2 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("历史文件 %d 个，始终未收敛到 max_backups=2: %v", backups, names(entries))
}

// 单文件上限必须真的起作用：lumberjack 的 maxSize 以 MiB 为单位，
// 传错单位会导致"永远不轮转"或"疯狂轮转"。
func TestLogRotationRespectsSize(t *testing.T) {
	dir := t.TempDir()
	cfg := logConfig(t, dir, "info")
	cfg.LogFile = "size.log"
	// 只测切分，不限制历史文件数量。
	cfg.LogMaxBackups = 0
	cfg.LogMaxAgeDays = 0
	log, closeLog, err := newLogger(cfg)
	if err != nil {
		t.Fatal(err)
	}
	blob := strings.Repeat("y", 64<<10)
	for i := 0; i < 100; i++ { // 约 6.4 MiB
		log.Info("数据", "i", i, "blob", blob)
	}
	if err := closeLog(); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	rotated := 0
	for _, entry := range entries {
		if entry.Name() != "size.log" {
			rotated++
		}
	}
	// 6.4 MiB / 1 MiB 应该切出若干个文件；上限单位错成字节就会只有一个。
	if rotated < 3 {
		t.Errorf("1 MiB 上限下应切出多个文件，实际切了 %d 个: %v", rotated, names(entries))
	}
}

func TestDefaultsForLogFile(t *testing.T) {
	cfg := &Config{}
	cfg.applyDefaults()
	if cfg.LogFile != "ofd-server.log" {
		t.Errorf("默认日志文件名 = %q", cfg.LogFile)
	}
	if cfg.LogMaxSizeMB != 100 || cfg.LogMaxBackups != 10 || cfg.LogMaxAgeDays != 30 {
		t.Errorf("轮转默认值不对: %d %d %d", cfg.LogMaxSizeMB, cfg.LogMaxBackups, cfg.LogMaxAgeDays)
	}
	// 没配 log_dir：只写 stdout。
	if cfg.LogToStdout == nil || !*cfg.LogToStdout {
		t.Errorf("未配 log_dir 时默认应写 stdout，实际 %v", cfg.LogToStdout)
	}

	// 配了 log_dir：默认只写文件，不重复写 stdout。
	withDir := &Config{LogDir: t.TempDir()}
	withDir.applyDefaults()
	if withDir.LogToStdout == nil || *withDir.LogToStdout {
		t.Errorf("配了 log_dir 时默认不该再写 stdout，实际 %v", withDir.LogToStdout)
	}

	// 显式值不被默认值覆盖，两个方向都要覆盖。
	for _, explicit := range []bool{true, false} {
		cfg := &Config{LogDir: t.TempDir(), LogToStdout: &explicit}
		cfg.applyDefaults()
		if *cfg.LogToStdout != explicit {
			t.Errorf("显式设为 %v 却被改成 %v", explicit, *cfg.LogToStdout)
		}
	}
}

func boolPtr(v bool) *bool { return &v }

func names(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Name())
	}
	return out
}

// captureStdout 临时把 os.Stdout 换成管道，收集 fn 写出的内容。
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = writer
	done := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(reader)
		done <- string(data)
	}()
	fn()
	writer.Close()
	os.Stdout = original
	return <-done
}
