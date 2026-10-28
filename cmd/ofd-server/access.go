package main

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"time"
)

// requestIDHeader 是请求 ID 的响应头，也接受调用方在请求里传入同名头。
const requestIDHeader = "X-Request-Id"

// statusWriter 记录状态码与响应字节数，供 access log 使用。
//
// Unwrap 让 http.NewResponseController 能穿透这层包装设置写 deadline；
// 少了它，所有经过包装的响应都会失去流式写超时保护。
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += n
	return n, err
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// ReadFrom 保留底层的 io.ReaderFrom。少了它，大产物下载会从 sendfile/零拷贝
// 退化成 32 KiB 的用户态缓冲区拷贝；状态码与字节数在这里同步维护。
func (w *statusWriter) ReadFrom(r io.Reader) (int64, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := io.Copy(w.ResponseWriter, r)
	w.bytes += int(n)
	return n, err
}

// requestID 复用调用方提供的合法 ID，否则生成一个随机 ID。
//
// 复用让网关或调用方的链路追踪能串起来；但长度和字符集必须受限，
// 否则它会被原样写回响应头与日志，CR/LF 会变成响应头注入。
func requestID(r *http.Request) string {
	if id := r.Header.Get(requestIDHeader); validRequestID(id) {
		return id
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return hex.EncodeToString(raw[:])
	}
	// crypto/rand 在现代系统上不可用的可能性极低；退回到时间戳也仍比空值好。
	return "req-" + strings.ReplaceAll(time.Now().UTC().Format("20060102T150405.000000000"), ".", "")
}

func validRequestID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	// 只排除空白、控制字符与高位字节。可见 ASCII 对响应头和日志都安全，
	// 而链路追踪系统常用的 base64/十六进制 ID 不会被无谓地替换掉。
	for i := 0; i < len(id); i++ {
		if id[i] < 0x21 || id[i] > 0x7e {
			return false
		}
	}
	return true
}
