package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/ocr"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/server/convertersvc"
	"github.com/zc310/ofd/internal/server/jobstore"
	"github.com/zc310/ofd/pkg/converter"
)

// TestMain 让当前测试程序按需模拟 Tesseract，不依赖 shell 或系统 OCR 安装。
func TestMain(m *testing.M) {
	if os.Getenv("OFD_SERVER_TEST_TESSERACT") == "1" {
		if err := runTesseractTestHelper(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runTesseractTestHelper() error {
	if len(os.Args) != 8 || !slices.Equal(os.Args[2:], []string{"stdout", "--psm", "3", "-l", "eng", "tsv"}) {
		return fmt.Errorf("Tesseract 参数错误: %q", os.Args)
	}
	path := os.Args[1]
	if dir := os.Getenv("OFD_SERVER_TEST_OCR_TEMP_DIR"); filepath.Dir(path) != dir {
		return fmt.Errorf("OCR 临时文件 %q 不在配置目录 %q 中", path, dir)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}
	if img.Bounds() != image.Rect(0, 0, 120, 80) {
		return fmt.Errorf("OCR 输入图片尺寸错误: %v", img.Bounds())
	}
	_, err = fmt.Fprint(os.Stdout, "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n5\t1\t1\t1\t1\t1\t10\t10\t50\t20\t99\thello\n")
	return err
}

func TestOCRConfigValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		ocr  OCRConfig
		ok   bool
	}{
		{"默认关闭", OCRConfig{}, true},
		{"本机", OCRConfig{Mode: "tesseract"}, true},
		{"远程", OCRConfig{Mode: "http", Endpoint: "http://localhost/ocr"}, true},
		{"未知模式", OCRConfig{Mode: "other"}, false},
		{"缺少地址", OCRConfig{Mode: "http"}, false},
		{"地址含凭据", OCRConfig{Mode: "http", Endpoint: "https://user:pass@example.com"}, false},
		{"负并发", OCRConfig{MaxConcurrency: -1}, false},
		{"负超时", OCRConfig{Timeout: -1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{DBPath: "jobs.db", OutputDir: "out", APIKey: "key", OCR: tc.ocr}
			if err := cfg.validate(); (err == nil) != tc.ok {
				t.Fatalf("validate = %v", err)
			}
		})
	}
}

func TestOCRImageConversionUsesConfiguredBackend(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("X-Ocr-Language") != "eng" {
			t.Errorf("请求头或方法错误: %s %v", r.Method, r.Header)
		}
		if _, err := png.Decode(r.Body); err != nil {
			t.Errorf("PNG 请求无效: %v", err)
		}
		fmt.Fprint(w, `{"width":120,"height":80,"blocks":[{"text":"hello","x":10,"y":10,"width":50,"height":20,"confidence":99}]}`)
	}))
	defer remote.Close()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var input bytes.Buffer
	if err := png.Encode(&input, image.NewRGBA(image.Rect(0, 0, 120, 80))); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		cfg   OCRConfig
		items int
	}{
		{"关闭", OCRConfig{}, 1},
		{"远程", OCRConfig{Mode: "http", Endpoint: remote.URL, APIKey: "secret", Language: "eng"}, 2},
		{"本机", OCRConfig{Mode: "tesseract", TesseractPath: path, Language: "eng"}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := convertersvc.New(t.TempDir(), nil)
			if tc.cfg.Mode == "tesseract" {
				t.Setenv("OFD_SERVER_TEST_TESSERACT", "1")
				t.Setenv("OFD_SERVER_TEST_OCR_TEMP_DIR", svc.TempDir)
			}
			svc.DefaultOptions = ocrOptions(tc.cfg, svc.TempDir)
			result, err := svc.Run(context.Background(), convertersvc.Spec{
				Input:  convertersvc.Input{Kind: convertersvc.InputUpload, FileName: "test.png", Bytes: input.Bytes()},
				Output: convertersvc.Output{Kind: convertersvc.OutputStream, Format: "ofd"},
			})
			if err != nil {
				t.Fatal(err)
			}
			doc, err := parser.NewOFD(result.Bytes)
			if err != nil {
				t.Fatal(err)
			}
			defer doc.Close()
			if got := len(doc.Documents[0].Pages[0].Content().Layer[0].Items); got != tc.items {
				t.Fatalf("项目数 = %d，期望 %d", got, tc.items)
			}
			entries, err := os.ReadDir(svc.TempDir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("转换后残留临时文件: %v", entries)
			}
		})
	}
}

