package netstack

import (
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

// 选源（2026-11-11 设计 §3）：物理源必须按「能不能到门户」选，而不是按路由
// 表的 metric 选——插了网线后路由偏向以太网，而校园认证网关只在 WLAN 网段
// 可达（真机实测：1.1.1.2:443 从 WLAN 源 0.03s 通，从以太网源超时）。

// 行为：候选按序探测，第一个通的即选中（有线优先 = 路由偏好源排前面）。
func TestSourcePickerPicksFirstReachableCandidate(t *testing.T) {
	a := net.IPv4(10, 141, 53, 34)
	b := net.IPv4(172, 29, 153, 208)
	p := NewSourcePicker(
		func() []net.IP { return []net.IP{a, b} },
		func(src net.IP) error {
			if src.Equal(a) {
				return errors.New("连门户 1.1.1.2:443 失败: i/o timeout")
			}
			return nil
		})
	got, err := p.Refresh()
	if err != nil {
		t.Fatalf("应选出可达候选: %v", err)
	}
	if !got.Equal(b) {
		t.Errorf("应选中可达的 %s，实际 %s", b, got)
	}
	if !p.Current().Equal(b) {
		t.Errorf("Current 应与选中一致，实际 %s", p.Current())
	}
}

// 行为：全部候选都不通 → 保留原选中源（不用坏源覆盖好源）+ 报错且逐个候选
// 带原因（面板/日志要能说清「为什么换不了」）。
func TestSourcePickerKeepsGoodSourceWhenAllFail(t *testing.T) {
	good := net.IPv4(172, 29, 153, 208)
	bad := net.IPv4(10, 141, 53, 34)
	allDown := false
	p := NewSourcePicker(
		func() []net.IP { return []net.IP{bad, good} },
		func(src net.IP) error {
			if allDown || src.Equal(bad) {
				return errors.New("i/o timeout")
			}
			return nil
		})
	got, err := p.Refresh()
	if err != nil {
		t.Fatalf("首次应选中有可用候选: %v", err)
	}
	if !got.Equal(good) {
		t.Fatalf("应选中 %s，实际 %s", good, got)
	}

	allDown = true
	got, err = p.Refresh()
	if err == nil {
		t.Fatal("全部候选不通时必须报错")
	}
	if !got.Equal(good) {
		t.Errorf("全部不通时必须保留原源 %s，实际 %s", good, got)
	}
	if !strings.Contains(err.Error(), "10.141.53.34") || !strings.Contains(err.Error(), "172.29.153.208") {
		t.Errorf("错误须逐个列出候选与原因，实际 %q", err)
	}
}

// 行为：探测链路必须能证明「对端确实是门户」——证书主机名对不上时判定不通。
// 这里用真 DNS 替身 + 真 TLS 监听器（不 mock 证书校验）。
//
// 门户地址判据会挡掉环回，所以替身 DNS 必须答一个**真地址**：取本机非环回
// v4（没有就跳过——无网环境跑不了这个用例）。
func TestProbePortalRequiresMatchingCertHostname(t *testing.T) {
	lanIP := localNonLoopbackIPv4(t)
	if lanIP == nil {
		t.Skip("本机无非环回 v4（无网环境），跳过")
	}
	addr, _, done := selfSignedTLSListenerWithDNS(t, "portal.example.test", lanIP.String()+":0")
	defer done()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	dns := fakeDNSOn(t, lanIP, lanIP.String()+":0")
	src := lanIP

	if err := ProbePortal(src, dns, "portal.example.test", port, 3*time.Second, false); err != nil {
		t.Fatalf("证书主机名相符时探测应通过: %v", err)
	}
	err = ProbePortal(src, dns, "other.example.test", port, 3*time.Second, false)
	if err == nil {
		t.Fatal("证书主机名不符时探测必须失败")
	}
	if !strings.Contains(err.Error(), "主机名") {
		t.Errorf("错误须指明证书主机名不符，实际 %q", err)
	}
}

// localNonLoopbackIPv4 本机第一个**可用**v4（复用生产的 UsableSource：环回/
// APIPA/两代 fake-ip 段都排除）。不自己再写一份排除规则——FlClash 开着的机器
// 上第一张"非虚拟网卡"的地址可能就是 TUN 的 fake-ip（28.x）。
func localNonLoopbackIPv4(t *testing.T) net.IP {
	t.Helper()
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if UsableSource(ipn.IP) {
				return ipn.IP
			}
		}
	}
	return nil
}

