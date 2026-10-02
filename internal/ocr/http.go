package ocr

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/goccy/go-json"

	"github.com/zc310/ofd/internal/media"
)

const defaultMaxResponseBytes int64 = 8 << 20

// HTTPClient 把图片发送到实现 OFD 通用 OCR HTTP 协议的服务。
// 请求体是 PNG，X-OCR-Language 指定语言；响应 JSON 的 blocks 使用左上角原点像素坐标。
type HTTPClient struct {
	Endpoint         string
	APIKey           string
	Language         string
	Client           *http.Client
	MaxResponseBytes int64
}

// NewHTTPClient 创建远程 OCR 引擎。
func NewHTTPClient(endpoint, apiKey, language string) *HTTPClient {
	return &HTTPClient{
		Endpoint: endpoint,
		APIKey:   apiKey,
		Language: language,
	}
}

type httpResponse struct {
	Width  int `json:"width"`
	Height int `json:"height"`
	Blocks []struct {
		Text       string  `json:"text"`
		X          int     `json:"x"`
		Y          int     `json:"y"`
		Width      int     `json:"width"`
		Height     int     `json:"height"`
		Confidence float64 `json:"confidence"`
	} `json:"blocks"`
}

// Recognize 将 PNG 图片发送至远程服务并解析文字块结果。
func (h *HTTPClient) Recognize(ctx context.Context, input image.Image) ([]TextBlock, error) {
	if input == nil {
		return nil, fmt.Errorf("OCR 输入图片为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	endpoint := strings.TrimSpace(h.Endpoint)
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("OCR HTTP endpoint 必须是有效的 http 或 https URL")
	}

	var body bytes.Buffer
	if err := media.EncodePNG(&body, input); err != nil {
		return nil, fmt.Errorf("编码 OCR 请求图片失败: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), &body)
	if err != nil {
		return nil, fmt.Errorf("创建 OCR HTTP 请求失败: %w", err)
	}
	request.Header.Set("Content-Type", "image/png")
	request.Header.Set("Accept", "application/json")
	language := strings.TrimSpace(h.Language)
	if language == "" {
		language = "chi_sim+eng"
	}
	request.Header.Set("X-OCR-Language", language)
	if key := strings.TrimSpace(h.APIKey); key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}

	client := h.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("调用 OCR HTTP 服务失败: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message, readErr := io.ReadAll(io.LimitReader(response.Body, 4096))
		if readErr != nil {
			return nil, fmt.Errorf("OCR HTTP 服务返回状态 %s（读取错误响应失败: %w）", response.Status, readErr)
		}
		return nil, fmt.Errorf("OCR HTTP 服务返回状态 %s: %s", response.Status, strings.TrimSpace(string(message)))
	}

	limit := h.MaxResponseBytes
	if limit <= 0 {
		limit = defaultMaxResponseBytes
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("读取 OCR HTTP 响应失败: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("OCR HTTP 响应超过大小上限 %d 字节", limit)
	}
	var decoded httpResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, fmt.Errorf("解析 OCR HTTP 响应失败: %w", err)
	}
	bounds := input.Bounds()
	if (decoded.Width != 0 && decoded.Width != bounds.Dx()) || (decoded.Height != 0 && decoded.Height != bounds.Dy()) {
		return nil, fmt.Errorf("OCR HTTP 响应图片尺寸 %dx%d 与输入尺寸 %dx%d 不一致", decoded.Width, decoded.Height, bounds.Dx(), bounds.Dy())
	}
	blocks := make([]TextBlock, 0, len(decoded.Blocks))
	for _, block := range decoded.Blocks {
		blocks = append(blocks, TextBlock{
			Text:       block.Text,
			Bounds:     image.Rect(bounds.Min.X+block.X, bounds.Min.Y+block.Y, bounds.Min.X+block.X+block.Width, bounds.Min.Y+block.Y+block.Height),
			Confidence: block.Confidence,
		})
	}
	return Normalize(blocks, bounds), nil
}
