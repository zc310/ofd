package ocr

import (
	"context"
	"github.com/goccy/go-json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPClientRecognize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "image/png" {
			t.Errorf("Content-Type = %q, want image/png", got)
		}
		if got := r.Header.Get("X-OCR-Language"); got != "chi_sim+eng" {
			t.Errorf("language = %q, want chi_sim+eng", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want bearer token", got)
		}
		config, err := png.DecodeConfig(r.Body)
		if err != nil {
			t.Errorf("decode uploaded PNG: %v", err)
		} else if config.Width != 20 || config.Height != 10 {
			t.Errorf("uploaded dimensions = %dx%d, want 20x10", config.Width, config.Height)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"width": 20, "height": 10,
			"blocks": []any{
				map[string]any{"text": " hello  world ", "x": 2, "y": 3, "width": 8, "height": 4, "confidence": 98.5},
				map[string]any{"text": "outside", "x": 40, "y": 30, "width": 3, "height": 3, "confidence": 80},
			},
		})
	}))
	defer server.Close()

	input := image.NewRGBA(image.Rect(5, 7, 25, 17))
	blocks, err := NewHTTPClient(server.URL, "test-key", "chi_sim+eng").Recognize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 1 {
		t.Fatalf("blocks = %#v, want clipped outside result", blocks)
	}
	if blocks[0].Text != "hello world" || blocks[0].Bounds != image.Rect(7, 10, 15, 14) || blocks[0].Confidence != 98.5 {
		t.Fatalf("block = %#v", blocks[0])
	}
}

func TestHTTPClientErrors(t *testing.T) {
	tests := []struct {
		name   string
		server http.HandlerFunc
		client *HTTPClient
		want   string
	}{
		{
			name:   "invalid endpoint",
			client: NewHTTPClient("file:///tmp/ocr", "", ""),
			want:   "endpoint",
		},
		{
			name: "service status",
			server: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "unavailable", http.StatusBadGateway)
			},
			want: "502 Bad Gateway",
		},
		{
			name: "invalid JSON",
			server: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("not-json"))
			},
			want: "解析 OCR HTTP 响应",
		},
		{
			name: "dimension mismatch",
			server: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"width":99,"height":10,"blocks":[]}`))
			},
			want: "图片尺寸",
		},
		{
			name: "response too large",
			server: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(strings.Repeat("x", 100)))
			},
			client: &HTTPClient{MaxResponseBytes: 16},
			want:   "大小上限",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := test.client
			if test.server != nil {
				server := httptest.NewServer(test.server)
				defer server.Close()
				if client == nil {
					client = NewHTTPClient(server.URL, "", "")
				} else {
					copy := *client
					copy.Endpoint = server.URL
					client = &copy
				}
			}
			_, err := client.Recognize(context.Background(), image.NewRGBA(image.Rect(0, 0, 10, 10)))
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(test.want)) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestHTTPClientCancellation(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer func() {
		close(release)
		server.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := NewHTTPClient(server.URL, "", "").Recognize(ctx, image.NewRGBA(image.Rect(0, 0, 10, 10)))
		result <- err
	}()
	<-started
	cancel()
	if err := <-result; err != context.Canceled {
		t.Fatalf("Recognize error = %v, want context.Canceled", err)
	}
}