// 行为：候选枚举不得把虚拟网卡的地址当候选，也不得给环回/APIPA/fake-ip。
func TestCandidateSourcesAreUsable(t *testing.T) {
	for _, src := range CandidateSources("") {
		if !UsableSource(src) {
			t.Errorf("候选源 %s 必须是可用物理源", src)
		}
	}
}

// 候选源健康记忆（2026-11-18 多源抖动实证）：选中的源可能几十秒后就死
// （17:47:39 选中 10.141.53.34，17:48:19 绑源 DNS 超时；17:56:13 选中
// 10.158.52.162，17:56:40 bind 失效）。Down 后 60s 内 Refresh 必须跳过它，
// 否则会反复选回刚死的候选；窗口过期后重新参与。

func TestSourcePickerSkipsDownCandidate(t *testing.T) {
	a := net.IPv4(10, 158, 52, 162)
	b := net.IPv4(172, 29, 153, 208)
	now := time.Unix(0, 0)
	p := NewSourcePicker(
		func() []net.IP { return []net.IP{a, b} },
		func(net.IP) error { return nil })
	p.now = func() time.Time { return now }

	got, err := p.Refresh()
	if err != nil || !got.Equal(a) {
		t.Fatalf("前置：应先选中首个候选 %s，实际 %v/%v", a, got, err)
	}
	p.Down(a, now)

	got, err = p.Refresh()
	if err != nil || !got.Equal(b) {
		t.Fatalf("降级后应跳过 %s 选 %s，实际 %v/%v", a, b, got, err)
	}
	now = now.Add(59 * time.Second)
	got, err = p.Refresh()
	if err != nil || !got.Equal(b) {
		t.Fatalf("降级窗口内仍应跳过 %s，实际 %v/%v", a, got, err)
	}
	now = now.Add(2 * time.Second)
	got, err = p.Refresh()
	if err != nil || !got.Equal(a) {
		t.Fatalf("降级过期后 %s 应重新参与，实际 %v/%v", a, got, err)
	}
}

// 行为：全部候选都在降级期 → 不探测、报错，并逐个说明「降级中」；调用方
// 据此保持原绑定（fail-closed，不拿坏源反复试）。
func TestSourcePickerAllDownReportsCooldown(t *testing.T) {
	a := net.IPv4(10, 141, 53, 34)
	probes := 0
	p := NewSourcePicker(
		func() []net.IP { return []net.IP{a} },
		func(net.IP) error { probes++; return nil })
	p.now = func() time.Time { return time.Unix(0, 0) }

	if _, err := p.Refresh(); err != nil {
		t.Fatalf("前置：首次应选中 %s: %v", a, err)
	}
	p.Down(a, time.Unix(0, 0))
	probes = 0
	got, err := p.Refresh()
	if err == nil {
		t.Fatal("全部候选降级时必须报错")
	}
	if probes != 0 {
		t.Fatalf("降级候选不得再探测，实际探测 %d 次", probes)
	}
	if got != nil && !got.Equal(a) {
		t.Errorf("无可用候选时应保留原源，实际 %v", got)
	}
	if !strings.Contains(p.Summary(), "降级中") {
		t.Errorf("摘要须说明降级，实际 %q", p.Summary())
	}
}

// 行为：Down(nil) 安全（组合根在无当前源时也会调用）。
func TestSourcePickerDownNilSafe(t *testing.T) {
	p := NewSourcePicker(func() []net.IP { return nil }, func(net.IP) error { return nil })
	p.Down(nil, time.Now())
}
