// Package allowlist 为主机名与 IP 地址提供拉取白名单，供 ofd-server 使用。
//
// 服务会按调用方给的 URL 拉取待转换的文档，也会按预注册的地址投递通知。这两个
// 方向如果不做限制，调用方就能让服务去请求内网地址、云元数据端点或本机端口，
// 等于把服务变成跳板。这里把策略集中在一处，入站拉取与出站通知共用。
package allowlist

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"

	"golang.org/x/net/publicsuffix"
)

// translationPrefixes 是把 IPv4 装进 IPv6 的保留前缀，以及已废弃但仍可能被路由的
// 站点本地网段。走这些前缀可以借道 IPv4 触达本机或内网，例如用 64:ff9b::/96
// (NAT64) 指向 127.0.0.1。
var translationPrefixes = func() []netip.Prefix {
	raw := []string{
		"64:ff9b::/96",   // NAT64
		"64:ff9b:1::/48", // 本地网络使用的 NAT64
		"2002::/16",      // 6to4，内嵌 IPv4
		"2001::/32",      // Teredo，内嵌可路由的 IPv4
		"fec0::/10",      // 站点本地（已废弃但仍可路由）
	}
	prefixes := make([]netip.Prefix, 0, len(raw))
	for _, item := range raw {
		if prefix, err := netip.ParsePrefix(item); err == nil {
			prefixes = append(prefixes, prefix)
		}
	}
	return prefixes
}()

// List 是一份主机名白名单。零值不可用：不含任何条目时 EverythingAllowed 返回
// false，即"默认拒绝"。
//
// 条目形如 "cdn.example.com"（精确）、"*.corp.example.com"（子域通配，仅匹配一级以上
// 的子域，不匹配 "corp.example.com" 本身）或 "10.20.0.0/16"（CIDR，也接受单个 IP）。
type List struct {
	hosts    []string
	suffixes []string
	nets     []netip.Prefix
	singleIP []netip.Addr
}

// Parse 解析白名单条目。空串与空白项被忽略，非法条目返回错误——配置写错应当在启动
// 时失败，而不是运行时静默放行或静默拒绝。
func Parse(entries []string) (*List, error) {
	list := &List{}
	for _, raw := range entries {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		if strings.HasPrefix(entry, "*.") {
			suffix := strings.ToLower(strings.TrimPrefix(entry, "*"))
			if err := checkWildcardSuffix(suffix); err != nil {
				return nil, fmt.Errorf("通配条目无效: %q: %w", raw, err)
			}
			list.suffixes = append(list.suffixes, suffix)
			continue
		}
		if strings.Contains(entry, "/") {
			prefix, err := netip.ParsePrefix(entry)
			if err != nil {
				return nil, fmt.Errorf("CIDR 条目无效: %q: %w", raw, err)
			}
			list.nets = append(list.nets, prefix.Masked())
			continue
		}
		if addr, err := netip.ParseAddr(entry); err == nil {
			list.singleIP = append(list.singleIP, addr.Unmap())
			continue
		}
		if !validHostname(entry) {
			return nil, fmt.Errorf("主机名条目无效: %q", raw)
		}
		list.hosts = append(list.hosts, strings.ToLower(entry))
	}
	return list, nil
}

// checkWildcardSuffix 校验通配后缀必须是一个可注册域名，而不能只是公共后缀。
// "*.example.com" 约束到 example.com 之下的子域，可用；"*.com" 与 "*.co.uk"
// 分别是整个 TLD 和整个 .co.uk 公共后缀，等于没有约束力，必须拒绝。
func checkWildcardSuffix(suffix string) error {
	domain := strings.TrimPrefix(suffix, ".")
	if domain == "" {
		return fmt.Errorf("通配后缀为空")
	}
	// PublicSuffix 返回的公共后缀若与域名本身相同，说明该"域名"就是公共后缀。
	// icann 为假表示不是 ICANN 正式管理的后缀（例如自定义内部域），仍按同一规则处理。
	publicSuffix, _ := publicsuffix.PublicSuffix(domain)
	if publicSuffix == domain {
		return fmt.Errorf("通配后缀只是公共后缀，约束不到具体站点")
	}
	return nil
}

// validHostname 粗筛主机名：允许字母、数字、点、连字符，不允许空标签或以点开头结尾。
// 不追求完整实现，只挡掉明显不是主机名的输入。
func validHostname(name string) bool {
	if len(name) > 253 || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for _, r := range label {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			default:
				return false
			}
		}
	}
	return true
}

// Empty 报告白名单是否为空。为空时 EverythingAllowed 恒为 false。
func (l *List) Empty() bool {
	return l == nil || (len(l.hosts) == 0 && len(l.suffixes) == 0 &&
		len(l.nets) == 0 && len(l.singleIP) == 0)
}

// EverythingAllowed 报告白名单是否处于"不限制"状态。空白名单即不限制外部访问，
// 服务据此在启动时给出告警并在请求时直接拒绝这类操作。
func (l *List) EverythingAllowed() bool { return l.Empty() }

