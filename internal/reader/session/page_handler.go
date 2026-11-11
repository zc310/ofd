package session

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image/color"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/zc310/ofd/pkg/reader"
)

// PageHandler 为单个会话提供预览图片；不监听网络，由宿主挂载到受信任的资源服务。
// 路径中含随机能力标识，宿主仍须限制导航和不可信内容对处理器的访问。
type PageHandler struct {
	session *Session
	prefix  string
}

// NewPageHandler 创建带随机路径的会话图片处理器。
func NewPageHandler(session *Session) (*PageHandler, error) {
	if session == nil {
		return nil, errors.New("阅读会话为空") //nolint:err113 // 内部构造错误不作为调用方契约。
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, fmt.Errorf("生成页面资源标识: %w", err)
	}
	return &PageHandler{session: session, prefix: "/reader/" + hex.EncodeToString(token[:]) + "/pages/"}, nil
}

// Prefix 返回本会话的页面路径前缀，末尾包含斜杠；后接零起始页码。
func (h *PageHandler) Prefix() string { return h.prefix }

// ServeHTTP 返回指定页的 PNG、JPG 或 SVG。purpose 可指定 export 或 print，
// 相应权限由 Reader 在实际渲染时检查；不指定时只允许预览。
func (h *PageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !strings.HasPrefix(r.URL.Path, h.prefix) {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "只支持 GET", http.StatusMethodNotAllowed)
		return
	}
	pageText := strings.TrimPrefix(r.URL.Path, h.prefix)
	index, err := strconv.Atoi(pageText)
	if err != nil || index < 0 || strconv.Itoa(index) != pageText {
		http.Error(w, "无效页码", http.StatusBadRequest)
		return
	}
	query := r.URL.Query()
	for key, values := range query {
		if (key != "dpi" && key != "format" && key != "background" && key != "purpose") || len(values) != 1 {
			http.Error(w, "无效渲染参数", http.StatusBadRequest)
			return
		}
	}
	dpi := 96.0
	if query.Has("dpi") {
		dpi, err = strconv.ParseFloat(query.Get("dpi"), 64)
		if err != nil || math.IsNaN(dpi) || math.IsInf(dpi, 0) || dpi <= 0 || dpi > 600 {
			http.Error(w, "无效 DPI", http.StatusBadRequest)
			return
		}
	}
	format := query.Get("format")
	contentType := "image/png"
	switch format {
	case "", "png":
		format = "png"
	case "jpg":
		contentType = "image/jpeg"
	case "svg":
		contentType = "image/svg+xml"
		// SVG 也可能被导航为独立文档；限制脚本、外部请求和顶层导航。
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src data:; style-src 'unsafe-inline'; font-src data:")
	default:
		http.Error(w, "不支持的图片格式", http.StatusBadRequest)
		return
	}
	background := color.RGBA{}
	if query.Has("background") {
		value := query.Get("background")
		if len(value) != 9 || value[0] != '#' {
			http.Error(w, "无效背景色", http.StatusBadRequest)
			return
		}
		components, decodeErr := hex.DecodeString(value[1:])
		if decodeErr != nil {
			http.Error(w, "无效背景色", http.StatusBadRequest)
			return
		}
		background = color.RGBA{R: components[0], G: components[1], B: components[2], A: components[3]}
	}
	purpose := query.Get("purpose")
	if purpose != "" && purpose != "export" && purpose != "print" {
		http.Error(w, "无效渲染用途", http.StatusBadRequest)
		return
	}
	var output bytes.Buffer
	options := reader.RenderOptions{DPI: dpi, Format: reader.RenderFormat(format), Background: background,
		Purpose: reader.PermissionAction(purpose), PNGCompressionLevel: 1}
	if err := h.session.RenderPage(r.Context(), &output, index, options); err != nil {
		// 不暴露内部路径等错误细节；出错时不返回底层可能产生的部分图片。
		http.Error(w, "页面渲染失败", http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(output.Bytes())
}
