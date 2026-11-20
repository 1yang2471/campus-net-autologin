package app

import (
	"errors"
	"net"
	"testing"
)

// L2 目标选择策略（2026-11-20 真机实证）：L2 探测用的是 **v4 绑源**客户端，
// 目标必须是 v4。绑源解析没给 v4（域名只有 AAAA / 校园 DNS 先回 v6）时：
//
//   - 回退系统解析的 v4——与浏览器 Happy Eyeballs 回落同路，观察的才是用户
//     真实路径；
//   - 两者都拿不到 v4 → 放弃这次观察，返回 ok（**不把探测自身失败报成
//     「出口受限」**：那正是本 bug 的形态——面板恒挂假警告，实际网是好的）。
func TestL2TargetUsesBoundV4(t *testing.T) {
	ip, ok := l2Target(net.ParseIP("183.2.172.177"), nil, func() (net.IP, error) {
		t.Fatal("绑源已给 v4，不应再走系统回退")
		return nil, errors.New("不应调用")
	})
	if !ok || ip.String() != "183.2.172.177" {
		t.Fatalf("应直接用绑源 v4，实际 %v ok=%v", ip, ok)
	}
}

// 行为：绑源解析给了 v6（或失败）时回退系统解析的 v4。
func TestL2TargetFallsBackOnV6(t *testing.T) {
	v6 := net.ParseIP("240e:ff:e020:98c:0:ff:b061:c306")
	for _, tc := range []struct {
		name string
		ip   net.IP
		err  error
	}{
		{"绑源给 v6", v6, nil},
		{"绑源解析失败", nil, errors.New("i/o timeout")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ip, ok := l2Target(tc.ip, tc.err, func() (net.IP, error) {
				return net.ParseIP("183.2.172.177"), nil
			})
			if !ok || ip.String() != "183.2.172.177" {
				t.Fatalf("应回退系统 v4，实际 %v ok=%v", ip, ok)
			}
		})
	}
}

// 行为：两边都拿不到 v4 → 放弃观察（ok=false），调用方报 ok 而不是 restricted。
func TestL2TargetGivesUpWithoutV4(t *testing.T) {
	if _, ok := l2Target(nil, errors.New("fail"), func() (net.IP, error) {
		return net.ParseIP("240e:ff:e020:98c:0:ff:b061:c306"), nil
	}); ok {
		t.Error("两边都没有 v4 时应放弃观察")
	}
}