// AllowedHost 判断主机名是否在白名单内。CIDR 与单个 IP 条目在此不参与匹配，它们
// 作用于解析后的地址（见 AllowAddr）。
func (l *List) AllowedHost(host string) bool {
	if l == nil {
		return false
	}
	name := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if name == "" {
		return false
	}
	// 字面量 IP 交给 AllowAddr 判断，AllowedHost 不放行。
	if _, err := netip.ParseAddr(name); err == nil {
		return false
	}
	for _, exact := range l.hosts {
		if name == exact {
			return true
		}
	}
	for _, suffix := range l.suffixes {
		// "*.example.com" 匹配 "a.example.com" 与 "a.b.example.com"，但不匹配
		// "example.com" 本身，也不匹配 "notexample.com"（后缀含前导点可避免）。
		if strings.HasSuffix(name, suffix) && len(name) > len(suffix) {
			return true
		}
	}
	return false
}

// AllowAddr 判断一个解析后的地址是否可访问。默认拒绝一切非公网单播地址：回环、
// 私有网段、链路本地（含 169.254.169.254 云元数据）、未指定、多播。
//
// 注意：这是"不得命中敏感网段"的黑名单式判断，不是"必须在公网白名单内"。白名单条目
// 里的 CIDR 用来在受控内网中放行特定网段（例如内部对象存储），因此
// AllowedHost 为真并不能单独成为放行依据，两者要同时满足。
func (l *List) AllowAddr(addr netip.Addr) error {
	if !addr.IsValid() {
		return fmt.Errorf("地址无效")
	}
	addr = addr.Unmap()
	// 白名单显式列出的网段或地址优先。
	for _, prefix := range l.nets {
		if prefix.Contains(addr) {
			return nil
		}
	}
	for _, single := range l.singleIP {
		if single == addr {
			return nil
		}
	}
	// 这些网段 netip 的判定方法没有覆盖，但都是实际可路由或可转换到 IPv4 的敏感
	// 地址，是绕过白名单的常见手段，必须显式拒绝。
	for _, prefix := range translationPrefixes {
		if prefix.Contains(addr) {
			return fmt.Errorf("地址位于 %s（IPv4 转换/站点本地网段）", prefix)
		}
	}
	switch {
	case addr.IsLoopback():
		return fmt.Errorf("回环地址")
	case addr.IsPrivate():
		return fmt.Errorf("私有网段")
	case addr.IsLinkLocalUnicast(), addr.IsLinkLocalMulticast():
		return fmt.Errorf("链路本地地址")
	case addr.IsUnspecified():
		return fmt.Errorf("未指定地址")
	case addr.IsMulticast():
		return fmt.Errorf("多播地址")
	}
	return nil
}

// AllowResolved 校验一组解析结果：必须全部通过 AllowAddr，且至少有一个地址可用。
// 只要有一个地址指向敏感网段就整体拒绝，避免"多数解析结果正常"掩盖恶意记录。
func (l *List) AllowResolved(addrs []netip.Addr) error {
	if len(addrs) == 0 {
		return fmt.Errorf("没有解析到地址")
	}
	for _, addr := range addrs {
		if err := l.AllowAddr(addr); err != nil {
			return fmt.Errorf("%s 不可访问: %w", addr, err)
		}
	}
	return nil
}

// ResolveAndCheck 按白名单校验 host 的解析结果，返回可用的地址列表。调用方应当把
// 校验放在真正拨号的那一刻（自定义 net.Dialer.DialContext）执行，否则检查与连接之间
// 存在时间窗，攻击者可用 DNS rebinding 在窗口内把域名改指到内网。
func (l *List) ResolveAndCheck(host string) ([]netip.Addr, error) {
	name := strings.TrimSpace(host)
	if name == "" {
		return nil, fmt.Errorf("主机名为空")
	}
	if addr, err := netip.ParseAddr(name); err == nil {
		if err := l.AllowAddr(addr); err != nil {
			return nil, fmt.Errorf("%s 不可访问: %w", name, err)
		}
		return []netip.Addr{addr.Unmap()}, nil
	}
	if !l.AllowedHost(name) {
		return nil, fmt.Errorf("主机不在白名单: %s", name)
	}
	ips, err := net.DefaultResolver.LookupNetIP(context.Background(), "ip", name)
	if err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", name, err)
	}
	addrs := make([]netip.Addr, 0, len(ips))
	for _, ip := range ips {
		addrs = append(addrs, ip.Unmap())
	}
	if err := l.AllowResolved(addrs); err != nil {
		return nil, err
	}
	return addrs, nil
}

// Describe 返回条目数量，供启动日志与 /healthz 展示，不泄露条目内容。
func (l *List) Describe() string {
	if l.Empty() {
		return "空（禁止一切外部访问）"
	}
	total := len(l.hosts) + len(l.suffixes) + len(l.nets) + len(l.singleIP)
	return fmt.Sprintf("%d 条", total)
}