type blockingOCR struct {
	started chan struct{}
	release chan struct{}
}

func (b *blockingOCR) Recognize(ctx context.Context, _ image.Image) ([]ocr.TextBlock, error) {
	b.started <- struct{}{}
	select {
	case <-b.release:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestOCRConcurrencyLimitAndCancelledWait(t *testing.T) {
	backend := &blockingOCR{started: make(chan struct{}, 2), release: make(chan struct{})}
	engine := &limitedOCREngine{engine: backend, slots: make(chan struct{}, 1)}
	done := make(chan error, 1)
	go func() {
		_, err := engine.Recognize(context.Background(), image.NewRGBA(image.Rect(0, 0, 1, 1)))
		done <- err
	}()
	<-backend.started
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := engine.Recognize(ctx, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("等待取消 = %v", err)
	}
	select {
	case <-backend.started:
		t.Fatal("超出并发限制")
	default:
	}
	close(backend.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestOCRAPIKeyEnvironmentOverride(t *testing.T) {
	t.Setenv("OFD_SERVER_OCR_API_KEY", "environment")
	cfg := Config{OCR: OCRConfig{APIKey: "file"}}
	applyEnvOverrides(&cfg)
	if cfg.OCR.APIKey != "environment" {
		t.Fatalf("API key = %q", cfg.OCR.APIKey)
	}
}

func TestOCRImageRequestsUseHeavyLaneForQueueAndInline(t *testing.T) {
	for _, mode := range []string{"tesseract", "http"} {
		for _, extension := range []string{"PNG", "JPG", "TIF", "ZIP"} {
			for _, kind := range []string{"dir", "stream"} {
				t.Run(mode+"/"+extension+"/"+kind, func(t *testing.T) {
					s, store, cfg := newTestServer(t)
					cfg.OCR = OCRConfig{Mode: mode, Endpoint: "https://ocr.example.com"}
					s.convert.DefaultOptions = ocrOptions(cfg.OCR, s.convert.TempDir)
					s.inlineRun = func(context.Context, convertersvc.Spec) (convertersvc.Result, error) {
						if len(s.inlineSemaphore(jobstore.LaneHeavy)) != 1 || len(s.inlineSemaphore(jobstore.LaneFast)) != 0 {
							t.Error("同步 OCR 没有使用 heavy 槽位")
						}
						return convertersvc.Result{Bytes: []byte("ofd"), FileName: "scan.ofd"}, nil
					}
					body, err := json.Marshal(map[string]any{
						"input": convertersvc.Input{
							Kind: convertersvc.InputUpload, FileName: "scan." + extension, Bytes: []byte("image"),
						},
						"output": convertersvc.Output{Kind: kind, Format: "ofd"},
					})
					if err != nil {
						t.Fatal(err)
					}
					got := do(t, newPipeServer(t, s.Handler()), http.MethodPost, "/v1/convert", string(body))
					id := got.jobID
					if kind == "dir" {
						if got.status != http.StatusAccepted {
							t.Fatalf("异步提交状态 = %d: %s", got.status, got.body)
						}
						id, _ = got.decode(t)["id"].(string)
					} else if got.status != http.StatusOK {
						t.Fatalf("同步转换状态 = %d: %s", got.status, got.body)
					}
					job, err := store.Get(id)
					if err != nil {
						t.Fatal(err)
					}
					if job == nil || job.Lane != jobstore.LaneHeavy {
						t.Fatalf("OCR 任务没有记录为 heavy: %+v", job)
					}
				})
			}
		}
	}
}

func TestOCRLocalEngineUsesServiceTempDir(t *testing.T) {
	dir := t.TempDir()
	var conv converter.Converter
	for _, option := range ocrOptions(OCRConfig{Mode: "tesseract"}, dir) {
		option(&conv)
	}
	engine, ok := conv.ImageOCREngine().(*ocr.Tesseract)
	if !ok {
		t.Fatalf("本机引擎类型 = %T", conv.ImageOCREngine())
	}
	if engine.TempDir != dir {
		t.Fatalf("OCR 临时目录 = %q，期望 %q", engine.TempDir, dir)
	}
}
