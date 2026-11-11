package main

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/zc310/ofd/pkg/reader"
	"golang.org/x/net/idna"
)

var errExternalLink = errors.New("外部链接只允许无凭据、长度不超过 4096 字节的 HTTP/HTTPS 绝对地址")

func checkedExternalLink(raw string) (string, error) {
	if raw == "" || len(raw) > 4096 || strings.ContainsAny(raw, "\\") || strings.ContainsFunc(raw, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) }) {
		return "", errExternalLink
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errExternalLink
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Opaque != "" || u.User != nil || u.Hostname() == "" || strings.HasSuffix(u.Host, ":") || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errExternalLink
	}
	host := u.Hostname()
	if net.ParseIP(host) == nil {
		host, err = idna.Lookup.ToASCII(host)
		if err != nil || strings.Contains(host, ":") {
			return "", errExternalLink
		}
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n <= 0 || n > 65535 {
			return "", errExternalLink
		}
		u.Host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	} else {
		u.Host = host
	}
	return u.String(), nil
}

// OpenExternalLink 每次由原生对话框确认后交给系统浏览器；不让文档导航宿主 WebView。
func (d *Desktop) OpenExternalLink(ctx context.Context, prefix string, scope int, raw string) (bool, error) {
	return d.openExternalWith(ctx, prefix, scope, raw, func(ctx context.Context, uri string) (bool, error) {
		choice := make(chan bool, 1)
		dialog := d.app.Dialog.Question().SetTitle(d.tr("打开外部链接？")).
			SetMessage(d.tr("此链接来自文档，将由系统浏览器打开，可能访问网络。\n请核对完整地址和域名：\n\n") + uri)
		dialog.AddButton(d.tr("取消")).SetAsCancel().SetAsDefault().OnClick(func() { choice <- false })
		dialog.AddButton(d.tr("在浏览器打开")).OnClick(func() { choice <- true })
		dialog.Show()
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case approved := <-choice:
			return approved, nil
		}
	}, d.app.Browser.OpenURL)
}

func (d *Desktop) openExternalWith(ctx context.Context, prefix string, scope int, raw string, confirm func(context.Context, string) (bool, error), open func(string) error) (bool, error) {
	uri, err := checkedExternalLink(raw)
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := d.document.CheckDocumentPermission(prefix, scope, reader.PermissionAccess); err != nil {
		return false, err
	}
	approved, err := confirm(ctx, uri)
	// 不在确认期间持有 openMu，允许关闭或替换文档；确认后必须再次校验原会话。
	d.openMu.Lock()
	defer d.openMu.Unlock()
	if cancelErr := ctx.Err(); cancelErr != nil {
		return false, cancelErr
	}
	if err != nil || !approved {
		return false, err
	}
	if err := d.document.CheckDocumentPermission(prefix, scope, reader.PermissionAccess); err != nil {
		return false, err
	}
	if err := open(uri); err != nil {
		return false, err
	}
	return true, nil
}
