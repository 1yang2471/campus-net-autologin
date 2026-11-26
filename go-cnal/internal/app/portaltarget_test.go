package app

import (
	"net"
	"strings"
	"testing"
	"time"
)

// 门户目标解析（Python _portal_target 同语义）：TUN 劫持系统 DNS 给 fake-ip
// （2026-11-08 真机实证 a.stu.edu.cn → 28.0.0.17），绑物理源发往 fake-ip 必然
// 超时——登录/补绑/flux 查询必须用校园 DNS 绑源解析出的真实 IP 钉住直连。
// 解析失败返回 nil（调用方跳过该拍，绝不把凭据发往猜测地址）。

func TestPortalTargetCacheKeysOnSource(t *testing.T) {
	calls := 0
	pt := newPortalTargetForSource(func(src net.IP) (net.IP, error) {
		calls++
		if src.Equal(net.ParseIP("192.0.2.1")) {
			return net.ParseIP("198.51.100.1"), nil
		}
		return net.ParseIP("198.51.100.2"), nil
	}, time.Minute)
	src1 := net.ParseIP("192.0.2.1")
	src2 := net.ParseIP("192.0.2.2")
	a, err := pt.lookupForSource(src1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := pt.lookupForSource(src1)
	if err != nil || !a.Equal(b) || calls != 1 {
		t.Fatalf("同源应命中缓存，实际 %v %v calls=%d", a, b, calls)
	}
	c, err := pt.lookupForSource(src2)
	if err != nil || c.Equal(a) || calls != 2 {
		t.Fatalf("换源必须重新解析，实际 %v calls=%d", c, calls)
	}
}

// 行为：TTL 内不重复解析（每拍探测都打 DNS 既慢又吵）。
func TestPortalTargetCacheTTL(t *testing.T) {
	calls := 0
	wrap := func() (net.IP, error) {
		calls++
		return net.IPv4(10, 129, 0, 9), nil
	}
	pt := newPortalTarget(wrap, 60*time.Second)
	a, err := pt.lookup() // 第 1 次：解析
	if err != nil {
		t.Fatal(err)
	}
	b, err := pt.lookup() // 第 2 次：命中缓存
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("TTL 内应命中缓存只解析一次，实际 %d 次", calls)
	}
	if !a.Equal(b) || !a.Equal(net.IPv4(10, 129, 0, 9)) {
		t.Errorf("缓存应返回同一 IP，实际 %v %v", a, b)
	}
}

// 行为：TTL 内失败解析不触发（缓存保护一次性 DNS 抖动）；TTL 过期后
// 解析失败必须返回 nil（fail-closed，不把凭据发往可能已回收的旧 IP）；
// 首次失败同样 nil。
func TestPortalTargetFailsClosedAfterTTL(t *testing.T) {
	fail := false
	pt := newPortalTarget(func() (net.IP, error) {
		if fail {
			return nil, errTest
		}
		return net.IPv4(10, 129, 0, 9), nil
	}, 60*time.Second)
	if ip, err := pt.lookup(); ip == nil || err != nil {
		t.Fatal("首次解析应成功")
	}
	fail = true
	if ip, err := pt.lookup(); ip == nil || err != nil { // TTL 内仍命中缓存
		t.Error("TTL 内应命中缓存（一次 DNS 抖动不该打断）")
	}
	pt.now = func() time.Time { return time.Now().Add(2 * 60 * time.Second) } // TTL 过期
	if ip, err := pt.lookup(); ip != nil || err == nil {
		t.Errorf("TTL 过期且解析失败必须 nil + error（fail-closed），实际 %v %v", ip, err)
	}
	if _, err := pt.lookup(); err == nil || !strings.Contains(err.Error(), "DNS 失败") {
		t.Errorf("失败原因应原样带出（路径日志要用），实际 %v", err)
	}

	fresh := newPortalTarget(func() (net.IP, error) { return nil, errTest }, time.Second)
	if ip, err := fresh.lookup(); ip != nil || err == nil {
		t.Errorf("首次失败必须 nil + error（不猜地址），实际 %v %v", ip, err)
	}
}

type testErr struct{}

func (*testErr) Error() string { return "DNS 失败" }

var errTest error = &testErr{}
