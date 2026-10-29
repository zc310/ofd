package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/goccy/go-json/v2"

	"github.com/zc310/ofd/internal/server/convertersvc"
)

const (
	// multipartMemory 是 multipart 表单在内存里保留的上限，超出部分由
	// net/http 落到临时文件；随后仍会读进 Input.Bytes，因此这不是内存上限。
	multipartMemory   = 8 << 20
	idempotencyHeader = "Idempotency-Key"
	maxIdempotencyKey = 128
)

// decodeSubmitRequest 支持三种提交体：
//
//	application/json         原有 JSON（含 base64 input.bytes）
//	multipart/form-data      字段 request（JSON）+ 文件 part file/input
//	application/octet-stream 裸文件 + query 参数
//
// 没带 Content-Type 时按 JSON 处理，兼容旧调用方。
func (s *Server) decodeSubmitRequest(r *http.Request) (*submitRequest, error) {
	contentType := r.Header.Get("Content-Type")
	if strings.TrimSpace(contentType) == "" {
		return s.decodeJSON(r)
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, &requestError{kind: "invalid_request", status: http.StatusBadRequest,
			msg: "Content-Type 不合法: " + err.Error()}
	}
	switch mediaType {
	case "multipart/form-data":
		return s.decodeMultipart(r)
	case "application/octet-stream":
		return s.decodeRaw(r)
	default:
		return s.decodeJSON(r)
	}
}

func (s *Server) decodeJSON(r *http.Request) (*submitRequest, error) {
	body, err := readRequestBody(r)
	if err != nil {
		return nil, err
	}
	var request submitRequest
	if err := decodeStrictJSON(body, &request); err != nil {
		return nil, &requestError{kind: "invalid_request", status: http.StatusBadRequest,
			msg: "解析请求体失败: " + err.Error()}
	}
	return &request, nil
}

func (s *Server) decodeMultipart(r *http.Request) (*submitRequest, error) {
	if err := r.ParseMultipartForm(multipartMemory); err != nil {
		if tooLarge(err) {
			return nil, &requestError{kind: "payload_too_large", status: http.StatusRequestEntityTooLarge,
				msg: fmt.Sprintf("请求体超过 %d 字节", s.cfg.MaxUploadBytes)}
		}
		return nil, &requestError{kind: "invalid_request", status: http.StatusBadRequest,
			msg: "解析 multipart 表单失败: " + err.Error()}
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}

	var request submitRequest
	rawRequest := r.FormValue("request")
	if rawRequest != "" {
		if err := decodeStrictJSON([]byte(rawRequest), &request); err != nil {
			return nil, &requestError{kind: "invalid_request", status: http.StatusBadRequest,
				msg: "解析 request 字段失败: " + err.Error()}
		}
	}

	file, header, err := r.FormFile("file")
	if errors.Is(err, http.ErrMissingFile) {
		file, header, err = r.FormFile("input")
	}
	if err == nil {
		defer file.Close()
		data, readErr := io.ReadAll(file)
		if readErr != nil {
			if tooLarge(readErr) {
				return nil, &requestError{kind: "payload_too_large", status: http.StatusRequestEntityTooLarge,
					msg: fmt.Sprintf("请求体超过 %d 字节", s.cfg.MaxUploadBytes)}
			}
			return nil, &requestError{kind: "invalid_request", status: http.StatusBadRequest,
				msg: "读取上传文件失败: " + readErr.Error()}
		}
		request.Input.Kind = convertersvc.InputUpload
		request.Input.Bytes = data
		if request.Input.FileName == "" {
			request.Input.FileName = header.Filename
		}
	} else if !errors.Is(err, http.ErrMissingFile) {
		return nil, &requestError{kind: "invalid_request", status: http.StatusBadRequest,
			msg: "读取 multipart 文件失败: " + err.Error()}
	}

	if rawRequest == "" {
		// 没有 request JSON 时允许完全用 query 描述输出，文件 part 就是内容。
		fromQuery, err := requestFromQuery(r, request.Input.Bytes, request.Input.FileName)
		if err != nil {
			return nil, err
		}
		request = *fromQuery
	}
	return &request, nil
}

func (s *Server) decodeRaw(r *http.Request) (*submitRequest, error) {
	body, err := readRequestBody(r)
	if err != nil {
		return nil, err
	}
	return requestFromQuery(r, body, r.URL.Query().Get("input_file_name"))
}

