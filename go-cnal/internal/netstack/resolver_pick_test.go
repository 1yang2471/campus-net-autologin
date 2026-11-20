package netstack

import (
	"net"
	"testing"
)

// 门户/大众站解析结果的地址族选择（2026-11-20 真机实证）：
// 校园 DNS 对 www.baidu.com 同时有 A(183.2.172.177) 与 AAAA(240e:...)，
// Go 的 LookupIP("ip") 可能先回 v6；而绑定客户端全是 **v4 源**——
// 用 v4 绑源拨 v6 目标报 "no suitable address found"，L2 探测因此**恒判**
// 「出口受限」（面板一直挂警告，实际两条源都能上网）。
//
// 规则：v4 优先；没有 v4 才回退 v6（只有 AAAA 的域名仍能拿到可用地址）。

// 行为：混合应答里优先取 v4，哪怕 v6 排在前。
func TestPickPortalIPPrefersV4(t *testing.T) {
	ips := []net.IP{
		net.ParseIP("240e:ff:e020:98c:0:ff:b061:c306"),
		net.ParseIP("183.2.172.177"),
	}
	got, err := pickPortalIP(ips)
	if err != nil {
		t.Fatalf("应选出地址: %v", err)
	}
	if got.String() != "183.2.172.177" {
		t.Errorf("应优先 v4，实际 %v", got)
	}
}

// 行为：只有 v6 时回退 v6（不把「没有 v4」当解析失败）。
func TestPickPortalIPFallsBackToV6(t *testing.T) {
	got, err := pickPortalIP([]net.IP{net.ParseIP("240e:ff:e020:98c:0:ff:b061:c306")})
	if err != nil {
		t.Fatalf("只有 v6 时应回退 v6: %v", err)
	}
	if got.To4() != nil {
		t.Errorf("应回退 v6，实际 %v", got)
	}
}

// 行为：环回/链路本地/fake-ip 仍被丢弃；全不可用时返回错误。
func TestPickPortalIPRejectsUnusable(t *testing.T) {
	ips := []net.IP{
		net.ParseIP("127.0.0.1"),
		net.ParseIP("28.0.0.1"),
		net.ParseIP("fe80::1"),
	}
	if got, err := pickPortalIP(ips); err == nil {
		t.Fatalf("全不可用应返回错误，实际 %v", got)
	}
}
