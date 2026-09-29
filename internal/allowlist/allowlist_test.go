package allowlist

import (
	"net/netip"
	"strings"
	"testing"
)

func mustParse(t *testing.T, entries ...string) *List {
	t.Helper()
	list, err := Parse(entries)
	if err != nil {
		t.Fatalf("Parse(%v) 失败: %v", entries, err)
	}
	return list
}

func TestParseRejectsInvalidEntries(t *testing.T) {
	for _, entry := range []string{
		"10.0.0.0/99",  // CIDR 越界
		"a..b",         // 空标签
		".example.com", // 前导点
		"example.com.", // 尾随点
		"exa mple.com", // 空格
		"*.com",        // 只约束到 TLD，等于放行整个 .com
		"*.co.uk",      // 同样只约束到公共后缀
		"*.",           // 空后缀
	} {
		if _, err := Parse([]string{entry}); err == nil {
			t.Errorf("Parse(%q) 应当报错", entry)
		}
	}
	if _, err := Parse([]string{"  ", ""}); err != nil {
		t.Errorf("空白条目应被忽略而不是报错: %v", err)
	}
}

func TestEmptyListRejectsEverything(t *testing.T) {
	for _, list := range []*List{nil, {}, mustParse(t)} {
		if !list.EverythingAllowed() {
			t.Errorf("空白名单应报告不限制: %+v", list)
		}
		if list.AllowedHost("example.com") {
			t.Errorf("空白名单不应放行任何主机: %+v", list)
		}
	}
	if got := mustParse(t, "cdn.example.com").Describe(); !strings.Contains(got, "1 条") {
		t.Errorf("Describe = %q", got)
	}
	if got := (&List{}).Describe(); !strings.Contains(got, "禁止") {
		t.Errorf("空名单 Describe = %q，应说明禁止一切外部访问", got)
	}
}

func TestAllowedHostExactAndWildcard(t *testing.T) {
	list := mustParse(t, "CDN.Example.com", "*.corp.example.com", "10.20.0.0/16")
	cases := []struct {
		host string
		want bool
		why  string
	}{
		{"cdn.example.com", true, "大小写不敏感的精确匹配"},
		{"cdn.example.com.", true, "尾随点等价"},
		{"a.corp.example.com", true, "一级子域"},
		{"a.b.corp.example.com", true, "多级子域"},
		{"corp.example.com", false, "*. 不匹配裸域"},
		{"notcorp.example.com", false, "后缀需含前导点，避免前缀伪装"},
		{"evil.com", false, "无关主机"},
		{"", false, "空主机名"},
		{"10.20.0.5", false, "IP 走 AllowAddr，不走 AllowedHost"},
	}
	for _, c := range cases {
		if got := list.AllowedHost(c.host); got != c.want {
			t.Errorf("AllowedHost(%q) = %v，期望 %v（%s）", c.host, got, c.want, c.why)
		}
	}
}

func TestAllowAddrRejectsSensitiveRanges(t *testing.T) {
	list := mustParse(t, "cdn.example.com")
	blocked := map[string]string{
		"127.0.0.1":        "回环",
		"::1":              "IPv6 回环",
		"10.1.2.3":         "私有 A 段",
		"172.16.5.4":       "私有 B 段",
		"192.168.1.1":      "私有 C 段",
		"169.254.169.254":  "云元数据端点",
		"fe80::1":          "IPv6 链路本地",
		"0.0.0.0":          "未指定",
		"224.0.0.1":        "多播",
		"::ffff:127.0.0.1": "IPv4-mapped 回环",
		"fc00::1":          "IPv6 ULA 私网",
		"fec0::1":          "IPv6 站点本地（已废弃但仍可路由）",
		"64:ff9b::7f00:1":  "NAT64 借道 127.0.0.1",
		"2002:7f00:1::":    "6to4 内嵌 127.0.0.1",
	}
	for addr, why := range blocked {
		parsed := netip.MustParseAddr(addr)
		if err := list.AllowAddr(parsed); err == nil {
			t.Errorf("AllowAddr(%s) 应当拒绝（%s）", addr, why)
		}
	}
	public := []string{"93.184.216.34", "1.1.1.1", "2606:2800:220:1:248:1893:25c8:1946"}
	for _, addr := range public {
		if err := list.AllowAddr(netip.MustParseAddr(addr)); err != nil {
			t.Errorf("AllowAddr(%s) 应当放行: %v", addr, err)
		}
	}
	if err := list.AllowAddr(netip.Addr{}); err == nil {
		t.Error("零值地址应当拒绝")
	}
}