func readRequestBody(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		if tooLarge(err) {
			return nil, &requestError{kind: "payload_too_large", status: http.StatusRequestEntityTooLarge,
				msg: "请求体超过上传大小限制"}
		}
		return nil, &requestError{kind: "invalid_request", status: http.StatusBadRequest,
			msg: "读取请求体失败"}
	}
	if len(body) == 0 {
		return nil, &requestError{kind: "invalid_request", status: http.StatusBadRequest, msg: "请求体为空"}
	}
	return body, nil
}

func tooLarge(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}

// requestFromQuery 把裸上传的 query 参数变成标准请求体。
//
// query 只覆盖裸上传最短路径需要的字段；复杂通知/远端目标仍建议用 JSON
// 或 multipart 的 request 字段，避免 URL 无限膨胀。
func requestFromQuery(r *http.Request, data []byte, fallbackFileName string) (*submitRequest, error) {
	q := r.URL.Query()
	outputFormat := firstQuery(q.Get("to"), q.Get("output.format"), q.Get("format"))
	if outputFormat == "" {
		return nil, &requestError{kind: "invalid_request", status: http.StatusBadRequest,
			msg: "缺少输出格式：裸上传请用 ?to=pdf 之类的参数指定"}
	}
	request := &submitRequest{
		Input: convertersvc.Input{
			Kind:     convertersvc.InputUpload,
			Format:   firstQuery(q.Get("input_format"), q.Get("input.format")),
			FileName: firstQuery(q.Get("input_file_name"), q.Get("input.file_name"), fallbackFileName),
			Bytes:    data,
		},
		Output: convertersvc.Output{
			Kind:     firstQuery(q.Get("kind"), q.Get("output.kind")),
			Format:   outputFormat,
			FileName: firstQuery(q.Get("file_name"), q.Get("output.file_name")),
			Dir:      firstQuery(q.Get("dir"), q.Get("output.dir")),
			Remote: convertersvc.RemoteOutput{
				Target: firstQuery(q.Get("remote_target"), q.Get("output.remote.target")),
				Path:   firstQuery(q.Get("remote_path"), q.Get("output.remote.path")),
			},
		},
	}
	if target := firstQuery(q.Get("notify_target"), q.Get("notify.target")); target != "" {
		request.Notify = &submitNotify{Target: target, Events: splitList(q.Get("notify_events"))}
	}
	if raw := q.Get("high_priority"); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, &requestError{kind: "invalid_request", status: http.StatusBadRequest,
				msg: "high_priority 必须是 true 或 false"}
		}
		request.HighPriority = value
	}
	return request, nil
}

func firstQuery(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func splitList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// idempotencyKey 读取并校验幂等键。空值表示不启用。
func idempotencyKey(r *http.Request) (string, error) {
	key := strings.TrimSpace(r.Header.Get(idempotencyHeader))
	if key == "" {
		return "", nil
	}
	if len(key) > maxIdempotencyKey {
		return "", &requestError{kind: "invalid_request", status: http.StatusBadRequest,
			msg: fmt.Sprintf("%s 不能超过 %d 字节", idempotencyHeader, maxIdempotencyKey)}
	}
	for _, r := range key {
		if r < 0x21 || r > 0x7e {
			return "", &requestError{kind: "invalid_request", status: http.StatusBadRequest,
				msg: idempotencyHeader + " 只能包含可见 ASCII 字符"}
		}
	}
	return key, nil
}

// requestFingerprint 对规范化后的请求做哈希，用于识别同一个幂等键是否被
// 用于不同请求。用 JSON 而不是原始 body，multipart boundary 与 JSON 空白
// 差异才不会造成假冲突。
//
// 文件内容单独喂给哈希，不参与 JSON 编码：256 MiB 的输入在 base64 后会膨胀
// 成 340 MiB 的字符串，而幂等键本来就是为了让大文件可以安全重试。
func requestFingerprint(request *submitRequest) (string, error) {
	metadata := *request
	metadata.Input.Bytes = nil
	raw, err := json.Marshal(&metadata, json.Deterministic(true))
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write(raw)
	h.Write([]byte{0})
	h.Write(request.Input.Bytes)
	return hex.EncodeToString(h.Sum(nil)), nil
}
