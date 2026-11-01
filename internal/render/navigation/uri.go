package navigation

import (
	"net/url"

	"github.com/zc310/ofd/internal/models"
)

// URI 按 GB/T 33190—2016 §14.4 表56及 RFC 3986 解析 Base 与相对地址。
// 不可解析的地址不猜测修复；仍由调用方决定允许的协议和打开策略。
func URI(action *models.ActionURI) string {
	if action == nil {
		return ""
	}
	if action.Base == nil || *action.Base == "" || action.URI == "" {
		return action.URI
	}
	reference, err := url.Parse(action.URI)
	if err != nil {
		return ""
	}
	if reference.IsAbs() {
		return action.URI
	}
	base, err := url.Parse(*action.Base)
	if err != nil {
		return ""
	}
	return base.ResolveReference(reference).String()
}