func TestCIDREntriesAllowPrivateRangesExplicitly(t *testing.T) {
	// 受控内网场景：白名单显式列出网段后，该网段可访问。
	list := mustParse(t, "minio.internal", "10.20.0.0/16")
	if err := list.AllowAddr(netip.MustParseAddr("10.20.5.6")); err != nil {
		t.Errorf("白名单网段内应放行: %v", err)
	}
	if err := list.AllowAddr(netip.MustParseAddr("10.21.5.6")); err == nil {
		t.Error("网段外应拒绝")
	}
	if err := list.AllowAddr(netip.MustParseAddr("169.254.169.254")); err == nil {
		t.Error("即使配置了 CIDR，元数据地址也不该被放开")
	}
	single := mustParse(t, "127.0.0.1")
	if err := single.AllowAddr(netip.MustParseAddr("127.0.0.1")); err != nil {
		t.Errorf("显式列出的单个 IP 应放行: %v", err)
	}
}

// AllowResolved 只要有一条解析记录指向敏感网段就整体拒绝，避免多数正常记录掩盖
// 恶意记录。
func TestAllowResolvedRejectsIfAnyAddrSensitive(t *testing.T) {
	list := mustParse(t, "example.com")
	if err := list.AllowResolved(nil); err == nil {
		t.Error("空解析结果应报错")
	}
	mixed := []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("127.0.0.1")}
	if err := list.AllowResolved(mixed); err == nil {
		t.Error("混合解析结果应整体拒绝")
	}
	clean := []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("1.1.1.1")}
	if err := list.AllowResolved(clean); err != nil {
		t.Errorf("全公网解析结果应放行: %v", err)
	}
}

// NAT64/6to4 把 IPv4 装进 IPv6，可以借道触达回环与私网，这类前缀必须拒绝。
func TestTranslationPrefixesRejected(t *testing.T) {
	list := mustParse(t, "example.com")
	for _, addr := range []string{
		"64:ff9b::7f00:1", // NAT64 → 127.0.0.1
		"64:ff9b::a00:1",  // NAT64 → 10.0.0.1
		"2002:7f00:1::",   // 6to4 → 127.0.0.1
		"2001::1",         // Teredo
	} {
		if err := list.AllowAddr(netip.MustParseAddr(addr)); err == nil {
			t.Errorf("AllowAddr(%s) 应当拒绝", addr)
		}
	}
}

// 字面量 IP 走 AllowAddr 而非 AllowedHost：只靠主机名白名单无法约束 IP 直连。
func TestLiteralIPBypassesHostPath(t *testing.T) {
	list := mustParse(t, "example.com")
	if list.AllowedHost("127.0.0.1") {
		t.Error("字面量 IP 不应经 AllowedHost 放行")
	}
	if err := list.AllowAddr(netip.MustParseAddr("127.0.0.1")); err == nil {
		t.Error("字面量回环 IP 应被 AllowAddr 拒绝")
	}
	// 显式列出的公网 IP 可以直接使用。
	ok := mustParse(t, "203.0.113.9")
	if err := ok.AllowAddr(netip.MustParseAddr("203.0.113.9")); err != nil {
		t.Errorf("显式公网 IP 应放行: %v", err)
	}
}
