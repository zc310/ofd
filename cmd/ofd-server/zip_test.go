package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hhrutter/tiff"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/server/convertersvc"
	"github.com/zc310/ofd/internal/server/jobstore"
	"github.com/zc310/ofd/internal/server/notify"
	"github.com/zc310/ofd/internal/server/runner"
)

func serverZIPImages(t *testing.T) []byte {
	t.Helper()
	var input bytes.Buffer
	writer := zip.NewWriter(&input)
	for _, name := range []string{"10.tiff", "2.jpg", "1.PNG", "__MACOSX/._1.PNG", "readme.txt"} {
		part, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		switch name {
		case "1.PNG":
			err = png.Encode(part, image.NewRGBA(image.Rect(0, 0, 40, 30)))
		case "2.jpg":
			err = jpeg.Encode(part, image.NewRGBA(image.Rect(0, 0, 60, 30)), nil)
		case "10.tiff":
			err = tiff.EncodeAll(part, []image.Image{
				image.NewRGBA(image.Rect(0, 0, 80, 30)),
				image.NewRGBA(image.Rect(0, 0, 100, 30)),
			}, &tiff.Options{})
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return input.Bytes()
}

func assertServerZIPPages(t *testing.T, data []byte, ocrEnabled bool) {
	t.Helper()
	doc, err := parser.NewOFD(data)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	pages := doc.Documents[0].Pages
	widths := []int{40, 60, 80, 100}
	if len(pages) != len(widths) {
		t.Fatalf("ZIP 转换页数 = %d，期望 %d", len(pages), len(widths))
	}
	items := 1
	if ocrEnabled {
		items = 2
	}
	for i, page := range pages {
		box, err := page.PhysicalBox()
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(box.Width-float64(widths[i])/300*25.4) > 0.001 {
			t.Fatalf("第 %d 页宽度错误: %v", i+1, box.Width)
		}
		if got := len(page.Content().Layer[0].Items); got != items {
			t.Fatalf("第 %d 页图元数 = %d，期望 %d", i+1, got, items)
		}
	}
}

func TestZIPImageUploadsConvertInNaturalOrderWithConfiguredOCR(t *testing.T) {
	input := serverZIPImages(t)
	for _, mode := range []string{"off", "http"} {
		for _, upload := range []string{"json", "raw", "multipart", "async"} {
			t.Run(mode+"/"+upload, func(t *testing.T) {
				s, store, cfg := newTestServer(t)
				var calls atomic.Int32
				remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if _, err := png.Decode(r.Body); err != nil {
						t.Errorf("OCR 请求图片无效: %v", err)
					}
					calls.Add(1)
					fmt.Fprint(w, `{"blocks":[{"text":"hello","x":1,"y":1,"width":20,"height":10,"confidence":99}]}`)
				}))
				defer remote.Close()
				cfg.OCR = OCRConfig{Mode: mode, Endpoint: remote.URL}
				s.convert.DefaultOptions = ocrOptions(cfg.OCR, s.convert.TempDir)
				if upload == "async" {
					deliverer := notify.NewDeliverer(s.registry, remote.Client())
					jobs := runner.New(store, s.convert, deliverer, runner.Config{
						FastWorkers: 1, HeavyWorkers: 1, NotifyWorkers: 1,
						PollInterval: 10 * time.Millisecond, Retention: time.Hour,
					}, testLogger())
					if err := jobs.Start(t.Context()); err != nil {
						t.Fatal(err)
					}
					defer jobs.Stop()
				}
				client := newPipeServer(t, s.Handler())
				kind := "stream"
				if upload == "async" {
					kind = "dir"
				}
				request := submitRequest{
					Input: convertersvc.Input{
						Kind: convertersvc.InputUpload, FileName: "scans.ZIP", Bytes: input,
					},
					Output: convertersvc.Output{Kind: kind, Format: "ofd"},
				}
				var got response
				switch upload {
				case "raw":
					got = doRequest(t, client, http.MethodPost, "/v1/convert?input_format=zip&to=ofd",
						bytes.NewReader(input), map[string]string{"Content-Type": "application/octet-stream"})
				case "multipart":
					var body bytes.Buffer
					writer := multipart.NewWriter(&body)
					part, err := writer.CreateFormFile("file", "scans.ZIP")
					if err != nil {
						t.Fatal(err)
					}
					if _, err := part.Write(input); err != nil {
						t.Fatal(err)
					}
					if err := writer.Close(); err != nil {
						t.Fatal(err)
					}
					got = doRequest(t, client, http.MethodPost, "/v1/convert?to=ofd", &body,
						map[string]string{"Content-Type": writer.FormDataContentType()})
				default:
					body, err := json.Marshal(request)
					if err != nil {
						t.Fatal(err)
					}
					got = do(t, client, http.MethodPost, "/v1/convert", string(body))
				}
				id := got.jobID
				if upload == "async" {
					if got.status != http.StatusAccepted {
						t.Fatalf("异步提交 = %d: %s", got.status, got.body)
					}
					id, _ = got.decode(t)["id"].(string)
					waitZIPJob(t, store, id)
					got = do(t, client, http.MethodGet, "/v1/jobs/"+id+"/content", "")
				}
				if got.status != http.StatusOK {
					t.Fatalf("ZIP 转换 = %d: %s", got.status, got.body)
				}
				assertServerZIPPages(t, []byte(got.body), mode != "off")
				job, err := store.Get(id)
				if err != nil {
					t.Fatal(err)
				}
				wantLane, wantCalls := jobstore.LaneFast, int32(0)
				if mode != "off" {
					wantLane, wantCalls = jobstore.LaneHeavy, 4
				}
				if job.Lane != wantLane || job.FromActual != "zip" || calls.Load() != wantCalls {
					t.Fatalf("任务通道=%s，实际格式=%s，OCR 次数=%d", job.Lane, job.FromActual, calls.Load())
				}
			})
		}
	}
}

func waitZIPJob(t *testing.T, store *jobstore.Store, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err := store.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		switch job.State {
		case jobstore.StateSucceeded:
			return
		case jobstore.StateFailed, jobstore.StateCancelled:
			t.Fatalf("ZIP 异步任务失败: %+v", job)
		case jobstore.StateQueued, jobstore.StateRunning:
			// 等待 worker 完成。
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("ZIP 异步任务未在时限内完成")
}
